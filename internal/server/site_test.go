package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

func flash(t *testing.T, loc string) string {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("msg")
}

// The site tab changes what nginx serves, so every action is POST + CSRF, input is validated and
// the password hash never reaches a page.
func TestAppSiteRoutes(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	for _, a := range []store.App{
		{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3001, Domain: "web.example.com"},
		{Name: "outra", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3002, Domain: "outra.example.com"},
		{Name: "semdominio", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3003},
	} {
		if err := s.store.Create(a); err != nil {
			t.Fatal(err)
		}
	}
	csrf := csrfOf(t, h, "/apps/web", c)
	post := func(app, action string, v url.Values) string {
		t.Helper()
		v.Set("csrf", csrf)
		w := do(h, "POST", "/apps/"+app+"/site/"+action, v, c)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("%s/%s: %d", app, action, w.Code)
		}
		return flash(t, w.Header().Get("Location"))
	}
	web := func() store.Web { a, _ := s.store.Get("web"); return a.Web }

	for _, action := range []string{"domain-add", "domain-delete", "auth-add", "auth-delete", "maintenance", "limits"} {
		if w := do(h, "POST", "/apps/web/site/"+action, url.Values{}, c); w.Code != http.StatusForbidden {
			t.Errorf("%s sem CSRF: %d", action, w.Code)
		}
	}
	if w := do(h, "POST", "/apps/web/site/domain-add", url.Values{"domain": {"x.example.com"}}, &http.Cookie{Name: "brpanel_session", Value: "invalida"}); !strings.Contains(w.Header().Get("Location"), "/login") {
		t.Error("sem sessão não pode alterar")
	}
	if w := do(h, "POST", "/apps/web/site/desconhecida", url.Values{"csrf": {csrf}}, c); w.Code != http.StatusNotFound {
		t.Errorf("ação desconhecida: %d", w.Code)
	}
	if got := web(); len(got.Domains) != 0 || len(got.Users) != 0 {
		t.Fatalf("nada deveria ter sido salvo: %+v", got)
	}

	// Domains: an alias and a redirect.
	if m := post("web", "domain-add", url.Values{"domain": {"Shop.Example.com"}, "kind": {"serve"}}); !strings.Contains(m, "Domínio adicionado") {
		t.Errorf("alias: %q", m)
	}
	if m := post("web", "domain-add", url.Values{"domain": {"www.web.example.com"}, "kind": {"redirect"}, "to": {"web.example.com"}, "permanent": {"on"}}); !strings.Contains(m, "Domínio adicionado") {
		t.Errorf("redirect: %q", m)
	}
	got := web()
	if len(got.Domains) != 2 || got.Domains[0].Name != "shop.example.com" || got.Domains[0].To != "" || got.Domains[1].To != "web.example.com" || !got.Domains[1].Permanent {
		t.Fatalf("domínios: %+v", got.Domains)
	}
	for name, v := range map[string]url.Values{
		"repetido":         {"domain": {"shop.example.com"}},
		"principal":        {"domain": {"web.example.com"}},
		"de outra app":     {"domain": {"outra.example.com"}},
		"injeção":          {"domain": {"x.example.com;return 200"}},
		"destino inválido": {"domain": {"y.example.com"}, "kind": {"redirect"}, "to": {"a.com/x"}},
		"sem destino":      {"domain": {"y.example.com"}, "kind": {"redirect"}},
		"para si mesmo":    {"domain": {"y.example.com"}, "kind": {"redirect"}, "to": {"y.example.com"}},
	} {
		if m := post("web", "domain-add", v); !strings.HasPrefix(m, "Não salvo") {
			t.Errorf("%s deveria ser recusado: %q", name, m)
		}
	}
	if m := post("semdominio", "domain-add", url.Values{"domain": {"z.example.com"}}); !strings.Contains(m, "domínio principal") {
		t.Errorf("sem domínio principal: %q", m)
	}
	if len(web().Domains) != 2 {
		t.Errorf("recusados não podem ser salvos: %+v", web().Domains)
	}
	// Another app cannot take an extra domain either.
	if m := post("outra", "domain-add", url.Values{"domain": {"shop.example.com"}}); !strings.HasPrefix(m, "Não salvo") {
		t.Errorf("domínio extra de outra app: %q", m)
	}

	// Password: only the hash is kept, and it never appears on the page.
	if m := post("web", "auth-add", url.Values{"user": {"ana"}, "password": {"senha-de-exemplo"}}); !strings.Contains(m, "Usuário salvo") {
		t.Errorf("auth-add: %q", m)
	}
	hash := web().Users[0].Hash
	if !strings.HasPrefix(hash, "$2y$") || strings.Contains(hash, "senha-de-exemplo") {
		t.Fatalf("hash = %q", hash)
	}
	for _, v := range []url.Values{
		{"user": {"a:b"}, "password": {"senha-de-exemplo"}},
		{"user": {"bia"}, "password": {"curta"}},
		{"user": {"bia\nroot"}, "password": {"senha-de-exemplo"}},
	} {
		if m := post("web", "auth-add", v); !strings.HasPrefix(m, "Não salvo") {
			t.Errorf("%v deveria ser recusado: %q", v, m)
		}
	}
	post("web", "auth-add", url.Values{"user": {"ana"}, "password": {"outra-senha-longa"}}) // changes the password
	if got := web(); len(got.Users) != 1 || got.Users[0].Hash == hash {
		t.Errorf("trocar a senha: %+v", got.Users)
	}

	// Maintenance and limits.
	post("web", "maintenance", url.Values{"state": {"on"}, "title": {"Voltamos já"}, "text": {"Atualizando <b>tudo</b>."}})
	if got := web(); !got.Maintenance || got.MaintTitle != "Voltamos já" {
		t.Errorf("manutenção: %+v", got)
	}
	if m := post("web", "maintenance", url.Values{"state": {"on"}, "title": {strings.Repeat("a", 200)}}); !strings.HasPrefix(m, "Não salvo") {
		t.Errorf("título longo: %q", m)
	}
	post("web", "limits", url.Values{"upload_mb": {"50"}, "read_timeout": {"120"}})
	if got := web(); got.MaxBodyMB != 50 || got.ReadTimeout != 120 {
		t.Errorf("limites: %+v", got)
	}
	for _, v := range []url.Values{{"upload_mb": {"-1"}}, {"upload_mb": {"abc"}}, {"read_timeout": {"3"}}, {"read_timeout": {"99999"}}} {
		if m := post("web", "limits", v); !strings.HasPrefix(m, "Não salvo") {
			t.Errorf("%v deveria ser recusado: %q", v, m)
		}
	}
	if got := web(); got.MaxBodyMB != 50 || got.ReadTimeout != 120 {
		t.Errorf("limites inválidos não podem mudar os salvos: %+v", got)
	}

	// The page lists everything and never prints the hash.
	page := do(h, "GET", "/apps/web", nil, c).Body.String()
	for _, want := range []string{"shop.example.com", "www.web.example.com", "ana", "ligada"} {
		if !strings.Contains(page, want) {
			t.Errorf("a página não mostra %q", want)
		}
	}
	if strings.Contains(page, "$2y$") || strings.Contains(page, web().Users[0].Hash) {
		t.Error("o hash da senha apareceu na página")
	}

	// Removing.
	post("web", "domain-delete", url.Values{"domain": {"shop.example.com"}})
	post("web", "auth-delete", url.Values{"user": {"ana"}})
	if got := web(); len(got.Domains) != 1 || len(got.Users) != 0 {
		t.Errorf("remoção: %+v", got)
	}
	if m := post("web", "auth-delete", url.Values{"user": {"ninguem"}}); !strings.HasPrefix(m, "Não salvo") {
		t.Errorf("remover quem não existe: %q", m)
	}
}
