package main

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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Test vector from GitHub's "Validating webhook deliveries" documentation.
func TestValidSignature(t *testing.T) {
	secret, body := "It's a Secret to Everybody", []byte("Hello, World!")
	good := "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
	if !validSignature(secret, body, good) {
		t.Error("assinatura válida rejeitada")
	}
	for _, h := range []string{"", good[7:], "sha256=zz", "sha1=" + good[7:], strings.Replace(good, "7", "8", 1)} {
		if validSignature(secret, body, h) {
			t.Errorf("assinatura inválida aceita: %q", h)
		}
	}
	if validSignature("outro", body, good) {
		t.Error("segredo errado aceito")
	}
}

func TestGithubSlug(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/Octo/Hello.git": "octo/hello",
		"git@github.com:octo/hello":         "octo/hello",
		"ssh://git@github.com/octo/hello/":  "octo/hello",
		"https://github.com/octo/hello":     "octo/hello",
	} {
		if got, ok := githubSlug(in); !ok || got != want {
			t.Errorf("%q => %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"https://gitlab.com/a/b", "https://github.com/a", "https://github.com/a/b/c"} {
		if _, ok := githubSlug(in); ok {
			t.Errorf("%q não deveria casar", in)
		}
	}
}

func testApp(t *testing.T, api string) (*GitHubApp, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	f := filepath.Join(t.TempDir(), "k.pem")
	if err := os.WriteFile(f, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGitHubApp("12345", f)
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
	if _, err := g.InstallationToken(context.Background(), "octo", "outro"); err != errNotInstalled {
		t.Fatalf("esperava errNotInstalled, veio %v", err)
	}
}
