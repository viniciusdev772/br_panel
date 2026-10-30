package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookie = "brpanel_session"
	sessionTTL    = 12 * time.Hour
	maxFails      = 5
	lockFor       = time.Minute
)

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func passwordEqual(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

type session struct {
	csrf string
	exp  time.Time
}

type Auth struct {
	password string
	secure   bool

	mu       sync.Mutex
	sessions map[string]session
	fails    map[string]*failState
}

type failState struct {
	n     int
	until time.Time
}

func NewAuth(password string, secure bool) *Auth {
	return &Auth{password: password, secure: secure, sessions: map[string]session{}, fails: map[string]*failState{}}
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (a *Auth) locked(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.fails[ip]
	return f != nil && time.Now().Before(f.until)
}

// Login checks the password and, on success, sets a session cookie.
func (a *Auth) Login(w http.ResponseWriter, r *http.Request, pw string) bool {
	ip := clientIP(r)
	if a.locked(ip) {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !passwordEqual(pw, a.password) {
		f := a.fails[ip]
		if f == nil {
			f = &failState{}
			a.fails[ip] = f
		}
		f.n++
		if f.n >= maxFails {
			f.n, f.until = 0, time.Now().Add(lockFor)
		}
		return false
	}
	delete(a.fails, ip)
	now := time.Now()
	for k, s := range a.sessions {
		if now.After(s.exp) {
			delete(a.sessions, k)
		}
	}
	id := randomToken(32)
	a.sessions[id] = session{csrf: randomToken(16), exp: now.Add(sessionTTL)}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: id, Path: "/", HttpOnly: true, Secure: a.secure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds()),
	})
	return true
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.mu.Lock()
		delete(a.sessions, c.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secure})
}

// Session returns the CSRF token for a valid session.
func (a *Auth) Session(r *http.Request) (csrf string, ok bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, found := a.sessions[c.Value]
	if !found || time.Now().After(s.exp) {
		delete(a.sessions, c.Value)
		return "", false
	}
	return s.csrf, true
}
