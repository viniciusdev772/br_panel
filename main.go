package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

// Config holds the only bootstrap options, given as flags: where to listen and
// where to keep data. Everything else lives in the SQLite database (see Settings).
type Config struct {
	Addr    string
	DataDir string
}

type Server struct {
	cfg   Config
	paths Paths
	store *Store
	auth  *Auth
	tmpl  map[string]*template.Template

	setMu sync.RWMutex
	set   Settings
	gh    *GitHubApp // nil unless a GitHub App is configured

	configured  atomic.Bool
	setupToken  string // one-time token required by /setup, empty once configured
	suggestedWH string // webhook secret suggested on the setup form
	dummyHash   string // verified against when the user does not exist (timing parity)

	pendMu  sync.Mutex
	pending map[string]bool
}

func main() {
	var cfg Config
	flag.StringVar(&cfg.Addr, "addr", ":8080", "endereço de escuta do painel (ex.: 127.0.0.1:8080 após configurar o SSL)")
	flag.StringVar(&cfg.DataDir, "data", "/var/lib/brpanel", "diretório de dados (banco SQLite, logs e apps)")
	flag.Parse()

	for _, d := range []string{"", "logs", "env", "apps"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, d), 0o750); err != nil {
			log.Fatal(err)
		}
	}
	store, err := OpenStore(filepath.Join(cfg.DataDir, "brpanel.db"))
	if err != nil {
		log.Fatal("banco de dados: ", err)
	}
	defer store.Close()
	if err := store.ImportLegacy(filepath.Join(cfg.DataDir, "apps.json")); err != nil {
		log.Fatal("migração do apps.json: ", err)
	}
	set, err := store.LoadSettings()
	if err != nil {
		log.Fatal(err)
	}

	srv := &Server{cfg: cfg, paths: Paths{Data: cfg.DataDir}, store: store, auth: NewAuth(), pending: map[string]bool{}}
	srv.dummyHash, _ = hashPassword(randomToken(16))
	if err := srv.applySettings(set); err != nil {
		log.Fatal(err)
	}
	if err := srv.loadTemplates(); err != nil {
		log.Fatal(err)
	}
	if srv.configured.Load() {
		if set.PanelDomain != "" {
			go srv.publishPanel()
		}
	} else {
		srv.announceSetup()
	}

	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go func() {
		log.Printf("BR Panel em http://%s (dados em %s)", cfg.Addr, cfg.DataDir)
		if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = hs.Shutdown(ctx)
}

// settings returns a snapshot of the current settings.
func (s *Server) settings() Settings {
	s.setMu.RLock()
	defer s.setMu.RUnlock()
	return s.set
}

func (s *Server) githubApp() *GitHubApp {
	s.setMu.RLock()
	defer s.setMu.RUnlock()
	return s.gh
}

func (s *Server) unitFile(app string) string {
	return filepath.Join(s.settings().UnitDir, unitName(app))
}

// applySettings persists set and makes it the live configuration.
func (s *Server) applySettings(set Settings) error {
	var gh *GitHubApp
	if set.GitHubAppID != "" && set.GitHubKey != "" {
		var err error
		if gh, err = ParseGitHubApp(set.GitHubAppID, []byte(set.GitHubKey)); err != nil {
			return err
		}
	}
	if err := s.store.SaveSettings(set); err != nil {
		return err
	}
	s.setMu.Lock()
	s.set, s.gh = set, gh
	s.setMu.Unlock()
	s.configured.Store(set.AdminHash != "")
	return nil
}

func (s *Server) loadTemplates() error {
	s.tmpl = map[string]*template.Template{}
	funcs := template.FuncMap{"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}}
	for _, page := range []string{"login", "apps", "app_form", "app", "github", "settings"} {
		t, err := template.New("").Funcs(funcs).ParseFS(webFS, "web/templates/layout.html", "web/templates/"+page+".html")
		if err != nil {
			return err
		}
		s.tmpl[page] = t
	}
	return nil
}

func (s *Server) routes() http.Handler {
	static, _ := fs.Sub(webFS, "web/static")
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.protected(s.handleLogout))
	mux.HandleFunc("GET /setup", s.handleSetupForm)
	mux.HandleFunc("POST /setup", s.handleSetup)
	mux.HandleFunc("GET /settings", s.protected(s.handleSettingsForm))
	mux.HandleFunc("POST /settings", s.protected(s.handleSettings))
	mux.HandleFunc("POST /webhooks/github", s.handleGitHubWebhook)
	mux.HandleFunc("GET /github", s.protected(s.handleGitHub))
	mux.HandleFunc("GET /{$}", s.protected(s.handleApps))
	mux.HandleFunc("GET /apps/new", s.protected(s.handleNewForm))
	mux.HandleFunc("POST /apps", s.protected(s.handleCreate))
	mux.HandleFunc("GET /apps/{name}", s.protected(s.handleApp))
	mux.HandleFunc("GET /apps/{name}/edit", s.protected(s.handleEditForm))
	mux.HandleFunc("POST /apps/{name}/edit", s.protected(s.handleEdit))
	mux.HandleFunc("GET /apps/{name}/log", s.protected(s.handleLog))
	mux.HandleFunc("POST /apps/{name}/{action}", s.protected(s.handleAction))
	return securityHeaders(s.gate(mux))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://cdnjs.cloudflare.com; "+
			"style-src 'self' https://cdnjs.cloudflare.com https://fonts.googleapis.com; font-src https://fonts.gstatic.com; "+
			"img-src 'self' data:; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		limit := int64(1 << 20)
		if r.URL.Path == "/webhooks/github" {
			limit = webhookMaxBody
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

type page struct {
	Title   string
	Authed  bool
	CSRF    string
	Flash   string
	Error   string
	Apps    []App
	App     App
	States  map[string]string
	State   string
	Editing bool
	SSL     bool
	GH      ghStatus

	Setup     bool
	Set       Settings
	HasKey    bool
	HasSecret bool
	Checks    []Check
	Form      map[string]string // submitted values to refill on errors
}

type ghStatus struct {
	WebhookEnabled bool
	AppEnabled     bool
	WebhookURL     string
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, p page) {
	p.CSRF, p.Authed = s.auth.Session(r)
	if p.Flash == "" {
		p.Flash = r.URL.Query().Get("msg")
	}
	var buf strings.Builder
	if err := s.tmpl[name].ExecuteTemplate(&buf, "layout", p); err != nil {
		log.Printf("template %s: %v", name, err)
		http.Error(w, "erro interno", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(buf.String()))
}

// protected requires a session and, for POST, a valid CSRF token.
func (s *Server) protected(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		csrf, ok := s.auth.Session(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost && !constEqual(r.PostFormValue("csrf"), csrf) {
			http.Error(w, "token CSRF inválido", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "login", page{Title: "Entrar"})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.auth.locked(ip) {
		w.WriteHeader(http.StatusTooManyRequests)
		s.render(w, r, "login", page{Title: "Entrar", Error: "Muitas tentativas. Aguarde um minuto."})
		return
	}
	set := s.settings()
	username := strings.TrimSpace(r.PostFormValue("username"))
	hash := set.AdminHash
	if !constEqual(username, set.AdminUser) {
		hash = s.dummyHash // same work whether or not the user exists
	}
	pw := r.PostFormValue("password")
	if len(pw) > maxPassword || !verifyPassword(pw, hash) || hash == s.dummyHash {
		s.auth.Fail(ip)
		w.WriteHeader(http.StatusUnauthorized)
		s.render(w, r, "login", page{Title: "Entrar", Error: "Usuário ou senha incorretos."})
		return
	}
	s.auth.Start(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// gate sends everything to /setup until the first-run wizard has been completed,
// and hides the wizard afterwards.
func (s *Server) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if s.configured.Load() {
			if p == "/setup" {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
		} else if p != "/setup" && !strings.HasPrefix(p, "/static/") {
			if p == "/webhooks/github" {
				http.NotFound(w, r)
				return
			}
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.Logout(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
