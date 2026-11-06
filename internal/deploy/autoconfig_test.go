package deploy

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

func TestSuggestName(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/octo/Meu_App.git":         "meu-app",
		"git@github.com:octo/site-legal":              "site-legal",
		"https://github.com/octo/a":                   "", // too short
		"ssh://git@host/x/" + strings.Repeat("a", 50): strings.Repeat("a", 32),
	} {
		if got := SuggestName(in); got != want {
			t.Errorf("%q => %q (esperado %q)", in, got, want)
		}
	}
}

func TestListenHost(t *testing.T) {
	if ListenHost(store.App{Domain: "a.exemplo.com"}) != "127.0.0.1" || ListenHost(store.App{}) != "0.0.0.0" {
		t.Error("ListenHost")
	}
}

func TestPickPortSkipsUsedAndBusy(t *testing.T) {
	s := openTestStore(t)
	srv := &Deployer{Store: s}
	_ = s.Create(store.App{Name: "a", Repo: "r", Branch: "main", StartCmd: "x", Port: firstAutoPort})
	if p := srv.PickPort(""); p != firstAutoPort+1 {
		t.Errorf("deveria pular a porta de outro app: %d", p)
	}
	old := portBusy
	portBusy = func(p int) bool { return p == 3002 } // something else listens on 3002
	t.Cleanup(func() { portBusy = old })
	if p := srv.PickPort(""); p != 3003 {
		t.Errorf("deveria pular porta em uso: %d", p)
	}
	if p := srv.PickPort("a"); p != firstAutoPort {
		t.Errorf("a própria porta do app conta como livre: %d", p)
	}
}

func TestAutoConfigure(t *testing.T) {
	s := openTestStore(t)
	srv := &Deployer{Store: s}
	app := store.App{Name: "web", Repo: "https://github.com/o/r", Branch: "main"}
	_ = s.Create(app)

	// explicit start command: nothing to do
	got, err := srv.autoConfigure(context.Background(), store.App{Name: "web", StartCmd: "./x"}, t.TempDir(), io.Discard)
	if err != nil || got.StartCmd != "./x" {
		t.Fatalf("%+v %v", got, err)
	}
	// unknown project: helpful error
	if _, err := srv.autoConfigure(context.Background(), app, project(t, map[string]string{"README.md": "x"}), io.Discard); err == nil || !strings.Contains(err.Error(), "Procfile") {
		t.Errorf("%v", err)
	}
	// Procfile-only project (no language requirements to check) is detected, saved and gets a port
	var log strings.Builder
	got, err = srv.autoConfigure(context.Background(), app, project(t, map[string]string{"Procfile": "web: ./run --port $PORT"}), &log)
	if err != nil || got.StartCmd != "./run --port $PORT" || got.Port < firstAutoPort {
		t.Fatalf("%+v %v", got, err)
	}
	saved, _ := s.Get("web")
	if saved.StartCmd != got.StartCmd || saved.Port != got.Port {
		t.Errorf("configuração detectada deve ser salva: %+v", saved)
	}
	if !strings.Contains(log.String(), "detectado: Procfile") {
		t.Errorf("log: %s", log.String())
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestEnvPairsResolveDatabaseReferences(t *testing.T) {
	s := openTestStore(t)
	_ = s.CreateDatabase(store.Database{Name: "pg", Kind: "postgres", Port: 5433, Username: "postgres", Password: "pw", DBName: "app"})
	d := &Deployer{Store: s}
	kv, err := d.envPairs(store.App{Env: "DATABASE_URL=${{pg.DATABASE_URL}}\nPLAIN=1\nMISSING=${{nope.URL}}"})
	if err != nil {
		t.Fatal(err)
	}
	if kv[0][1] != "postgresql://postgres:pw@127.0.0.1:5433/app" || kv[1][1] != "1" || kv[2][1] != "${{nope.URL}}" {
		t.Errorf("%v", kv)
	}
	content := d.envFileContent(store.App{Name: "web", Port: 3001, Env: "U=${{pg.URL}}"}, 3001, mountSetup{})
	if !strings.Contains(content, `U="postgresql://postgres:pw@127.0.0.1:5433/app"`) {
		t.Errorf("o arquivo de ambiente deve ter a referência resolvida:\n%s", content)
	}
}

// A system user has no real home: without HOME in the unit, corepack/npm fail with EACCES
// creating ~/.cache and the service restarts forever.
func TestEnvFileSetsHomeToAppDirUnlessOverridden(t *testing.T) {
	d := &Deployer{Store: openTestStore(t), Paths: Paths{Data: "/srv/brp"}}
	got := d.envFileContent(store.App{Name: "web"}, 3001, mountSetup{})
	want := `HOME="` + d.Paths.AppDir("web") + `"`
	if !strings.Contains(got, want) {
		t.Errorf("falta %s:\n%s", want, got)
	}
	got = d.envFileContent(store.App{Name: "web", Env: "HOME=/x"}, 3001, mountSetup{})
	if strings.Index(got, `HOME="/x"`) < strings.Index(got, want) {
		t.Errorf("HOME da app deve vir depois do padrão:\n%s", got)
	}
}

func TestPickPortRangeReservesEveryInstance(t *testing.T) {
	s := openTestStore(t)
	d := &Deployer{Store: s}
	// "api" runs 3 instances: firstAutoPort .. firstAutoPort+2
	_ = s.Create(store.App{Name: "api", Repo: "r", Branch: "main", StartCmd: "x", Port: firstAutoPort, Replicas: 3})
	if p := d.PickPort(""); p != firstAutoPort+3 {
		t.Errorf("uma app nova deve ficar depois das instâncias de api: %d", p)
	}
	if p := d.PickPortRange("", 2); p != firstAutoPort+3 {
		t.Errorf("faixa de 2 portas: %d", p)
	}
	if p := d.PickPortRange("api", 3); p != firstAutoPort {
		t.Errorf("a app reaproveita as próprias portas: %d", p)
	}
}

func TestPortConflict(t *testing.T) {
	s := openTestStore(t)
	d := &Deployer{Store: s}
	_ = s.Create(store.App{Name: "api", Repo: "r", Branch: "main", StartCmd: "x", Port: 3100, Replicas: 3}) // 3100-3102
	_ = s.CreateDatabase(store.Database{Name: "pg", Kind: "postgres", Port: 3205, Username: "u", Password: "p", DBName: "d"})
	for _, tc := range []struct {
		name string
		a    store.App
		bad  bool
	}{
		{"mesma app, mesmas portas", store.App{Name: "api", Port: 3100, Replicas: 3}, false},
		{"app nova depois da faixa", store.App{Name: "web", Port: 3103, Replicas: 2}, false},
		{"começa dentro da faixa de api", store.App{Name: "web", Port: 3102, Replicas: 1}, true},
		{"termina dentro da faixa de api", store.App{Name: "web", Port: 3099, Replicas: 2}, true},
		{"cobre a faixa inteira de api", store.App{Name: "web", Port: 3099, Replicas: 6}, true},
		{"api crescendo sobre outra app", store.App{Name: "api", Port: 3100, Replicas: 3}, false},
		{"faixa passa pela porta de um banco", store.App{Name: "web", Port: 3200, Replicas: 8}, true},
		{"passa de 65535", store.App{Name: "web", Port: 65535, Replicas: 2}, true},
		{"porta automática", store.App{Name: "web", Port: 0, Replicas: 4}, false},
	} {
		if got := d.PortConflict(tc.a); (got != "") != tc.bad {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
}

func TestEnvFileContentUsesTheInstancePort(t *testing.T) {
	d := &Deployer{Store: openTestStore(t)}
	a := store.App{Name: "api", Port: 3100, Replicas: 3}
	if !strings.Contains(d.envFileContent(a, 3102, mountSetup{}), `PORT="3102"`) || strings.Contains(d.envFileContent(a, 3102, mountSetup{}), "3100") {
		t.Error("cada instância recebe a própria PORT")
	}
	p := Paths{Data: "/d"}
	if p.EnvFileN("api", 0) != p.EnvFile("api") || p.EnvFileN("api", 2) != "/d/env/api.r2.env" {
		t.Errorf("%s %s", p.EnvFileN("api", 0), p.EnvFileN("api", 2))
	}
}

// The real probe (the other tests replace it): a port somebody listens on is busy, a free one is not.
func TestListenBusyOnARealPort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if !listenBusy(port) {
		t.Errorf("a porta %d está em uso por este teste", port)
	}
	l.Close()
	if listenBusy(port) {
		t.Errorf("a porta %d foi liberada", port)
	}
}
