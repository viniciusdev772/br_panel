package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeServer(t *testing.T) {
	for _, tc := range []struct {
		in, base string
		plain    bool
		bad      bool
	}{
		{"painel.example.com", "https://painel.example.com", false, false},
		{"https://painel.example.com/", "https://painel.example.com", false, false},
		{"203.0.113.7", "http://203.0.113.7:8080", true, false},
		{"203.0.113.7:9000", "http://203.0.113.7:9000", true, false},
		{"http://203.0.113.7:8080", "http://203.0.113.7:8080", true, false},
		{"localhost", "http://localhost:8080", false, false},
		{"127.0.0.1:8080", "http://127.0.0.1:8080", false, false},
		{"https://203.0.113.7:8443", "https://203.0.113.7:8443", false, false},
		{"  painel.example.com/caminho?x=1 ", "https://painel.example.com", false, false},
		{"", "", false, true},
		{"ftp://x.example.com", "", false, true},
		{"http://", "", false, true},
	} {
		base, plain, err := normalizeServer(tc.in)
		if (err != nil) != tc.bad || base != tc.base || plain != tc.plain {
			t.Errorf("%q -> %q plain=%v err=%v; quero %q plain=%v bad=%v", tc.in, base, plain, err, tc.base, tc.plain, tc.bad)
		}
	}
}

// fakePanel answers like the real one for the few routes the remote tests touch.
func fakePanel(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"token inválido ou revogado"}`))
			return
		}
		switch r.URL.Path {
		case "/api/overview":
			w.Write([]byte(`{"host":{"cpus":2},"res":{},"events":[]}`))
		case "/api/apps":
			w.Write([]byte(`[{"name":"loja","state":"active","status":"deployed","replicas":1,"repo":"https://github.com/o/r"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func runReal(t *testing.T, stdin string, args ...string) (code int, out, errs string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Run(args, strings.NewReader(stdin), &o, &e)
	return code, o.String(), e.String()
}

func TestLoginLogoutAndRemoteCommands(t *testing.T) {
	dir := t.TempDir()
	old := configDir
	configDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { configDir = old })
	const token = "brp_exemplo-de-token"
	srv := fakePanel(t, token)
	defer srv.Close()

	if code, out, _ := runReal(t, "", "whoami"); code != 0 || !strings.Contains(out, "modo: local") {
		t.Errorf("whoami sem login: %d %q", code, out)
	}
	// A wrong token is refused and nothing is saved.
	if code, _, errs := runReal(t, "brp_errado\n", "login", srv.URL, "--token-stdin"); code != 1 || !strings.Contains(errs, "recusou o token") {
		t.Errorf("token errado: %d %q", code, errs)
	}
	if _, ok := loadConfig(); ok {
		t.Fatal("login com token errado foi salvo")
	}
	if code, _, errs := runReal(t, "sem-prefixo\n", "login", srv.URL, "--token-stdin"); code != 2 || !strings.Contains(errs, "brp_") {
		t.Errorf("token sem prefixo: %d %q", code, errs)
	}
	// Plain http to a non-local address needs --allow-http (the token would cross the network).
	if code, _, errs := runReal(t, token+"\n", "login", "203.0.113.7", "--token-stdin"); code != 1 || !strings.Contains(errs, "--allow-http") {
		t.Errorf("http sem SSL: %d %q", code, errs)
	}
	if code, out, errs := runReal(t, token+"\n", "login", srv.URL, "--token-stdin"); code != 0 || !strings.HasPrefix(out, "OK: conectado") {
		t.Fatalf("login: %d %q %q", code, out, errs)
	}
	if c, ok := loadConfig(); !ok || c.Server != srv.URL || c.Token != token {
		t.Fatalf("config: %+v", c)
	}
	if code, out, _ := runReal(t, "", "whoami"); code != 0 || !strings.Contains(out, "modo: remoto") || strings.Contains(out, token) {
		t.Errorf("whoami remoto não pode mostrar o token inteiro: %q", out)
	}

	// Saved login: ordinary commands go to the remote server with the token.
	if code, out, errs := runReal(t, "", "apps", "list"); code != 0 || !strings.Contains(out, "loja") {
		t.Errorf("apps list remoto: %d %q %q", code, out, errs)
	}
	// The server forgets the token: the CLI says what to do.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	if code, _, errs := runReal(t, "", "apps", "list"); code != 1 || !strings.Contains(errs, "revogado") || !strings.Contains(errs, "brpanel login") {
		t.Errorf("token revogado: %d %q", code, errs)
	}
	// Server unreachable.
	srv.Close()
	if code, _, errs := runReal(t, "", "status"); code != 1 || !strings.Contains(errs, "não consegui conectar") {
		t.Errorf("servidor fora do ar: %d %q", code, errs)
	}

	if code, out, _ := runReal(t, "", "logout"); code != 0 || !strings.Contains(out, "removido") {
		t.Errorf("logout: %d %q", code, out)
	}
	if _, ok := loadConfig(); ok {
		t.Error("logout não apagou o login")
	}
	if code, out, _ := runReal(t, "", "logout"); code != 0 || !strings.Contains(out, "nenhum login") {
		t.Errorf("logout repetido: %q", out)
	}
}
