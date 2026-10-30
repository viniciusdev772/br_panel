package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// publishedPanel makes the panel look published on example.com (the Host httptest uses).
func publishedPanel(t *testing.T, s *Server, domain string) {
	t.Helper()
	set := s.settings()
	set.PanelDomain, set.CertDir = domain, t.TempDir()
	if err := os.MkdirAll(filepath.Join(set.CertDir, domain), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set.CertDir, domain, "fullchain.pem"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.applySettings(set); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubSetupRoutesRequireSessionAndCSRF(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	for _, tc := range []struct{ method, path string }{{"GET", "/github/app/new"}, {"POST", "/github/domain"}} {
		w := do(h, tc.method, tc.path, url.Values{"domain": {"p.example.com"}})
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Errorf("%s %s sem sessão: %d %s", tc.method, tc.path, w.Code, w.Header().Get("Location"))
		}
	}
	if w := do(h, "POST", "/github/domain", url.Values{"domain": {"p.example.com"}}, c); w.Code != http.StatusForbidden {
		t.Errorf("sem CSRF: %d", w.Code)
	}
	csrf := csrfOf(t, h, "/github", c)
	w := do(h, "POST", "/github/domain", url.Values{"csrf": {csrf}, "domain": {"p.example.com"}, "acme_email": {"a@example.com"}}, c)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "Configure a Cloudflare") {
		t.Errorf("sem Cloudflare: %d", w.Code)
	}
	if s.settings().PanelDomain != "" {
		t.Error("domínio não deve ser salvo sem Cloudflare")
	}
}

func TestGitHubAppNewPage(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if w := do(h, "GET", "/github/app/new", nil, c); w.Code != http.StatusConflict {
		t.Errorf("sem domínio: %d", w.Code)
	}
	publishedPanel(t, s, "outro.example.org") // Host do teste é example.com
	if w := do(h, "GET", "/github/app/new", nil, c); w.Code != http.StatusConflict {
		t.Errorf("fora do domínio do painel: %d", w.Code)
	}
	publishedPanel(t, s, "example.com")
	w := do(h, "GET", "/github/app/new", nil, c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `name="manifest"`) || !strings.Contains(w.Body.String(), "https://github.com/settings/apps/new?state=") {
		t.Fatalf("página: %d %s", w.Code, w.Body.String())
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self' https://github.com") {
		t.Errorf("CSP = %q", csp)
	}
	if do(h, "GET", "/github", nil, c).Header().Get("Content-Security-Policy") == w.Header().Get("Content-Security-Policy") {
		t.Error("o CSP relaxado deve valer só na página de criação")
	}
	if s.ghState == "" {
		t.Error("state não foi guardado")
	}
}

func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app-manifests/abc123/conversions" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "slug": "br-panel-teste", "pem": pemKey, "webhook_secret": "0123456789abcdef0123"})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGitHubCallback(t *testing.T) {
	s, h := newTestServer(t)
	setup(t, s, h, nil)
	s.ghAPI = fakeGitHub(t).URL
	cb := "/github/app/callback?code=abc123&state="

	// No pending state, wrong state and expired state are all refused without side effects.
	if w := do(h, "GET", cb+"qualquer", nil); w.Code != http.StatusBadRequest {
		t.Errorf("sem state pendente: %d", w.Code)
	}
	s.ghState, s.ghStateExp = "estado-certo", time.Now().Add(time.Minute)
	if w := do(h, "GET", cb+"estado-errado", nil); w.Code != http.StatusBadRequest {
		t.Errorf("state errado: %d", w.Code)
	}
	s.ghStateExp = time.Now().Add(-time.Second)
	if w := do(h, "GET", cb+"estado-certo", nil); w.Code != http.StatusBadRequest {
		t.Errorf("state expirado: %d", w.Code)
	}
	if s.settings().GitHubAppID != "" || s.githubApp() != nil {
		t.Fatal("nada deveria ter sido salvo")
	}

	s.ghState, s.ghStateExp = "estado-certo", time.Now().Add(time.Minute)
	w := do(h, "GET", cb+"estado-certo", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "https://github.com/apps/br-panel-teste/installations/new") {
		t.Fatalf("callback: %d %s", w.Code, w.Body.String())
	}
	set := s.settings()
	if set.GitHubAppID != "42" || set.GitHubAppSlug != "br-panel-teste" || set.GitHubWebhookSecret != "0123456789abcdef0123" || s.githubApp() == nil {
		t.Errorf("configurações = %+v", set)
	}
	if w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("o callback não deve vazar code/state por Referer")
	}
	if w := do(h, "GET", cb+"estado-certo", nil); w.Code != http.StatusBadRequest {
		t.Errorf("state reutilizado: %d", w.Code)
	}
}

func TestGitHubPublishRetryRoute(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if w := do(h, "POST", "/github/publish", url.Values{}); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("sem sessão: %d", w.Code)
	}
	if w := do(h, "POST", "/github/publish", url.Values{}, c); w.Code != http.StatusForbidden {
		t.Errorf("sem CSRF: %d", w.Code)
	}
	csrf := csrfOf(t, h, "/github", c)
	if w := do(h, "POST", "/github/publish", url.Values{"csrf": {csrf}}, c); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("sem domínio: %d", w.Code)
	}
}

func TestGitHubPageShowsPublicationState(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	set := s.settings()
	set.PanelDomain, set.CertDir = "p.example.com", t.TempDir() // no certificate
	if err := s.applySettings(set); err != nil {
		t.Fatal(err)
	}
	s.pubMu.Lock()
	s.pubRunning = true
	s.pubMu.Unlock()
	body := do(h, "GET", "/github", nil, c).Body.String()
	if !strings.Contains(body, `data-refresh="5"`) || strings.Contains(body, "Tentar de novo") {
		t.Errorf("publicando: deve atualizar sozinha e sem botão\n%s", body)
	}
	s.pubMu.Lock()
	s.pubRunning, s.pubErr = false, "o certificado não foi emitido (Detail: NXDOMAIN)"
	s.pubMu.Unlock()
	body = do(h, "GET", "/github", nil, c).Body.String()
	if !strings.Contains(body, "Tentar de novo") || !strings.Contains(body, "NXDOMAIN") || strings.Contains(body, "data-refresh") {
		t.Errorf("falhou: deve mostrar o motivo e o botão\n%s", body)
	}
}

func TestTailBufReason(t *testing.T) {
	var b tailBuf
	_, _ = b.Write([]byte("Requesting a certificate\n  Detail: DNS problem: NXDOMAIN\nSome challenges have failed.\n"))
	if got := b.reason(); got != "Detail: DNS problem: NXDOMAIN" {
		t.Errorf("reason = %q", got)
	}
	var c tailBuf
	_, _ = c.Write([]byte("a\n\nultima linha\n"))
	if got := c.reason(); got != "ultima linha" {
		t.Errorf("reason = %q", got)
	}
	_, _ = c.Write([]byte(strings.Repeat("x", 20000)))
	if len(c.b) > 8192 {
		t.Errorf("buffer sem limite: %d", len(c.b))
	}
}
