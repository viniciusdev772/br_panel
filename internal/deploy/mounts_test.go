package deploy

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/githubapp"
	"github.com/viniciusdev772/br_panel/internal/settings"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/volume"
)

// mountDeployer returns a deployer with the app "web" and every path inside a temporary folder.
// The app user is empty, so nothing depends on a system account.
func mountDeployer(t *testing.T) (*Deployer, *store.Store) {
	t.Helper()
	data := t.TempDir()
	st := openTestStore(t)
	if err := st.Create(store.App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3001}); err != nil {
		t.Fatal(err)
	}
	set := settings.Default()
	set.AppUser = ""
	set.UnitDir = filepath.Join(data, "units")
	set.NginxDir = filepath.Join(data, "nginx")
	return New(st, data, func() settings.Settings { return set }, func() *githubapp.Client { return nil }), st
}

func TestPrepareMountsCreatesFoldersFilesAndTargets(t *testing.T) {
	d, st := mountDeployer(t)
	bind := t.TempDir()
	var fileID int64
	for _, m := range []store.Mount{
		{App: "web", Name: "uploads", Kind: store.MountVolume, Target: "storage/uploads"},
		{App: "web", Name: "midia", Kind: store.MountBind, Source: bind, Target: "media"},
		{App: "web", Name: "conf", Kind: store.MountFile, Target: "config/app.json", Content: `{"a":1}`},
	} {
		id, err := st.AddMount(m)
		if err != nil {
			t.Fatal(err)
		}
		if m.Kind == store.MountFile {
			fileID = id
		}
	}
	prep, err := d.prepareMounts(context.Background(), store.App{Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	src := d.Paths.SrcDir("web")
	vol := d.Paths.VolumeDir("web", "uploads")
	file := d.Paths.FileMount("web", fileID)
	if fi, err := os.Stat(vol); err != nil || !fi.IsDir() {
		t.Errorf("pasta do volume: %v", err)
	}
	// The app opens the volume by its real path ($VOLUME_UPLOADS): the folder above it must let the
	// app user walk through (x) without listing the other volumes. It was root-only (0750) before.
	if fi, err := os.Stat(d.Paths.VolumesDir("web")); err != nil || fi.Mode().Perm() != 0o711 {
		t.Errorf("pasta pai dos volumes precisa ser 0711: %v %v", err, fi)
	}
	if fi, _ := os.Stat(vol); fi.Mode().Perm() != 0o750 {
		t.Errorf("a pasta do volume continua 0750: %v", fi.Mode().Perm())
	}
	if fi, err := os.Stat(file); err != nil {
		t.Fatalf("arquivo guardado: %v", err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("o arquivo guardado deve ser 0600: %v", fi.Mode())
	}
	if b, _ := os.ReadFile(file); string(b) != `{"a":1}` {
		t.Errorf("conteúdo = %q", b)
	}
	for _, target := range []string{filepath.Join(src, "storage/uploads"), filepath.Join(src, "media"), filepath.Join(src, "config/app.json")} {
		if _, err := os.Stat(target); err != nil {
			t.Errorf("destino %s não foi criado: %v", target, err)
		}
	}
	want := map[string]string{
		"VOLUME_UPLOADS": vol,
		"VOLUME_MIDIA":   bind,
		"VOLUME_CONF":    file,
	}
	for _, p := range prep.Env {
		if want[p[0]] != p[1] {
			t.Errorf("%s = %q (esperado %q)", p[0], p[1], want[p[0]])
		}
	}
	if len(prep.Env) != 3 {
		t.Errorf("env = %v", prep.Env)
	}
	binds := strings.Join(prep.Bind, " ")
	if !strings.Contains(binds, vol+":"+filepath.Join(src, "storage/uploads")) || !strings.Contains(binds, bind+":"+filepath.Join(src, "media")) {
		t.Errorf("BindPaths = %v", prep.Bind)
	}
	if len(prep.ReadOnly) != 1 || prep.ReadOnly[0] != file+":"+filepath.Join(src, "config/app.json") {
		t.Errorf("ReadOnly = %v", prep.ReadOnly)
	}
}

func TestPrepareMountsRefusesBadRowsAndMissingBind(t *testing.T) {
	d, st := mountDeployer(t)
	// AddMount does not validate: a row edited outside the form must still be refused here.
	if _, err := st.AddMount(store.Mount{App: "web", Name: "fora", Kind: store.MountVolume, Target: "../fora"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.prepareMounts(context.Background(), store.App{Name: "web"}); err == nil {
		t.Error("destino com .. deveria ser recusado")
	}

	d2, st2 := mountDeployer(t)
	if _, err := st2.AddMount(store.Mount{App: "web", Name: "sumiu", Kind: store.MountBind, Source: filepath.Join(t.TempDir(), "nao-existe"), Target: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.prepareMounts(context.Background(), store.App{Name: "web"}); err == nil || !strings.Contains(err.Error(), "não existe") {
		t.Errorf("origem ausente: %v", err)
	}
}

func TestWriteInstanceMountsEveryInstance(t *testing.T) {
	d, st := mountDeployer(t)
	if _, err := st.AddMount(store.Mount{App: "web", Name: "uploads", Kind: store.MountVolume, Target: "storage/uploads"}); err != nil {
		t.Fatal(err)
	}
	a := store.App{Name: "web", Port: 3001, Replicas: 2, StartCmd: "x"}
	prep, err := d.prepareMounts(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	for k := 0; k < 2; k++ {
		if err := d.writeInstance(a, k, prep); err != nil {
			t.Fatal(err)
		}
	}
	for k := 0; k < 2; k++ {
		unit, err := os.ReadFile(d.unitFile("web", k))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(unit), "BindPaths="+d.Paths.VolumeDir("web", "uploads")+":") {
			t.Errorf("instância %d sem a montagem:\n%s", k, unit)
		}
		env, err := os.ReadFile(d.Paths.EnvFileN("web", k))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(env), `VOLUME_UPLOADS="`+d.Paths.VolumeDir("web", "uploads")+`"`) {
			t.Errorf("instância %d sem VOLUME_UPLOADS:\n%s", k, env)
		}
	}
}

func TestRemoveDeletesVolumesFilesAndBackups(t *testing.T) {
	d, _ := mountDeployer(t)
	fakeBin(t, map[string]string{"systemctl": "exit 0"})
	dirs := []string{d.Paths.VolumesDir("web"), d.Paths.FilesDir("web"), d.Paths.VolumeBackupDir("web"), d.Paths.AppDir("web")}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dirs[0], "dado.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.Remove("web"); err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s deveria ter sido apagado", dir)
		}
	}
}

func TestArchiveVolumeRoundTrip(t *testing.T) {
	d, _ := mountDeployer(t)
	vol := d.Paths.VolumeDir("web", "uploads")
	if err := os.MkdirAll(filepath.Join(vol, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vol, "sub", "a.txt"), []byte("dados"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), volume.FileName("uploads", time.Now()))
	if err := archiveVolume(vol, dst); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dst); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup: %v %v", fi, err)
	}
	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := t.TempDir()
	if err := volume.Extract(f, out); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(out, "sub", "a.txt")); err != nil || string(b) != "dados" {
		t.Errorf("conteúdo restaurado = %q, %v", b, err)
	}
	// a missing folder is a clear error, not an empty archive
	if err := archiveVolume(filepath.Join(vol, "nao-existe"), filepath.Join(t.TempDir(), "x.tar.gz")); err == nil {
		t.Error("volume sem pasta deveria falhar")
	}
}

func TestVolumeBackupPathRefusesTraversal(t *testing.T) {
	d, _ := mountDeployer(t)
	dir := d.Paths.VolumeBackupDir("web")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	name := volume.FileName("uploads", time.Now())
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.VolumeBackupPath("web", name); !ok {
		t.Error("backup válido não encontrado")
	}
	for _, bad := range []string{"../" + name, "outra/" + name, "uploads.tar.gz", ""} {
		if _, ok := d.VolumeBackupPath("web", bad); ok {
			t.Errorf("%q não deveria ser aceito", bad)
		}
	}
	if err := d.DeleteVolumeBackup("web", "../"+name); err == nil {
		t.Error("apagar com caminho relativo deveria falhar")
	}
	if err := d.DeleteVolumeBackup("web", name); err != nil {
		t.Error(err)
	}
}

func TestRestoreVolumeReplacesTheFolder(t *testing.T) {
	d, st := mountDeployer(t)
	if _, err := st.AddMount(store.Mount{App: "web", Name: "uploads", Kind: store.MountVolume, Target: "storage/uploads"}); err != nil {
		t.Fatal(err)
	}
	calls := fakeBin(t, map[string]string{"systemctl": `echo "systemctl $@" >> "$(dirname "$0")/calls"; exit 0`})
	dir := d.Paths.VolumeBackupDir("web")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	old := d.Paths.VolumeDir("web", "uploads")
	if err := os.MkdirAll(old, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "antigo.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// build a backup holding a different file
	stage := t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, "novo.txt"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := volume.FileName("uploads", time.Now())
	if err := archiveVolume(stage, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	backup, ok := d.VolumeBackupPath("web", name)
	if !ok {
		t.Fatal("backup não encontrado")
	}
	if err := d.restoreVolume(context.Background(), "web", "uploads", backup, io.Discard); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(old)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "novo.txt" {
		t.Errorf("a pasta deveria conter só o arquivo do backup: %v", entries)
	}
	if _, err := os.Stat(old + restoreSuffix); !os.IsNotExist(err) {
		t.Error("a pasta temporária deveria ter sido removida")
	}
	got, _ := os.ReadFile(calls)
	if !strings.Contains(string(got), "systemctl stop ") || !strings.Contains(string(got), "systemctl start ") {
		t.Errorf("as instâncias deveriam parar e voltar: %s", got)
	}
}

func TestBackupVolumesNeedsAVolume(t *testing.T) {
	d, _ := mountDeployer(t)
	if err := d.BackupVolumes("web", "manual"); err == nil {
		t.Error("sem volume não há o que copiar")
	}
}
