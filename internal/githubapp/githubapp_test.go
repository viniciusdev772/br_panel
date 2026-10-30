package githubapp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Test vector from GitHub's "Validating webhook deliveries" documentation.
func TestValidSignature(t *testing.T) {
	secret, body := "It's a Secret to Everybody", []byte("Hello, World!")
	good := "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
	if !ValidSignature(secret, body, good) {
		t.Error("assinatura válida rejeitada")
	}
	for _, h := range []string{"", good[7:], "sha256=zz", "sha1=" + good[7:], strings.Replace(good, "7", "8", 1)} {
		if ValidSignature(secret, body, h) {
			t.Errorf("assinatura inválida aceita: %q", h)
		}
	}
	if ValidSignature("outro", body, good) {
		t.Error("segredo errado aceito")
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/Octo/Hello.git": "octo/hello",
		"git@github.com:octo/hello":         "octo/hello",
		"ssh://git@github.com/octo/hello/":  "octo/hello",
		"https://github.com/octo/hello":     "octo/hello",
	} {
		if got, ok := Slug(in); !ok || got != want {
			t.Errorf("%q => %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"https://gitlab.com/a/b", "https://github.com/a", "https://github.com/a/b/c"} {
		if _, ok := Slug(in); ok {
			t.Errorf("%q não deveria casar", in)
		}
	}
}

func testApp(t *testing.T, api string) (*Client, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	g, err := Parse("12345", pemBytes)
	if err != nil {
		t.Fatal(err)
	}
	g.api = api
	return g, key
}

func verifyJWT(t *testing.T, tok string, pub *rsa.PublicKey) {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt malformado: %q", tok)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("assinatura do jwt: %v", err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Iss      string `json:"iss"`
		Iat, Exp int64
	}
	_ = json.Unmarshal(raw, &c)
	if c.Iss != "12345" || c.Exp-c.Iat > 600 || c.Exp <= time.Now().Unix() {
		t.Fatalf("claims inválidas: %+v", c)
	}
}

func TestInstallationToken(t *testing.T) {
	var key *rsa.PrivateKey
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyJWT(t, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), &key.PublicKey)
		if r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Error("faltou X-GitHub-Api-Version")
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/octo/hello/installation":
			_, _ = w.Write([]byte(`{"id":77}`))
		case "POST /app/installations/77/access_tokens":
			var b struct {
				Repositories []string          `json:"repositories"`
				Permissions  map[string]string `json:"permissions"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			if len(b.Repositories) != 1 || b.Repositories[0] != "hello" || b.Permissions["contents"] != "read" {
				t.Errorf("escopo do token: %+v", b)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_test"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	g, k := testApp(t, srv.URL)
	key = k
	tok, err := g.InstallationToken(context.Background(), "octo", "hello")
	if err != nil || tok != "ghs_test" {
		t.Fatalf("%q %v", tok, err)
	}
	if _, err := g.InstallationToken(context.Background(), "octo", "outro"); err != ErrNotInstalled {
		t.Fatalf("esperava ErrNotInstalled, veio %v", err)
	}
}

func TestManifestAndConvert(t *testing.T) {
	m, err := Manifest("BR Panel x", "https://p.example.com")
	if err != nil || !strings.Contains(m, `"url":"https://p.example.com/webhooks/github"`) || !strings.Contains(m, `"redirect_url":"https://p.example.com/github/app/callback"`) ||
		!strings.Contains(m, `"contents":"read"`) || !strings.Contains(m, `"push"`) {
		t.Fatalf("%v %s", err, m)
	}
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		if r.Header.Get("Authorization") != "" {
			t.Error("conversão não deve enviar credenciais")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42,"slug":"br-panel-x","pem":"-----BEGIN-----","webhook_secret":"abcdef0123456789abcd"}`))
	}))
	defer srv.Close()
	c, err := ConvertManifest(context.Background(), srv.URL, "abc123")
	if err != nil || c.ID != 42 || c.Slug != "br-panel-x" || path != "POST /app-manifests/abc123/conversions" {
		t.Fatalf("%+v %v %s", c, err, path)
	}
	for _, bad := range []string{"", "../x", "a/b", "a b", strings.Repeat("a", 101)} {
		if _, err := ConvertManifest(context.Background(), srv.URL, bad); err == nil {
			t.Errorf("código %q aceito", bad)
		}
	}
}

func TestRepositories(t *testing.T) {
	var key *rsa.PrivateKey
	repo := func(name string) map[string]any {
		return map[string]any{"full_name": name, "clone_url": "https://github.com/" + name + ".git", "default_branch": "main", "private": true}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/app/installations":
			verifyJWT(t, auth, &key.PublicKey)
			_, _ = w.Write([]byte(`[{"id":1},{"id":2}]`))
		case strings.HasPrefix(r.URL.Path, "/app/installations/") && strings.HasSuffix(r.URL.Path, "/access_tokens"):
			verifyJWT(t, auth, &key.PublicKey)
			var b struct {
				Repositories []string          `json:"repositories"`
				Permissions  map[string]string `json:"permissions"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			if len(b.Repositories) != 0 || len(b.Permissions) != 1 || b.Permissions["metadata"] != "read" {
				t.Errorf("o token de listagem deve pedir só metadata:read: %+v", b)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_inst` + strings.Split(r.URL.Path, "/")[3] + `"}`))
		case r.URL.Path == "/installation/repositories":
			page := r.URL.Query().Get("page")
			var repos []map[string]any
			switch {
			case auth == "ghs_inst1" && page == "1": // a full page forces a second request
				for i := 0; i < repoPageSize; i++ {
					repos = append(repos, repo("octo/r"+strings.Repeat("a", i%5)+string(rune('a'+i%26))+strings.Repeat("z", i/26)))
				}
			case auth == "ghs_inst1" && page == "2":
				repos = []map[string]any{repo("octo/ultimo")}
			case auth == "ghs_inst2" && page == "1":
				repos = []map[string]any{repo("Acme/Primeiro")}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"repositories": repos})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	g, k := testApp(t, srv.URL)
	key = k
	repos, err := g.Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != repoPageSize+2 {
		t.Fatalf("repositórios = %d", len(repos))
	}
	if repos[0].FullName != "Acme/Primeiro" || repos[0].CloneURL != "https://github.com/Acme/Primeiro.git" || repos[0].DefaultBranch != "main" || !repos[0].Private {
		t.Errorf("primeiro = %+v (ordenação sem diferenciar maiúsculas)", repos[0])
	}
	if last := repos[len(repos)-1]; last.FullName != "octo/ultimo" {
		t.Errorf("último = %+v", last)
	}
}
