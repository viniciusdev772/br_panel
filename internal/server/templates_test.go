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

func TestTemplatesInstall(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	// "apps" becomes a file: the deploy that starts after an install fails at once and installs nothing.
	apps := filepath.Join(s.cfg.DataDir, "apps")
	if err := os.RemoveAll(apps); err != nil || os.WriteFile(apps, nil, 0o600) != nil {
		t.Fatal("preparo do teste")
	}
	csrf := csrfOf(t, h, "/templates", c)

	if w := do(h, "GET", "/templates", nil, c); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "n8n") || !strings.Contains(w.Body.String(), "Gitea") {
		t.Errorf("/templates: %d", w.Code)
	}
	if w := do(h, "GET", "/templates/n8n", nil, c); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Instalar n8n") {
		t.Errorf("/templates/n8n: %d", w.Code)
	}
	if w := do(h, "GET", "/templates/nao-existe", nil, c); w.Code != http.StatusNotFound {
		t.Errorf("template inexistente: %d", w.Code)
	}
	for _, p := range []string{"/templates", "/templates/n8n", "/api/templates"} {
		if w := do(h, "GET", p, nil); w.Code != http.StatusSeeOther {
			t.Errorf("GET %s sem sessão: %d", p, w.Code)
		}
	}
	if w := do(h, "POST", "/templates/n8n", url.Values{"name": {"x1"}}, c); w.Code != http.StatusForbidden {
		t.Errorf("instalar sem CSRF: %d", w.Code)
	}
	if w := do(h, "POST", "/templates/n8n", url.Values{"name": {"x1"}}); w.Code != http.StatusSeeOther || s.store.CountApps() != 0 {
		t.Errorf("instalar sem sessão: %d", w.Code)
	}

	post := func(id string, v url.Values) int {
		v.Set("csrf", csrf)
		return do(h, "POST", "/templates/"+id, v, c).Code
	}
	if code := post("n8n", url.Values{"name": {"flows"}, "domain": {"Flows.Example.com"}}); code != http.StatusSeeOther {
		t.Fatalf("instalar n8n: %d", code)
	}
	waitIdle(t, s, "flows")
	a, ok := s.store.Get("flows")
	if !ok || a.Repo != "template:n8n" || a.Domain != "flows.example.com" || a.Port == 0 || a.Kind != "Template: n8n" {
		t.Fatalf("app criado: %+v", a)
	}
	for _, want := range []string{"TPL_VERSION=latest", "N8N_ENCRYPTION_KEY=", "WEBHOOK_URL=https://flows.example.com/"} {
		if !strings.Contains(a.Env, want) {
			t.Errorf("faltou %q nas variáveis:\n%s", want, a.Env)
		}
	}
	if m := s.store.ListMounts("flows"); len(m) != 1 || m[0].Name != "data" || m[0].Kind != store.MountVolume {
		t.Errorf("armazenamento: %+v", m)
	}

	// Refusals leave nothing behind.
	for name, v := range map[string]url.Values{
		"nome repetido":    {"name": {"flows"}},
		"domínio em uso":   {"name": {"outro"}, "domain": {"flows.example.com"}},
		"domínio inválido": {"name": {"outro"}, "domain": {"not a domain"}},
		"nome inválido":    {"name": {"Nome Ruim!"}},
		"versão perigosa":  {"name": {"outro"}, "version": {"1;rm -rf /"}},
	} {
		if code := post("n8n", v); code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d", name, code)
		}
	}
	if post("gitea", url.Values{"name": {"git"}}) != http.StatusSeeOther {
		t.Fatal("instalar gitea")
	}
	waitIdle(t, s, "git")
	if s.store.CountApps() != 2 {
		t.Errorf("apps: %d", s.store.CountApps())
	}
	g, _ := s.store.Get("git")
	if g.Port == a.Port {
		t.Error("cada instalação precisa da própria porta")
	}

	// The same flow from the CLI.
	if code, out, errs := runCLI(t, s, "", "templates", "list"); code != 0 || !strings.Contains(out, "uptime-kuma") {
		t.Errorf("templates list: %d %q %q", code, out, errs)
	}
	if code, out, _ := runCLI(t, s, "", "templates", "show", "n8n"); code != 0 || !strings.Contains(out, "Depois de instalar") {
		t.Errorf("templates show: %d %q", code, out)
	}
	if code, _, errs := runCLI(t, s, "", "templates", "show", "nada"); code != 1 || !strings.Contains(errs, "n8n") {
		t.Errorf("show inexistente deve listar os ids: %d %q", code, errs)
	}
	if code, out, errs := runCLI(t, s, "", "install", "uptime-kuma", "--name", "status"); code != 0 || !strings.Contains(out, "OK:") {
		t.Errorf("install: %d %q %q", code, out, errs)
	}
	waitIdle(t, s, "status")
	if code, _, errs := runCLI(t, s, "", "install", "uptime-kuma", "--name", "status"); code != 1 || !strings.Contains(errs, "já existe") {
		t.Errorf("install repetido: %d %q", code, errs)
	}
	if code, _, errs := runCLI(t, s, "", "install", "nada"); code != 1 || !strings.Contains(errs, "não existe") {
		t.Errorf("install inexistente: %d %q", code, errs)
	}
	if st, _ := s.store.Get("status"); st.Branch != "2.5.9" || st.Repo != "https://github.com/louislam/uptime-kuma.git" {
		t.Errorf("uptime-kuma: %+v", st)
	}
}
