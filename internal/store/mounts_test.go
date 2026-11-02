package store

import (
	"path/filepath"
	"testing"
)

func TestMountsLifecycle(t *testing.T) {
	s, _ := newTestStore(t)
	for _, n := range []string{"web", "api"} {
		if err := s.Create(App{Name: n, Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.AddMount(Mount{App: "web", Name: "uploads", Kind: MountVolume, Target: "storage/uploads"})
	if err != nil || id == 0 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	if _, err := s.AddMount(Mount{App: "web", Name: "uploads", Kind: MountVolume, Target: "outro"}); err != ErrExists {
		t.Errorf("nome repetido na mesma app: %v", err)
	}
	if _, err := s.AddMount(Mount{App: "web", Name: "outro", Kind: MountVolume, Target: "storage/uploads"}); err != ErrExists {
		t.Errorf("destino repetido na mesma app: %v", err)
	}
	if _, err := s.AddMount(Mount{App: "api", Name: "uploads", Kind: MountVolume, Target: "storage/uploads"}); err != nil {
		t.Errorf("outra app pode usar o mesmo nome: %v", err)
	}
	if _, err := s.AddMount(Mount{App: "web", Name: "conf", Kind: MountFile, Target: "app.json", Content: "{}"}); err != nil {
		t.Fatal(err)
	}
	got := s.ListMounts("web")
	if len(got) != 2 || got[0].Name != "uploads" || got[1].Content != "{}" {
		t.Fatalf("%+v", got)
	}
	if err := s.DeleteMount("api", got[0].ID); err != nil || len(s.ListMounts("web")) != 2 {
		t.Error("remover pela app errada não pode apagar a montagem de outra")
	}
	if err := s.DeleteMount("web", got[0].ID); err != nil || len(s.ListMounts("web")) != 1 {
		t.Errorf("DeleteMount: %v", err)
	}
	if err := s.Delete("web"); err != nil || len(s.ListMounts("web")) != 0 || len(s.ListMounts("api")) != 1 {
		t.Error("excluir a app apaga só as montagens dela")
	}
}

func TestVolumeBackupSchedule(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Create(App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x"})
	if s.VolumeBackup("web") != "off" || s.VolumeBackup("nao-existe") != "off" {
		t.Error("o padrão é desligado")
	}
	if err := s.SetVolumeBackup("web", "weekly"); err != nil || s.VolumeBackup("web") != "weekly" {
		t.Errorf("agenda: %v", err)
	}
}

// A database created before the mounts migration must open and gain the new table.
func TestMountsMigrationOnOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TABLE api_tokens; DROP TABLE mounts; ALTER TABLE apps DROP COLUMN volume_backup; ALTER TABLE apps DROP COLUMN web; PRAGMA user_version = 6`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("migrar de 6 para a versão atual: %v", err)
	}
	defer s.Close()
	if _, err := s.AddMount(Mount{App: "a", Name: "v", Kind: MountVolume, Target: "v"}); err != nil {
		t.Error(err)
	}
}
