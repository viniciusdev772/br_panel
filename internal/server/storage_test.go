package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
)

// The storage tab changes state, so every action is POST + CSRF; names and paths are validated.
func TestAppStorageRoutes(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if err := s.store.Create(store.App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3001}); err != nil {
		t.Fatal(err)
	}
	csrf := csrfOf(t, h, "/apps/web", c)

	for _, action := range []string{"mount-add", "mount-delete", "volume-backup", "volume-restore", "volume-delete", "volume-schedule"} {
		if w := do(h, "POST", "/apps/web/"+action, url.Values{}, c); w.Code != http.StatusForbidden {
			t.Errorf("%s sem CSRF: %d", action, w.Code)
		}
	}
	if len(s.store.ListMounts("web")) != 0 {
		t.Fatal("nada deveria ter sido salvo sem CSRF")
	}

	add := url.Values{"csrf": {csrf}, "mount_name": {"uploads"}, "mount_kind": {"volume"}, "mount_target": {"storage/uploads"}}
	if w := do(h, "POST", "/apps/web/mount-add", add, c); w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "#armazenamento") {
		t.Fatalf("adicionar volume: %d %s", w.Code, w.Header().Get("Location"))
	}
	if m := s.store.ListMounts("web"); len(m) != 1 || m[0].Kind != store.MountVolume || m[0].Target != "storage/uploads" {
		t.Fatalf("montagens = %+v", m)
	}

	// same name or same target, and hostile paths, are refused
	dup := url.Values{"csrf": {csrf}, "mount_name": {"uploads"}, "mount_kind": {"volume"}, "mount_target": {"outro"}}
	if w := do(h, "POST", "/apps/web/mount-add", dup, c); !strings.Contains(w.Header().Get("Location"), "N%C3%A3o+salvo") {
		t.Errorf("nome repetido deveria ser recusado: %s", w.Header().Get("Location"))
	}
	for _, bad := range []url.Values{
		{"csrf": {csrf}, "mount_name": {"fora"}, "mount_kind": {"volume"}, "mount_target": {"../fora"}},
		{"csrf": {csrf}, "mount_name": {"etc"}, "mount_kind": {"bind"}, "mount_source": {"/etc"}, "mount_target": {"etc"}},
		{"csrf": {csrf}, "mount_name": {"x"}, "mount_kind": {"file"}, "mount_target": {"a\nExecStartPre=/bin/x"}, "mount_content": {"x"}},
		{"csrf": {csrf}, "mount_name": {"x"}, "mount_kind": {"tmpfs"}, "mount_target": {"x"}},
	} {
		do(h, "POST", "/apps/web/mount-add", bad, c)
	}
	if m := s.store.ListMounts("web"); len(m) != 1 {
		t.Errorf("entradas inválidas foram salvas: %+v", m)
	}

	// a file mount keeps its text in the database
	file := url.Values{"csrf": {csrf}, "mount_name": {"conf"}, "mount_kind": {"file"}, "mount_target": {"config/app.json"}, "mount_content": {"{\"a\":1}"}}
	if w := do(h, "POST", "/apps/web/mount-add", file, c); w.Code != http.StatusSeeOther {
		t.Errorf("adicionar arquivo: %d", w.Code)
	}
	all := s.store.ListMounts("web")
	if len(all) != 2 || all[1].Content != `{"a":1}` {
		t.Fatalf("montagens = %+v", all)
	}
	// removing uses the row id, never the name
	if w := do(h, "POST", "/apps/web/mount-delete", url.Values{"csrf": {csrf}, "id": {strconv.FormatInt(all[1].ID, 10)}}, c); w.Code != http.StatusSeeOther {
		t.Errorf("remover montagem: %d", w.Code)
	}
	if m := s.store.ListMounts("web"); len(m) != 1 || m[0].ID != all[0].ID {
		t.Errorf("remover apagou a montagem errada: %+v", m)
	}

	// schedule accepts only the three known values
	if w := do(h, "POST", "/apps/web/volume-schedule", url.Values{"csrf": {csrf}, "schedule": {"hourly"}}, c); !strings.Contains(w.Header().Get("Location"), "Falha") {
		t.Errorf("agendamento inválido deveria falhar: %s", w.Header().Get("Location"))
	}
	if w := do(h, "POST", "/apps/web/volume-schedule", url.Values{"csrf": {csrf}, "schedule": {"daily"}}, c); w.Code != http.StatusSeeOther {
		t.Errorf("agendar: %d", w.Code)
	}
	if got := s.store.VolumeBackup("web"); got != "daily" {
		t.Errorf("agendamento = %q", got)
	}

	// backup files are listed, downloadable and deletable; the name is validated
	for _, p := range []string{"/apps/web/volume-backups/..%2f..%2fetc%2fpasswd", "/apps/web/volume-backups/uploads.tar.gz"} {
		if w := do(h, "GET", p, nil, c); w.Code != http.StatusNotFound {
			t.Errorf("%s deveria ser 404: %d", p, w.Code)
		}
	}
	// a volume with no folder yet cannot be copied: the message says so
	if w := do(h, "POST", "/apps/web/volume-backup", url.Values{"csrf": {csrf}}, c); w.Code != http.StatusSeeOther {
		t.Errorf("backup sem pasta: %d", w.Code)
	}
	for i := 0; i < 200 && s.dep.VolumeBusy("web") != ""; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	volDir := filepath.Join(s.cfg.DataDir, "volumes", "web", "uploads")
	if err := os.MkdirAll(volDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(volDir, "dado.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if w := do(h, "POST", "/apps/web/volume-backup", url.Values{"csrf": {csrf}}, c); strings.Contains(w.Header().Get("Location"), "Falha") {
		t.Fatalf("backup: %s", w.Header().Get("Location"))
	}
	var backup string
	deadline := time.Now().Add(5 * time.Second)
	for backup == "" && time.Now().Before(deadline) {
		if b := s.dep.VolumeBackups("web"); len(b) > 0 {
			backup = b[0].File
		}
		time.Sleep(20 * time.Millisecond)
	}
	if backup == "" {
		t.Fatal("o backup não foi criado")
	}
	if w := do(h, "GET", "/apps/web/volume-backups/"+backup, nil, c); w.Code != http.StatusOK {
		t.Errorf("download: %d", w.Code)
	}
	if w := do(h, "POST", "/apps/web/volume-restore", url.Values{"csrf": {csrf}, "file": {"../" + backup}}, c); !strings.Contains(w.Header().Get("Location"), "Falha") {
		t.Errorf("restaurar com caminho relativo deveria falhar: %s", w.Header().Get("Location"))
	}
	if w := do(h, "POST", "/apps/web/volume-delete", url.Values{"csrf": {csrf}, "file": {backup}}, c); w.Code != http.StatusSeeOther {
		t.Errorf("excluir backup: %d", w.Code)
	}
	if b := s.dep.VolumeBackups("web"); len(b) != 0 {
		t.Errorf("o backup deveria ter sido apagado: %+v", b)
	}
}
