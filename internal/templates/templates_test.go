package templates

import (
	"strings"
	"testing"
)

// Every entry must be installable as an ordinary app: valid, single-line commands, unique ids.
func TestCatalogIsValid(t *testing.T) {
	seen := map[string]bool{}
	for _, tp := range All() {
		if err := Validate(tp); err != nil {
			t.Error(err)
		}
		if seen[tp.ID] {
			t.Errorf("id repetido: %s", tp.ID)
		}
		seen[tp.ID] = true
		for _, c := range []string{tp.Build, tp.Start} {
			if len(c) > 500 || strings.ContainsAny(c, "\r\n") {
				t.Errorf("%s: comando precisa de uma linha só e até 500 caracteres: %q", tp.ID, c)
			}
		}
		for _, m := range tp.Mounts {
			if m.Name == "" || m.Target == "" {
				t.Errorf("%s: montagem incompleta", tp.ID)
			}
		}
		// Whatever a start line reads from the storage must be declared.
		if strings.Contains(tp.Start, "$VOLUME_DATA") && len(tp.Mounts) == 0 {
			t.Errorf("%s usa $VOLUME_DATA sem montagem", tp.ID)
		}
	}
	if _, ok := Get("n8n"); !ok {
		t.Error("o n8n precisa estar no catálogo")
	}
}

func TestInstantiate(t *testing.T) {
	n8n, _ := Get("n8n")
	p, err := n8n.Instantiate(Input{Name: "n8n", Domain: "flows.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Repo != "template:n8n" || p.Build != n8n.Build || len(p.Mounts) != 1 {
		t.Errorf("plano: %+v", p)
	}
	for _, want := range []string{"TPL_VERSION=latest", "WEBHOOK_URL=https://flows.example.com/", "N8N_PROXY_HOPS=1", "N8N_ENCRYPTION_KEY="} {
		if !strings.Contains(p.Env, want) {
			t.Errorf("faltou %q em:\n%s", want, p.Env)
		}
	}
	if strings.Contains(p.Env, "N8N_SECURE_COOKIE") {
		t.Error("com domínio o cookie seguro deve continuar ligado")
	}
	// Without a domain the cookie must work over plain http, and no webhook URL is invented.
	q, _ := n8n.Instantiate(Input{Name: "n8n"})
	if !strings.Contains(q.Env, "N8N_SECURE_COOKIE=false") || strings.Contains(q.Env, "WEBHOOK_URL") {
		t.Errorf("sem domínio:\n%s", q.Env)
	}
	// Secrets are random per install and never the same twice.
	a, b := keyOf(p.Env, "N8N_ENCRYPTION_KEY"), keyOf(q.Env, "N8N_ENCRYPTION_KEY")
	if len(a) != 64 || a == b {
		t.Errorf("segredo fraco ou repetido: %q %q", a, b)
	}
	// Git-based: the version becomes the branch/tag.
	kuma, _ := Get("uptime-kuma")
	k, err := kuma.Instantiate(Input{Name: "status", Version: "2.5.8"})
	if err != nil || k.Branch != "2.5.8" || k.Repo != kuma.Repo {
		t.Errorf("uptime-kuma: %+v %v", k, err)
	}
}

func keyOf(env, key string) string {
	for _, l := range strings.Split(env, "\n") {
		if v, ok := strings.CutPrefix(l, key+"="); ok {
			return v
		}
	}
	return ""
}

func TestVersionIsInjectionSafe(t *testing.T) {
	n8n, _ := Get("n8n")
	for _, v := range []string{"1.0;rm -rf /", "$(id)", "`id`", "a b", "../x", "-rf", "x\nBAD=1", "1..2", strings.Repeat("1", 41)} {
		if _, err := n8n.Instantiate(Input{Name: "x", Version: v}); err == nil {
			t.Errorf("versão %q deveria ser recusada", v)
		}
	}
	for _, v := range []string{"latest", "2.45.7", "v1.2.3-rc.1", "28.3.0"} {
		if _, err := n8n.Instantiate(Input{Name: "x", Version: v}); err != nil {
			t.Errorf("versão %q deveria valer: %v", v, err)
		}
	}
}

func TestForRepo(t *testing.T) {
	if tp, ok := ForRepo("template:gitea"); !ok || tp.ID != "gitea" {
		t.Error("referência template:")
	}
	if tp, ok := ForRepo("https://github.com/louislam/uptime-kuma.git"); !ok || tp.ID != "uptime-kuma" {
		t.Error("repositório de template git")
	}
	for _, r := range []string{"template:nao-existe", "https://github.com/o/r", "template:"} {
		if _, ok := ForRepo(r); ok {
			t.Errorf("%q não é de template", r)
		}
	}
}
