// Package auth provides in-memory sessions, CSRF tokens and login throttling.
package auth

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

func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func ConstEqual(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

type session struct {
	csrf string
	exp  time.Time
}

type Auth struct {
	mu       sync.Mutex
	sessions map[string]session
	fails    map[string]*failState
}

type failState struct {
	n     int
	until time.Time
}

func New() *Auth {
	return &Auth{sessions: map[string]session{}, fails: map[string]*failState{}}
}

// ClientIP returns the peer address. X-Real-IP (set by our nginx vhost) is
// honoured only when the connection comes from loopback, so it cannot be spoofed remotely.
func ClientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
		if real := net.ParseIP(r.Header.Get("X-Real-IP")); real != nil {
			return real.String()
		}
	}
	return h
}

func (a *Auth) Locked(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.fails[ip]
	return f != nil && time.Now().Before(f.until)
}

// Fail records a failed attempt from ip, locking it out after maxFails.
func (a *Auth) Fail(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.fails[ip]
	if f == nil {
		f = &failState{}
		a.fails[ip] = f
	}
	f.n++
	if f.n >= maxFails {
		f.n, f.until = 0, time.Now().Add(lockFor)
	}
}

// Start clears ip's failures and opens a session (cookie Secure when served over HTTPS).
func (a *Auth) Start(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.fails, ClientIP(r))
	now := time.Now()
	for k, s := range a.sessions {
		if now.After(s.exp) {
			delete(a.sessions, k)
		}
	}
	id := RandomToken(32)
	a.sessions[id] = session{csrf: RandomToken(16), exp: now.Add(sessionTTL)}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: id, Path: "/", HttpOnly: true, Secure: IsHTTPS(r),
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds()),
	})
}

// IsHTTPS is true for direct TLS or when our own nginx (loopback) says the client used HTTPS.
func IsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(h)
	return err == nil && ip != nil && ip.IsLoopback() && r.Header.Get("X-Forwarded-Proto") == "https"
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.mu.Lock()
		delete(a.sessions, c.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: IsHTTPS(r)})
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

// RevokeAll ends every session (used after a password change).
func (a *Auth) RevokeAll() {
	a.mu.Lock()
	a.sessions = map[string]session{}
	a.mu.Unlock()
}
