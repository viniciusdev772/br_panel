package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

func TestDatabaseForwardRoutesAndCard(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if err := s.store.CreateDatabase(store.Database{Name: "pg", Kind: "postgres", Port: 5433, Username: "postgres", Password: "pw", DBName: "app"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateDatabase(store.Database{Name: "padrao", Kind: "postgres", Port: 5432, Username: "postgres", Password: "pw", DBName: "app"}); err != nil {
		t.Fatal(err)
	}

	for _, action := range []string{"forward-on", "forward-off"} {
		p := "/databases/pg/" + action
		if w := do(h, "POST", p, url.Values{}); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Errorf("%s sem sessão: %d", p, w.Code)
		}
		if w := do(h, "POST", p, url.Values{}, c); w.Code != http.StatusForbidden {
			t.Errorf("%s sem CSRF: %d", p, w.Code)
		}
	}

	body := do(h, "GET", "/databases/pg", nil, c).Body.String()
	for _, want := range []string{"Repasse na porta padrão (5432)", "/databases/pg/forward-on", "Ativar repasse"} {
		if !strings.Contains(body, want) {
			t.Errorf("página do banco sem %q", want)
		}
	}
	if strings.Contains(body, "/databases/pg/forward-off") {
		t.Error("sem repasse ativo não deve oferecer desativar")
	}
	if body := do(h, "GET", "/databases/padrao", nil, c).Body.String(); strings.Contains(body, "Repasse na porta padrão") {
		t.Error("banco que já usa a porta padrão não precisa do cartão")
	}
}

func TestDatabasePageShowsActiveForward(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if err := s.store.CreateDatabase(store.Database{Name: "pg", Kind: "postgres", Port: 5433, Username: "postgres", Password: "pw", DBName: "app"}); err != nil {
		t.Fatal(err)
	}
	set := s.settings()
	set.UnitDir = t.TempDir()
	if err := s.applySettings(set); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set.UnitDir, "brpanelfwd-pg.socket"), []byte("x"), 0o644); err != nil { // the unit file is the state
		t.Fatal(err)
	}
	body := do(h, "GET", "/databases/pg", nil, c).Body.String()
	for _, want := range []string{"ativo: 127.0.0.1:5432", "/databases/pg/forward-off", "Desativar repasse"} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem %q", want)
		}
	}
	if strings.Contains(body, "/databases/pg/forward-on") {
		t.Error("com repasse ativo não deve oferecer ativar")
	}
}
