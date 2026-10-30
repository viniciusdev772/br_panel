package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(remote, realIP string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	if realIP != "" {
		r.Header.Set("X-Real-IP", realIP)
	}
	return r
}

func TestClientIPTrustsProxyHeaderOnlyFromLoopback(t *testing.T) {
	if got := ClientIP(req("127.0.0.1:5000", "203.0.113.9")); got != "203.0.113.9" {
		t.Errorf("proxy local deve repassar o IP real: %s", got)
	}
	if got := ClientIP(req("198.51.100.7:5000", "203.0.113.9")); got != "198.51.100.7" {
		t.Errorf("cabeçalho forjado por cliente remoto deve ser ignorado: %s", got)
	}
	if got := ClientIP(req("127.0.0.1:5000", "lixo")); got != "127.0.0.1" {
		t.Errorf("IP inválido deve ser ignorado: %s", got)
	}
}

func TestIsHTTPS(t *testing.T) {
	r := req("127.0.0.1:1", "")
	r.Header.Set("X-Forwarded-Proto", "https")
	if !IsHTTPS(r) {
		t.Error("nginx local informando https")
	}
	r = req("198.51.100.7:1", "")
	r.Header.Set("X-Forwarded-Proto", "https")
	if IsHTTPS(r) {
		t.Error("cliente remoto não pode afirmar https")
	}
}

func TestLockoutAfterFailures(t *testing.T) {
	a := New()
	for i := 0; i < maxFails; i++ {
		if a.Locked("1.2.3.4") {
			t.Fatalf("bloqueou cedo demais (%d)", i)
		}
		a.Fail("1.2.3.4")
	}
	if !a.Locked("1.2.3.4") || a.Locked("5.6.7.8") {
		t.Error("só o IP que falhou deve ser bloqueado")
	}
}

func TestSessionLifecycle(t *testing.T) {
	a := New()
	w := httptest.NewRecorder()
	r := req("127.0.0.1:1", "")
	a.Start(w, r)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie: %+v", cookies)
	}
	r2 := req("127.0.0.1:1", "")
	r2.AddCookie(cookies[0])
	csrf, ok := a.Session(r2)
	if !ok || csrf == "" {
		t.Fatal("sessão válida")
	}
	a.RevokeAll()
	if _, ok := a.Session(r2); ok {
		t.Error("sessão deveria ter sido revogada")
	}
	if _, ok := a.Session(req("127.0.0.1:1", "")); ok {
		t.Error("sem cookie não há sessão")
	}
}

func TestConstEqualAndToken(t *testing.T) {
	if !ConstEqual("a", "a") || ConstEqual("a", "b") || ConstEqual("", "x") {
		t.Error("ConstEqual")
	}
	if a, b := RandomToken(8), RandomToken(8); len(a) != 16 || a == b {
		t.Errorf("RandomToken: %s %s", a, b)
	}
}
