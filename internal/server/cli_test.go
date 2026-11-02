package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/cli"
	"github.com/viniciusdev772/br_panel/internal/store"
)

// handlerRT sends the CLI's requests straight into the CLI-side handler, as the Unix socket would.
type handlerRT struct{ h http.Handler }

func (t handlerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.h.ServeHTTP(w, r)
	return w.Result(), nil
}

func runCLI(t *testing.T, s *Server, stdin string, args ...string) (code int, out, errs string) {
	t.Helper()
	var o, e bytes.Buffer
	code = cli.RunWith(handlerRT{s.cliHandler()}, args, strings.NewReader(stdin), &o, &e)
	return code, o.String(), e.String()
}

// The CLI reuses the panel's own routes, so the validation, nginx handling and messages are the
// web UI's. What it must never do is open those routes to anyone but the socket.
func TestCLIEndToEnd(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if err := s.store.Create(store.App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3001, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	app := func() store.App { a, _ := s.store.Get("web"); return a }
	ok := func(args ...string) string {
		t.Helper()
		code, out, errs := runCLI(t, s, "", args...)
		if code != 0 {
			t.Fatalf("%v: código %d\n%s%s", args, code, out, errs)
		}
		return out
	}
	bad := func(want string, args ...string) {
		t.Helper()
		code, out, errs := runCLI(t, s, "", args...)
		if code != 1 || !strings.HasPrefix(errs, "ERRO:") || !strings.Contains(errs, want) {
			t.Fatalf("%v: queria falha com %q, veio %d\n%s%s", args, want, code, out, errs)
		}
	}

	if out := ok("apps", "list"); !strings.Contains(out, "web") || !strings.Contains(out, "NOME") {
		t.Errorf("apps list: %q", out)
	}
	var list []cli.AppInfo
	if err := json.Unmarshal([]byte(ok("apps", "list", "--json")), &list); err != nil || len(list) != 1 || list[0].Name != "web" || list[0].Env != "" {
		t.Errorf("apps list --json: %v %+v", err, list)
	}
	if out := ok("app", "show", "web"); !strings.Contains(out, "repositório:") || !strings.Contains(out, "https://github.com/o/r") {
		t.Errorf("apps show: %q", out)
	}
	bad("não encontrado", "apps", "show", "nope")
	if out := ok("dbs", "list"); !strings.Contains(out, "Nenhum banco") {
		t.Errorf("dbs list: %q", out)
	}
	ok("status")
	ok("events")

	// Variables: merged line by line, validated by the same code as the web tab.
	ok("apps", "env-set", "web", "A=1", "B=2")
	ok("apps", "env", "set", "web", "A=9")
	if got := app().Env; got != "A=9\nB=2" {
		t.Errorf("env = %q", got)
	}
	ok("apps", "env-unset", "web", "B")
	if got := app().Env; got != "A=9" {
		t.Errorf("env depois do unset = %q", got)
	}
	bad("inexistente", "apps", "env-set", "web", "DATABASE_URL=${{nao.DATABASE_URL}}")
	if got := app().Env; got != "A=9" {
		t.Errorf("variável inválida foi salva: %q", got)
	}

	// Settings: only what is given changes; invalid values are refused and nothing is saved.
	ok("apps", "set", "web", "--memory-mb", "256", "--replicas", "2")
	if a := app(); a.MemoryMB != 256 || a.Replicas != 2 || a.Env != "A=9" || a.Port != 3001 || a.StartCmd != "x" {
		t.Errorf("apps set: %+v", a)
	}
	bad("", "apps", "set", "web", "--replicas", "99")
	if app().Replicas != 2 {
		t.Error("valor inválido foi salvo")
	}

	// Site tab.
	ok("apps", "maintenance", "web", "on", "--title", "Voltamos já", "--text", "Atualizando.")
	ok("apps", "maintenance", "web", "off")
	if w := app().Web; w.Maintenance || w.MaintTitle != "Voltamos já" || w.MaintText != "Atualizando." {
		t.Errorf("manutenção: %+v", w)
	}
	bad("domínio principal", "apps", "domain-add", "web", "shop.example.com") // needs the main domain first
	ok("apps", "set", "web", "--domain", "web.example.com")
	ok("apps", "domain-add", "web", "shop.example.com")
	ok("apps", "domain-add", "web", "www.example.com", "--redirect-to", "shop.example.com", "--permanent")
	if d := app().Web.Domains; len(d) != 2 || d[1].To != "shop.example.com" || !d[1].Permanent {
		t.Errorf("domínios: %+v", d)
	}
	ok("apps", "domain-remove", "web", "shop.example.com")
	if code, _, _ := runCLI(t, s, "", "apps", "auth-add", "web", "cliente", "--password-stdin"); code != 2 {
		t.Errorf("senha vazia deveria ser erro de uso: %d", code)
	}
	if code, _, errs := runCLI(t, s, "senha-de-exemplo-123\n", "apps", "auth-add", "web", "cliente", "--password-stdin"); code != 0 {
		t.Fatalf("auth-add: %d %s", code, errs)
	}
	shown := ok("apps", "show", "web", "--json")
	if strings.Contains(shown, "$2") || strings.Contains(shown, "senha-de-exemplo") || !strings.Contains(shown, "cliente") {
		t.Errorf("a senha ou o hash apareceram: %s", shown)
	}
	ok("apps", "limits", "web", "--upload-mb", "50")
	if w := app().Web; w.MaxBodyMB != 50 {
		t.Errorf("limites: %+v", w)
	}

	// Nothing above worked because of a hole: the same routes still demand a session from browsers.
	for _, p := range []string{"/api/apps", "/api/apps/web", "/api/databases", "/cli", "/cli/guide.txt"} {
		if w := do(h, "GET", p, nil); w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "/login") {
			t.Errorf("GET %s sem sessão: %d", p, w.Code)
		}
	}
	if w := do(h, "POST", "/apps/web/restart", nil); w.Code != http.StatusSeeOther {
		t.Errorf("POST sem sessão: %d", w.Code)
	}

	// The panel page and the plain-text guide.
	page := do(h, "GET", "/cli", nil, c)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "brpanel apps deploy") || !strings.Contains(page.Body.String(), "REGRAS:") {
		t.Errorf("/cli: %d", page.Code)
	}
	g := do(h, "GET", "/cli/guide.txt", nil, c)
	if g.Code != http.StatusOK || !strings.HasPrefix(g.Header().Get("Content-Type"), "text/plain") || !strings.Contains(g.Body.String(), "RECEITAS") {
		t.Errorf("/cli/guide.txt: %d %s", g.Code, g.Header().Get("Content-Type"))
	}
}
