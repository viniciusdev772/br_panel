package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

// memStore is an in-memory Persistence standing in for the database.
type memStore struct{ rows map[string]session }

func (m *memStore) SaveSession(h, csrf string, exp time.Time) { m.rows[h] = session{csrf, exp} }
func (m *memStore) DeleteSession(h string)                    { delete(m.rows, h) }
func (m *memStore) DeleteAllSessions()                        { m.rows = map[string]session{} }
func (m *memStore) EachSession(fn func(string, string, time.Time)) {
	for h, s := range m.rows {
		fn(h, s.csrf, s.exp)
	}
}

func TestSessionsSurviveRestart(t *testing.T) {
	db := &memStore{rows: map[string]session{}}
	a := New()
	a.Persist(db)
	w := httptest.NewRecorder()
	a.Start(w, req("127.0.0.1:1", ""))
	cookie := w.Result().Cookies()[0]
	for h := range db.rows {
		if h == cookie.Value {
			t.Fatal("o id da sessão não pode ir para o banco, só o hash")
		}
	}

	b := New() // the panel restarted
	b.Persist(db)
	r := req("127.0.0.1:1", "")
	r.AddCookie(cookie)
	if csrf, ok := b.Session(r); !ok || csrf == "" {
		t.Fatal("a sessão deveria continuar válida depois do reinício")
	}

	b.Logout(httptest.NewRecorder(), r)
	if len(db.rows) != 0 {
		t.Error("logout deve apagar a sessão do banco")
	}
	c := New()
	c.Persist(db)
	if _, ok := c.Session(r); ok {
		t.Error("sessão encerrada não pode voltar")
	}
}

func TestRevokeAllClearsPersistedSessions(t *testing.T) {
	db := &memStore{rows: map[string]session{}}
	a := New()
	a.Persist(db)
	a.Start(httptest.NewRecorder(), req("127.0.0.1:1", ""))
	a.RevokeAll()
	if len(db.rows) != 0 {
		t.Error("trocar a senha deve apagar todas as sessões salvas")
	}
}

func TestExpiredPersistedSessionIsRejected(t *testing.T) {
	r := req("127.0.0.1:1", "")
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "abc"})
	db := &memStore{rows: map[string]session{idHash("abc"): {"x", time.Now().Add(-time.Minute)}}}
	a := New()
	a.Persist(db)
	if _, ok := a.Session(r); ok {
		t.Error("sessão expirada não pode valer")
	}
	if len(db.rows) != 0 {
		t.Error("a sessão expirada deve ser apagada do banco")
	}
}
