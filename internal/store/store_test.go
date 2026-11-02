package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "t.db"))
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
	s2, err := Open(filepath.Join(dir, "t.db"))
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

func TestMetricsBucketsAndLatest(t *testing.T) {
	s, _ := newTestStore(t)
	base := time.Now().Add(-10 * time.Minute).Truncate(time.Minute)
	var pts []Point
	for i := 0; i < 8; i++ { // 8 samples, 15 s apart
		pts = append(pts, Point{TS: base.Add(time.Duration(i) * 15 * time.Second).Unix(), CPU: float64(i * 10), Mem: int64(i+1) << 20})
	}
	if err := s.InsertMetrics("web", pts); err != nil {
		t.Fatal(err)
	}
	got := s.Metrics("web", base, 60) // one-minute buckets
	if len(got) != 2 || got[0].CPU != 15 || got[0].Mem != 4<<20 {
		t.Errorf("%+v", got)
	}
	if l := s.LatestMetrics(base)["web"]; l.Mem != 8<<20 {
		t.Errorf("latest: %+v", l)
	}
	if len(s.LatestMetrics(time.Now())) != 0 {
		t.Error("amostras antigas não devem aparecer")
	}
	s.PruneMetrics(time.Now())
	if len(s.Metrics("web", base, 60)) != 0 {
		t.Error("prune")
	}
}

func TestDatabasesAndEvents(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Create(App{Name: "web", Repo: "r", Branch: "main", StartCmd: "x", Port: 3001})
	if err := s.CreateDatabase(Database{Name: "web", Kind: "postgres", Port: 5433, Username: "u", Password: "p"}); err != ErrExists {
		t.Errorf("banco com nome de app deve falhar: %v", err)
	}
	if err := s.CreateDatabase(Database{Name: "pg", Kind: "postgres", Port: 5433, Username: "u", Password: "p", DBName: "app"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateDatabase(Database{Name: "pg", Kind: "redis", Port: 6380, Username: "u", Password: "p"}); err != ErrExists {
		t.Errorf("nome duplicado: %v", err)
	}
	if err := s.Create(App{Name: "pg", Repo: "r", Branch: "main", StartCmd: "x"}); err != ErrExists {
		t.Errorf("app com nome de banco deve falhar: %v", err)
	}
	d, ok := s.GetDatabase("pg")
	if !ok || d.Status != DBProvisioning || d.Public || d.BackupSchedule != "off" || d.DBName != "app" {
		t.Fatalf("%+v %v", d, ok)
	}
	s.SetDatabaseStatus("pg", DBReady)
	_ = s.SetDatabasePublic("pg", true)
	_ = s.SetDatabasePassword("pg", "novo")
	_ = s.SetDatabaseSchedule("pg", "daily")
	if d, _ := s.GetDatabase("pg"); d.Status != DBReady || !d.Public || d.Password != "novo" || d.BackupSchedule != "daily" {
		t.Errorf("%+v", d)
	}
	used := s.UsedPorts()
	if !used[3001] || !used[5433] {
		t.Errorf("portas: %v", used)
	}
	_ = s.InsertMetrics("db:pg", []Point{{TS: time.Now().Unix(), CPU: 1, Mem: 1}})
	if err := s.DeleteDatabase("pg"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.GetDatabase("pg"); ok || len(s.Metrics("db:pg", time.Now().Add(-time.Hour), 1)) != 0 {
		t.Error("DeleteDatabase deve remover o banco e as métricas")
	}

	for i := 0; i < 3; i++ {
		s.AddEvent("deploy", "web", "msg")
	}
	if ev := s.Events(2); len(ev) != 2 || ev[0].Resource != "web" {
		t.Errorf("%+v", ev)
	}
}

func TestSessionsRoundTrip(t *testing.T) {
	s, dir := newTestStore(t)
	exp := time.Now().Add(time.Hour).Truncate(time.Second)
	s.SaveSession("h1", "csrf1", exp)
	s.SaveSession("h2", "csrf2", time.Now().Add(-time.Minute)) // expired
	s.DeleteSession("nada")

	got := map[string]string{}
	s.EachSession(func(h, csrf string, e time.Time) {
		got[h] = csrf
		if h == "h1" && !e.Equal(exp) {
			t.Errorf("exp: %v", e)
		}
		s.SaveSession(h, csrf, e) // the callback may use the database (single connection)
	})
	if len(got) != 1 || got["h1"] != "csrf1" {
		t.Fatalf("só a sessão válida deve voltar: %v", got)
	}

	s.Close() // reopen: it must still be there
	s2, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	n := 0
	s2.EachSession(func(string, string, time.Time) { n++ })
	if n != 1 {
		t.Errorf("depois de reabrir: %d sessões", n)
	}
	s2.DeleteAllSessions()
	s2.EachSession(func(string, string, time.Time) { t.Error("deveria estar vazio") })
}
