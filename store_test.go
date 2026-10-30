package main

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func TestStoreAppsLifecycle(t *testing.T) {
	s, dir := newTestStore(t)
	if st, _ := os.Stat(filepath.Join(dir, "t.db")); st.Mode().Perm() != 0o600 {
		t.Errorf("permissão do banco: %v", st.Mode().Perm())
	}
	a := App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3000, Domain: "a.exemplo.com", AutoDeploy: true, Env: "A=1"}
	if err := s.Create(a); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(a); err != ErrExists {
		t.Errorf("nome duplicado: %v", err)
	}
	b := a
	b.Name = "web2"
	if err := s.Create(b); err != ErrExists {
		t.Errorf("domínio duplicado deveria falhar: %v", err)
	}
	b.Domain = ""
	if err := s.Create(b); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("web")
	if !ok || got.Status != StatusIdle || !got.AutoDeploy || got.Env != "A=1" || got.Port != 3000 {
		t.Fatalf("%+v %v", got, ok)
	}
	if o, ok := s.DomainOwner("a.exemplo.com"); !ok || o != "web" {
		t.Error("DomainOwner")
	}

	if _, ok := s.BeginDeploy("web"); !ok {
		t.Fatal("primeiro BeginDeploy")
	}
	if _, ok := s.BeginDeploy("web"); ok {
		t.Error("deploy simultâneo deveria ser recusado")
	}
	a.Branch = "dev"
	if err := s.UpdateConfig(a); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.Get("web"); g.Branch != "dev" || g.Status != StatusDeploying {
		t.Errorf("UpdateConfig não deve alterar o status: %+v", g)
	}
	s.FinishDeploy("web", true)
	if g, _ := s.Get("web"); g.Status != StatusDeployed || g.LastDeploy.IsZero() {
		t.Errorf("%+v", g)
	}
	if len(s.List()) != 2 {
		t.Error("List")
	}
	_ = s.Delete("web")
	if _, ok := s.Get("web"); ok {
		t.Error("Delete")
	}
}

func TestStoreReopenResetsInterruptedDeploys(t *testing.T) {
	s, dir := newTestStore(t)
	_ = s.Create(App{Name: "web", Repo: "r", Branch: "main", StartCmd: "x"})
	s.BeginDeploy("web")
	s.Close()
	s2, err := OpenStore(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if g, _ := s2.Get("web"); g.Status != StatusFailed {
		t.Errorf("status após reinício: %s", g.Status)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	s, _ := newTestStore(t)
	def, err := s.LoadSettings()
	if err != nil || def.NginxDir != "/etc/nginx/conf.d" || def.AdminHash != "" {
		t.Fatalf("%+v %v", def, err)
	}
	def.AdminUser, def.PanelDomain, def.GitHubKey = "admin", "p.exemplo.com", "-----KEY-----\nabc\n"
	if err := s.SaveSettings(def); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadSettings()
	if got != def {
		t.Errorf("\n%+v\n%+v", got, def)
	}
}

func TestImportLegacy(t *testing.T) {
	s, dir := newTestStore(t)
	p := filepath.Join(dir, "apps.json")
	_ = os.WriteFile(p, []byte(`[{"name":"old","repo":"https://github.com/o/r","branch":"main","build_cmd":"make","start_cmd":"./run","port":4000,"auto_deploy":true}]`), 0o600)
	if err := s.ImportLegacy(p); err != nil {
		t.Fatal(err)
	}
	g, ok := s.Get("old")
	if !ok || g.BuildCmd != "make" || g.StartCmd != "./run" || !g.AutoDeploy || g.Port != 4000 {
		t.Fatalf("%+v", g)
	}
	if _, err := os.Stat(p + ".migrated"); err != nil {
		t.Error("apps.json deveria ser renomeado")
	}
}
