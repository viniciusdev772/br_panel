package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
)

const testPassword = "senha-muito-longa-123"

func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"logs", "env", "apps", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := New(Config{Addr: "127.0.0.1:0", DataDir: dir}, st)
	if err != nil {
		t.Fatal(err)
	}
	s.announceSetup()
	return s, s.routes()
}

func do(h http.Handler, method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, body)
	r.RemoteAddr = "198.51.100.1:4000"
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func sessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == "brpanel_session" {
			return c
		}
	}
	return nil
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// setup completes the first-run wizard and returns the admin session cookie.
func setup(t *testing.T, s *Server, h http.Handler, extra url.Values) *http.Cookie {
	t.Helper()
	form := url.Values{"token": {s.setupToken}, "username": {"admin"}, "password": {testPassword}, "password2": {testPassword}}
	for k, v := range extra {
		form[k] = v
	}
	w := do(h, "POST", "/setup", form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	c := sessionCookie(w)
	if c == nil {
		t.Fatal("setup deveria abrir sessão")
	}
	return c
}

func csrfOf(t *testing.T, h http.Handler, path string, c *http.Cookie) string {
	t.Helper()
	m := csrfRe.FindStringSubmatch(do(h, "GET", path, nil, c).Body.String())
	if m == nil {
		t.Fatalf("sem token CSRF em %s", path)
	}
	return m[1]
}

func TestFirstRunGate(t *testing.T) {
	_, h := newTestServer(t)
	for _, p := range []string{"/", "/login", "/apps/new", "/settings"} {
		if w := do(h, "GET", p, nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/setup" {
			t.Errorf("%s deveria ir para /setup: %d %s", p, w.Code, w.Header().Get("Location"))
		}
	}
	if w := do(h, "POST", "/webhooks/github", nil); w.Code != http.StatusNotFound {
		t.Errorf("webhook antes do setup: %d", w.Code)
	}
	if w := do(h, "GET", "/static/app.css", nil); w.Code != http.StatusOK {
		t.Errorf("estáticos devem servir no setup: %d", w.Code)
	}
	if w := do(h, "GET", "/setup", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Token de instalação") {
		t.Errorf("/setup: %d", w.Code)
	}
}

func TestSetupRequiresTokenAndStrongPassword(t *testing.T) {
	s, h := newTestServer(t)
	base := func() url.Values {
		return url.Values{"token": {s.setupToken}, "username": {"admin"}, "password": {testPassword}, "password2": {testPassword}}
	}
	bad := base()
	bad.Set("token", "errado")
	if w := do(h, "POST", "/setup", bad); w.Code != http.StatusForbidden {
		t.Errorf("token errado: %d", w.Code)
	}
	short := base()
	short.Set("password", "curta")
	short.Set("password2", "curta")
	if w := do(h, "POST", "/setup", short); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("senha curta: %d", w.Code)
	}
	mismatch := base()
	mismatch.Set("password2", "outra-senha-longa-123")
	if w := do(h, "POST", "/setup", mismatch); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("senhas diferentes: %d", w.Code)
	}
	if s.configured.Load() {
		t.Fatal("nada disso deveria concluir o setup")
	}
	setup(t, s, h, nil)
	if !s.configured.Load() || s.setupToken != "" {
		t.Error("setup concluído deve invalidar o token")
	}
	if w := do(h, "GET", "/setup", nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("/setup depois de configurado: %d %s", w.Code, w.Header().Get("Location"))
	}
	if _, err := os.Stat(filepath.Join(s.cfg.DataDir, "setup_token")); !os.IsNotExist(err) {
		t.Error("arquivo do token deve ser removido")
	}
}

func TestLoginAndCSRF(t *testing.T) {
	s, h := newTestServer(t)
	setup(t, s, h, nil)

	if w := do(h, "GET", "/", nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("sem sessão: %d", w.Code)
	}
	if w := do(h, "POST", "/login", url.Values{"username": {"admin"}, "password": {"errada"}}); w.Code != http.StatusUnauthorized {
		t.Errorf("senha errada: %d", w.Code)
	}
	if w := do(h, "POST", "/login", url.Values{"username": {"outro"}, "password": {testPassword}}); w.Code != http.StatusUnauthorized {
		t.Errorf("usuário errado: %d", w.Code)
	}
	w := do(h, "POST", "/login", url.Values{"username": {"admin"}, "password": {testPassword}})
	c := sessionCookie(w)
	if w.Code != http.StatusSeeOther || c == nil {
		t.Fatalf("login: %d", w.Code)
	}
	if w := do(h, "GET", "/", nil, c); w.Code != http.StatusOK {
		t.Errorf("dashboard: %d", w.Code)
	}
	// every POST needs the CSRF token
	if w := do(h, "POST", "/logout", url.Values{}, c); w.Code != http.StatusForbidden {
		t.Errorf("POST sem CSRF: %d", w.Code)
	}
	csrf := csrfOf(t, h, "/", c)
	if w := do(h, "POST", "/logout", url.Values{"csrf": {csrf}}, c); w.Code != http.StatusSeeOther {
		t.Errorf("logout com CSRF: %d", w.Code)
	}
}

func TestLoginLockout(t *testing.T) {
	s, h := newTestServer(t)
	setup(t, s, h, nil)
	for i := 0; i < 5; i++ {
		do(h, "POST", "/login", url.Values{"username": {"admin"}, "password": {"errada"}})
	}
	if w := do(h, "POST", "/login", url.Values{"username": {"admin"}, "password": {testPassword}}); w.Code != http.StatusTooManyRequests {
		t.Errorf("após 5 falhas até a senha certa deve ser bloqueada: %d", w.Code)
	}
}

func TestAppCRUDAndValidation(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	csrf := csrfOf(t, h, "/apps/new", c)

	bad := url.Values{"csrf": {csrf}, "repo": {"ext::sh -c id"}, "start_cmd": {"x"}}
	if w := do(h, "POST", "/apps", bad, c); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("repositório inválido: %d", w.Code)
	}
	evil := url.Values{"csrf": {csrf}, "name": {"../x"}, "repo": {"https://github.com/o/r"}}
	if w := do(h, "POST", "/apps", evil, c); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("nome inválido: %d", w.Code)
	}
	if _, ok := s.store.Get("x"); ok || len(s.store.List()) != 0 {
		t.Error("nada deveria ter sido criado")
	}
	if w := do(h, "GET", "/apps/..%2fetc", nil, c); w.Code != http.StatusNotFound {
		t.Errorf("path traversal: %d", w.Code)
	}
	if w := do(h, "GET", "/apps/inexistente", nil, c); w.Code != http.StatusNotFound {
		t.Errorf("app inexistente: %d", w.Code)
	}

	// create with only the repository: name and port are filled in; the deploy then
	// fails fast (no network/credentials in tests), which is fine for this check.
	ok := url.Values{"csrf": {csrf}, "repo": {"https://github.com/octocat/Meu-App.git"}, "start_cmd": {"./run"}, "build_cmd": {"true"}}
	if w := do(h, "POST", "/apps", ok, c); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/apps/meu-app" {
		t.Fatalf("criar: %d %s", w.Code, w.Header().Get("Location"))
	}
	app, found := s.store.Get("meu-app")
	if !found || app.Port < 3001 {
		t.Fatalf("app: %+v", app)
	}
	waitIdle(t, s, "meu-app")

	// duplicate domain is rejected
	dom := url.Values{"csrf": {csrf}, "name": {"outro"}, "repo": {"https://github.com/o/r"}, "start_cmd": {"x"}, "domain": {"a.exemplo.com"}}
	if w := do(h, "POST", "/apps", dom, c); w.Code != http.StatusSeeOther {
		t.Fatalf("primeiro domínio: %d", w.Code)
	}
	waitIdle(t, s, "outro")
	dom.Set("name", "terceiro")
	if w := do(h, "POST", "/apps", dom, c); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("domínio duplicado: %d", w.Code)
	}
}

func waitIdle(t *testing.T, s *Server, name string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if a, ok := s.store.Get(name); ok && !a.Deploying() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("deploy de %s não terminou", name)
}

func sign(secret, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func TestWebhook(t *testing.T) {
	s, h := newTestServer(t)
	const secret = "segredo-do-webhook-123456"
	setup(t, s, h, url.Values{"webhook_secret": {secret}})

	if err := s.store.Create(store.App{Name: "auto", Repo: "https://github.com/Octo/Hello.git", Branch: "main", StartCmd: "x", AutoDeploy: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.Create(store.App{Name: "manual", Repo: "https://github.com/octo/hello", Branch: "main", StartCmd: "x"}); err != nil {
		t.Fatal(err)
	}
	post := func(event, sig, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
		r.RemoteAddr = "140.82.112.1:1"
		r.Header.Set("X-GitHub-Event", event)
		if sig != "" {
			r.Header.Set("X-Hub-Signature-256", sig)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	push := func(ref string) string {
		return `{"ref":"` + ref + `","after":"abcdef1234567","repository":{"full_name":"octo/hello"}}`
	}

	body := push("refs/heads/main")
	if w := post("push", "", body); w.Code != http.StatusUnauthorized {
		t.Errorf("sem assinatura: %d", w.Code)
	}
	if w := post("push", sign("outro-segredo-qualquer", body), body); w.Code != http.StatusUnauthorized {
		t.Errorf("assinatura errada: %d", w.Code)
	}
	if w := post("ping", sign(secret, body), body); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pong") {
		t.Errorf("ping: %d %q", w.Code, w.Body.String())
	}
	if w := post("issues", sign(secret, body), body); w.Code != http.StatusNoContent {
		t.Errorf("evento ignorado: %d", w.Code)
	}
	other := push("refs/heads/dev")
	if w := post("push", sign(secret, other), other); w.Code != http.StatusAccepted || !strings.HasPrefix(w.Body.String(), "0 deploy") {
		t.Errorf("outra branch: %d %q", w.Code, w.Body.String())
	}
	tag := push("refs/tags/v1")
	if w := post("push", sign(secret, tag), tag); w.Code != http.StatusNoContent {
		t.Errorf("tag: %d", w.Code)
	}
	// matches only the app that opted in (repo slug compared case-insensitively)
	w := post("push", sign(secret, body), body)
	if w.Code != http.StatusAccepted || !strings.HasPrefix(w.Body.String(), "1 deploy") {
		t.Fatalf("push válido: %d %q", w.Code, w.Body.String())
	}
	waitIdle(t, s, "auto")
	if a, _ := s.store.Get("manual"); a.Status != store.StatusIdle {
		t.Errorf("app sem deploy automático não deve ser tocado: %s", a.Status)
	}
}
