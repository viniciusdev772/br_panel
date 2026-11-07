package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/ai"
)

func TestSecretName(t *testing.T) {
	for _, n := range []string{".env", ".env.local", ".env.production", "config/id_rsa", "keys/server.pem", "tls.key", ".npmrc", "credentials.json", "secrets.yml"} {
		if !secretName(n) {
			t.Errorf("%s deveria ser tratado como segredo", n)
		}
	}
	for _, n := range []string{".env.example", ".env.sample", "package.json", "src/server.js", "README.md", "monkey.js"} {
		if secretName(n) {
			t.Errorf("%s não é segredo", n)
		}
	}
}

func TestInsideStaysInProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "a.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../x", "src/../../x", "/etc/passwd", "src/\x00"} {
		if _, err := inside(dir, bad); err == nil {
			t.Errorf("%q deveria ser recusado", bad)
		}
	}
	if p, err := inside(dir, "src/a.txt"); err != nil || !strings.HasSuffix(filepath.ToSlash(p), "src/a.txt") {
		t.Errorf("caminho válido recusado: %v %v", p, err)
	}
	if _, err := inside(dir, "src/nao-existe"); err == nil {
		t.Error("arquivo inexistente deveria dar erro")
	}
	// a link that points outside the project must not be followed
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "segredo"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "atalho")); err != nil {
		t.Skip("sem permissão para criar links neste sistema:", err)
	}
	if _, err := inside(dir, "atalho/segredo"); err == nil {
		t.Error("um link para fora da pasta deveria ser recusado")
	}
}

func TestExtraToolsAreKnownToThePrompt(t *testing.T) {
	seen := map[string]bool{}
	prompt := ai.SystemPrompt("")
	for _, tl := range aiExtraTools() {
		if tl.Confirm {
			t.Errorf("%s: as ferramentas extras só leem, não pedem aprovação", tl.Name)
		}
		if seen[tl.Name] {
			t.Errorf("ferramenta repetida: %s", tl.Name)
		}
		seen[tl.Name] = true
		if !strings.Contains(prompt, tl.Name) {
			t.Errorf("o prompt de sistema não menciona %s", tl.Name)
		}
		for _, c := range ai.Tools() {
			if c.Name == tl.Name {
				t.Errorf("%s colide com um comando da CLI", tl.Name)
			}
		}
	}
}

func TestAIDetectNeedsConfiguredAI(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	form := url.Values{"csrf": {csrfOf(t, h, "/apps/new", c)}, "repo": {"https://github.com/octocat/Hello-World.git"}, "ai": {"1"}}
	w := do(h, "POST", "/apps/detect", form, c)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "Configure a IA") {
		t.Fatalf("sem IA configurada deveria recusar com orientação: %d %s", w.Code, w.Body.String())
	}
}
