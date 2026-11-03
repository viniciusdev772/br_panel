// Package server is the panel's HTTP layer: first-run setup, authentication, app management,
// monitoring endpoints and the GitHub webhook.
package server

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/viniciusdev772/br_panel/internal/auth"
	"github.com/viniciusdev772/br_panel/internal/autoscale"
	"github.com/viniciusdev772/br_panel/internal/cli"
	"github.com/viniciusdev772/br_panel/internal/dbengine"
	"github.com/viniciusdev772/br_panel/internal/deploy"
	"github.com/viniciusdev772/br_panel/internal/githubapp"
	"github.com/viniciusdev772/br_panel/internal/monitor"
	"github.com/viniciusdev772/br_panel/internal/nodeenv"
	"github.com/viniciusdev772/br_panel/internal/settings"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/sysinfo"
	"github.com/viniciusdev772/br_panel/internal/systemd"
	"github.com/viniciusdev772/br_panel/internal/templates"
	"github.com/viniciusdev772/br_panel/internal/volume"
	"github.com/viniciusdev772/br_panel/web"
)

// Config holds the only bootstrap options, given as flags: where to listen and
// where to keep data. Everything else lives in the SQLite database (see settings.Settings).
type Config struct {
	Addr    string
	DataDir string
}

// Server wires the HTTP handlers to the store, the deployer and the live settings.
type Server struct {
	cfg   Config
	dep   *deploy.Deployer
	dbm   *dbengine.Manager
	node  *nodeenv.Installer
	store *store.Store
	auth  *auth.Auth
	tmpl  map[string]*template.Template

	setMu sync.RWMutex
	set   settings.Settings
	gh    *githubapp.Client // nil unless a GitHub App is configured

	ghMu       sync.Mutex // guards the pending GitHub App creation state
	ghState    string     // single-use "state" of the manifest flow in progress
	ghStateExp time.Time
	ghAPI      string // GitHub API base; empty outside tests

	pubMu      sync.Mutex // guards the panel publication status shown on /github
	pubRunning bool
	pubErr     string // why the last publication ended without a certificate

	configured atomic.Bool
	setupToken string // one-time token required by /setup, empty once configured
	dummyHash  string // verified against when the user does not exist (timing parity)
}

// New builds a Server on top of an open store, loading the saved settings.
func New(cfg Config, st *store.Store) (*Server, error) {
	set, err := st.LoadSettings()
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, store: st, auth: auth.New()}
	s.auth.Persist(st) // sessions survive a restart (every panel update restarts it)
	s.dep = deploy.New(st, cfg.DataDir, s.settings, s.githubApp)
	s.node = nodeenv.New(filepath.Join(cfg.DataDir, "nvm"), func() string { return s.settings().AppUser }, func() string { return s.settings().NodeVersion })
	s.dep.Node = s.node
	s.dbm = dbengine.New(st, cfg.DataDir, s.settings)
	s.dummyHash, _ = settings.HashPassword(auth.RandomToken(16))
	if err := s.applySettings(set); err != nil {
		return nil, err
	}
	if err := s.loadTemplates(); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(filepath.Join(cfg.DataDir, "tmp"))       // leftovers of interrupted detections
	_ = os.MkdirAll(filepath.Join(cfg.DataDir, "tmp"), 0o711) // traversable by the app user, see cmd/brpanel
	return s, nil
}

// Run starts the background workers and serves HTTP until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	go monitor.Run(ctx, s.store)
	go autoscale.Run(ctx, s.store, s.dep)
	go s.dbm.RunScheduler(ctx)
	go s.dep.RunVolumeScheduler(ctx)
	go s.serveCLI(ctx)
	if s.configured.Load() {
		if s.settings().PanelDomain != "" {
			go s.publishPanel()
		}
	} else {
		s.announceSetup()
	}

	hs := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	errc := make(chan error, 1)
	go func() {
		log.Printf("BR Panel em http://%s (dados em %s)", s.cfg.Addr, s.cfg.DataDir)
		errc <- hs.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// settings returns a snapshot of the current settings.
func (s *Server) settings() settings.Settings {
	s.setMu.RLock()
	defer s.setMu.RUnlock()
	return s.set
}

func (s *Server) githubApp() *githubapp.Client {
	s.setMu.RLock()
	defer s.setMu.RUnlock()
	return s.gh
}

// applySettings persists set and makes it the live configuration.
func (s *Server) applySettings(set settings.Settings) error {
	var gh *githubapp.Client
	if set.GitHubAppID != "" && set.GitHubKey != "" {
		var err error
		if gh, err = githubapp.Parse(set.GitHubAppID, []byte(set.GitHubKey)); err != nil {
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
	funcs := template.FuncMap{
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				m[kv[i].(string)] = kv[i+1]
			}
			return m
		},
		"bytes": humanBytes,
		"iso":   func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
		"join":  strings.Join,
		"split": strings.Split,
	}
	for _, page := range []string{"login", "apps", "app_form", "app", "github", "github_create", "github_done", "settings", "database", "database_new", "cli", "templates", "template"} {
		t, err := template.New("").Funcs(funcs).ParseFS(web.FS, "templates/layout.html", "templates/"+page+".html")
		if err != nil {
			return err
		}
		s.tmpl[page] = t
	}
	return nil
}

func (s *Server) routes() http.Handler {
	static, _ := fs.Sub(web.FS, "static")
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
	mux.HandleFunc("POST /github/domain", s.protected(s.handleGitHubDomain))
	mux.HandleFunc("POST /github/publish", s.protected(s.handleGitHubPublish))
	mux.HandleFunc("GET /github/app/new", s.protected(s.handleGitHubAppNew))
	mux.HandleFunc("GET /github/app/callback", s.handleGitHubCallback) // reached from github.com: validated by its single-use state
	mux.HandleFunc("GET /{$}", s.protected(s.handleApps))
	mux.HandleFunc("GET /apps/new", s.protected(s.handleNewForm))
	mux.HandleFunc("POST /apps", s.protected(s.handleCreate))
	mux.HandleFunc("GET /apps/{name}", s.protected(s.handleApp))
	mux.HandleFunc("GET /apps/{name}/edit", s.protected(s.handleEditForm))
	mux.HandleFunc("POST /apps/{name}/edit", s.protected(s.handleEdit))
	mux.HandleFunc("GET /apps/{name}/log", s.protected(s.handleLog))
	mux.HandleFunc("GET /apps/{name}/logs", s.protected(s.handleLogs))
	mux.HandleFunc("GET /apps/{name}/metrics", s.protected(s.handleMetrics))
	mux.HandleFunc("GET /apps/{name}/volume-backups/{file}", s.protected(s.handleVolumeDownload))
	mux.HandleFunc("POST /apps/{name}/site/{action}", s.protected(s.handleSite))
	mux.HandleFunc("GET /api/overview", s.protected(s.handleOverview))
	mux.HandleFunc("GET /api/apps", s.protected(s.handleAPIApps))
	mux.HandleFunc("GET /api/apps/{name}", s.protected(s.handleAPIApp))
	mux.HandleFunc("GET /api/databases", s.protected(s.handleAPIDatabases))
	mux.HandleFunc("GET /api/tokens", s.protected(s.handleAPITokens))
	mux.HandleFunc("GET /api/templates", s.protected(s.handleAPITemplates))
	mux.HandleFunc("GET /templates", s.protected(s.handleTemplates))
	mux.HandleFunc("GET /templates/{id}", s.protected(s.handleTemplateForm))
	mux.HandleFunc("POST /templates/{id}", s.protected(s.handleTemplateInstall))
	mux.HandleFunc("POST /cli/tokens", s.protected(s.handleTokenCreate))
	mux.HandleFunc("POST /cli/tokens/{id}/revoke", s.protected(s.handleTokenRevoke))
	mux.HandleFunc("GET /cli", s.protected(s.handleCLIPage))
	mux.HandleFunc("GET /cli/guide.txt", s.protected(s.handleCLIGuide))
	mux.HandleFunc("GET /api/github/repos", s.protected(s.handleGitHubRepos))
	mux.HandleFunc("POST /tools/node/install", s.protected(s.handleNodeInstall))
	mux.HandleFunc("GET /tools/node/status", s.protected(s.handleNodeStatus))
	mux.HandleFunc("POST /apps/detect", s.protected(s.handleDetect))
	mux.HandleFunc("POST /apps/{name}/env", s.protected(s.handleEnvSave))
	mux.HandleFunc("GET /databases/new", s.protected(s.handleDBNew))
	mux.HandleFunc("POST /databases", s.protected(s.handleDBCreate))
	mux.HandleFunc("POST /databases/install", s.protected(s.handleDBInstall))
	mux.HandleFunc("GET /databases/install/status", s.protected(s.handleInstallStatus))
	mux.HandleFunc("GET /databases/{name}", s.protected(s.handleDBPage))
	mux.HandleFunc("GET /databases/{name}/log", s.protected(s.handleDBLog))
	mux.HandleFunc("GET /databases/{name}/metrics", s.protected(s.handleDBMetrics))
	mux.HandleFunc("GET /databases/{name}/data", s.protected(s.handleDBData))
	mux.HandleFunc("GET /databases/{name}/backups/{file}", s.protected(s.handleDBBackupDownload))
	mux.HandleFunc("POST /databases/{name}/{action}", s.protected(s.handleDBAction))
	mux.HandleFunc("POST /apps/{name}/{action}", s.protected(s.handleAction))
	return securityHeaders(s.gate(s.cliAuth(mux)))
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
	Nav     string // active top-bar tab: arch | settings | github
	Authed  bool
	CSRF    string
	Flash   string
	Error   string
	Apps    []store.App
	App     store.App
	States  map[string]string
	State   string
	Info    systemd.ServiceInfo
	Host    sysinfo.HostStats
	Latest  map[string]store.Point
	Editing bool
	SSL     bool
	GH      ghStatus

	Bdg badge

	Cards  []card
	Edges  []edge
	Events []store.Event

	Kinds          []dbengine.KindStatus
	KindSel        dbengine.KindStatus
	DefaultName    string
	InstallRunning bool
	InstallErr     string
	DB             store.Database
	DBSpec         dbengine.Spec
	DBVersion      string
	Fwd            dbengine.ForwardInfo
	Conns          []dbengine.Connection
	Console        string
	Backups        []dbengine.BackupInfo
	Busy           string
	UsedBy         []string
	VarGroups      []varGroup

	Mounts         []store.Mount
	VolumeBackups  []volume.Info
	VolumeSchedule string
	VolumeBusy     string

	Tpls []templates.Template
	Tpl  templates.Template

	CLIGroups []cli.Group
	CLIPrompt string
	CLICount  int

	Tokens       []store.Token
	NewToken     string // shown once, right after creation
	NewTokenName string
	PanelHost    string

	SetupCode      string // setup only: code already known from the install link
	AppUserDefault string // setup only: system user submitted silently with the form
	Setup          bool
	Set            settings.Settings
	HasKey         bool
	HasSecret      bool
	HasCF          bool
	Checks         []settings.Check
	Form           map[string]string // submitted values to refill on errors
}

type ghStatus struct {
	WebhookEnabled bool
	AppEnabled     bool
	WebhookURL     string

	// One-click App creation (manifest flow).
	HasCloudflare bool
	PanelDomain   string
	PanelReady    bool // domain set and certificate issued
	OnPanelDomain bool // the browser is already on the panel domain
	NeedEmail     bool // no Let's Encrypt e-mail saved yet
	Publishing    bool // Nginx/SSL for the panel domain is being set up right now
	PublishErr    string
	InstallURL    string
	CreateURL     string
	Manifest      string
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, p page) {
	if viaCLI(r) { // the CLI wants the outcome, not the page
		writeJSON(w, map[string]string{"error": p.Error, "flash": p.Flash, "token": p.NewToken})
		return
	}
	p.CSRF, p.Authed = s.auth.Session(r)
	if p.Nav == "" {
		switch name {
		case "settings":
			p.Nav = "settings"
		case "github":
			p.Nav = "github"
		case "login":
		default:
			p.Nav = "arch"
		}
	}
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
		if r.Method == http.MethodPost && !auth.ConstEqual(r.PostFormValue("csrf"), csrf) {
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
	ip := auth.ClientIP(r)
	if s.auth.Locked(ip) {
		w.WriteHeader(http.StatusTooManyRequests)
		s.render(w, r, "login", page{Title: "Entrar", Error: "Muitas tentativas. Aguarde um minuto."})
		return
	}
	set := s.settings()
	username := strings.TrimSpace(r.PostFormValue("username"))
	hash := set.AdminHash
	if !auth.ConstEqual(username, set.AdminUser) {
		hash = s.dummyHash // same work whether or not the user exists
	}
	pw := r.PostFormValue("password")
	if len(pw) > settings.MaxPassword || !settings.VerifyPassword(pw, hash) || hash == s.dummyHash {
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

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
