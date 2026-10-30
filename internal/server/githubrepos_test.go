package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/githubapp"
)

func TestGitHubReposRoute(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if w := do(h, "GET", "/api/github/repos", nil); w.Code != http.StatusSeeOther {
		t.Errorf("sem sessão: %d", w.Code)
	}
	if w := do(h, "GET", "/api/github/repos", nil, c); w.Code != http.StatusNotFound {
		t.Errorf("sem GitHub App: %d", w.Code)
	}
	if body := do(h, "GET", "/apps/new", nil, c).Body.String(); strings.Contains(body, `id="repo-search"`) {
		t.Error("sem GitHub App o formulário não deve mostrar o seletor")
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	gh, err := githubapp.Parse("1", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err != nil {
		t.Fatal(err)
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app/installations":
			_, _ = w.Write([]byte(`[{"id":9}]`))
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_x"}`))
		case r.URL.Path == "/installation/repositories":
			_, _ = w.Write([]byte(`{"repositories":[{"full_name":"octo/hello","clone_url":"https://github.com/octo/hello.git","default_branch":"develop","private":true}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	gh.UseAPI(fake.URL)
	s.setMu.Lock()
	s.gh = gh
	s.setMu.Unlock()

	w := do(h, "GET", "/api/github/repos", nil, c)
	var got struct {
		Repos []githubapp.Repo `json:"repos"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.Repos) != 1 || got.Repos[0].DefaultBranch != "develop" || got.Repos[0].CloneURL != "https://github.com/octo/hello.git" {
		t.Fatalf("lista: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "ghs_x") {
		t.Error("a resposta não deve ser cacheada nem conter token")
	}
	if body := do(h, "GET", "/apps/new", nil, c).Body.String(); !strings.Contains(body, `id="repo-search"`) {
		t.Error("com GitHub App o formulário deve mostrar o seletor")
	}

	fake.Close() // GitHub unreachable: a clear error, not a hang or a 500
	if w := do(h, "GET", "/api/github/repos", nil, c); w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "Não foi possível listar") {
		t.Errorf("GitHub fora do ar: %d %s", w.Code, w.Body.String())
	}
}
