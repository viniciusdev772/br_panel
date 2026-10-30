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
	l, err := net.Listen("tcp", "127.0.0.1:3002")
	if err == nil {
		defer l.Close()
		if p := srv.PickPort(""); p != 3003 {
			t.Errorf("deveria pular porta em uso: %d", p)
		}
	}
	if p := srv.PickPort("a"); p != firstAutoPort && err == nil {
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
