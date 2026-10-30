package main

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

type Config struct {
	Addr     string
	DataDir  string
	UnitDir  string
	Password string
	AppUser  string
	Secure   bool

	NginxDir    string
	AcmeRoot    string
	CertDir     string
	AcmeEmail   string
	PanelDomain string

	GitHubAppID         string
	GitHubKeyFile       string
	GitHubWebhookSecret string
}

type Server struct {
	cfg   Config
	paths Paths
	store *Store
	auth  *Auth
	gh    *GitHubApp // nil unless PANEL_GITHUB_APP_ID/PANEL_GITHUB_KEY_FILE are set
	tmpl  map[string]*template.Template

	pendMu  sync.Mutex
	pending map[string]bool
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := Config{
		Addr:    env("PANEL_ADDR", "127.0.0.1:8080"),
		DataDir: env("PANEL_DATA", "/var/lib/brpanel"),
		UnitDir: env("PANEL_UNIT_DIR", "/etc/systemd/system"),
		AppUser: os.Getenv("PANEL_APP_USER"),
		Secure:  os.Getenv("PANEL_SECURE_COOKIE") == "1",

		NginxDir:    env("PANEL_NGINX_DIR", "/etc/nginx/conf.d"),
		AcmeRoot:    env("PANEL_ACME_ROOT", "/var/www/brpanel-acme"),
		CertDir:     env("PANEL_CERT_DIR", "/etc/letsencrypt/live"),
		AcmeEmail:   os.Getenv("PANEL_ACME_EMAIL"),
		PanelDomain: strings.ToLower(os.Getenv("PANEL_DOMAIN")),

		GitHubAppID:         os.Getenv("PANEL_GITHUB_APP_ID"),
		GitHubKeyFile:       os.Getenv("PANEL_GITHUB_KEY_FILE"),
		GitHubWebhookSecret: os.Getenv("PANEL_GITHUB_WEBHOOK_SECRET"),
	}
	if err := validateDomain(cfg.PanelDomain); err != nil {
		log.Fatal("PANEL_DOMAIN: ", err)
	}
	if cfg.AppUser != "" && !regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`).MatchString(cfg.AppUser) {
		log.Fatal("PANEL_APP_USER inválido")
	}
	for _, d := range []string{"", "logs", "env", "apps"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, d), 0o750); err != nil {
			log.Fatal(err)
		}
	}
	cfg.Password = loadPassword(cfg.DataDir)

	paths := Paths{Data: cfg.DataDir, Units: cfg.UnitDir}
	store, err := OpenStore(paths.AppsFile())
	if err != nil {
		log.Fatal(err)
	}
	srv := &Server{cfg: cfg, paths: paths, store: store, auth: NewAuth(cfg.Password, cfg.Secure), pending: map[string]bool{}}
	if cfg.GitHubAppID != "" || cfg.GitHubKeyFile != "" {
		if srv.gh, err = LoadGitHubApp(cfg.GitHubAppID, cfg.GitHubKeyFile); err != nil {
			log.Fatal("GitHub App: ", err)
		}
	}
	if err := srv.loadTemplates(); err != nil {
		log.Fatal(err)
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
	if cfg.PanelDomain != "" {
		go srv.publishPanel()
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

// loadPassword uses PANEL_PASSWORD, or a generated one persisted in the data dir.
func loadPassword(dir string) string {
	if p := os.Getenv("PANEL_PASSWORD"); p != "" {
		return p
	}
	file := filepath.Join(dir, "admin_password")
	if b, err := os.ReadFile(file); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		return strings.TrimSpace(string(b))
	}
	p := randomToken(12)
	if err := os.WriteFile(file, []byte(p+"\n"), 0o600); err != nil {
		log.Fatal(err)
	}
	log.Printf("senha de admin gerada (salva em %s): %s", file, p)
	return p
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
	for _, page := range []string{"login", "apps", "app_form", "app", "github"} {
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
	return securityHeaders(mux)
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
		if r.Method == http.MethodPost && !passwordEqual(r.PostFormValue("csrf"), csrf) {
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
	if s.auth.locked(clientIP(r)) {
		w.WriteHeader(http.StatusTooManyRequests)
		s.render(w, r, "login", page{Title: "Entrar", Error: "Muitas tentativas. Aguarde um minuto."})
		return
	}
	if !s.auth.Login(w, r, r.PostFormValue("password")) {
		w.WriteHeader(http.StatusUnauthorized)
		s.render(w, r, "login", page{Title: "Entrar", Error: "Senha incorreta."})
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.Logout(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	apps := s.store.List()
	states := map[string]string{}
	for _, a := range apps {
		states[a.Name] = serviceState(a.Name)
	}
	s.render(w, r, "apps", page{Title: "Aplicações", Apps: apps, States: states})
}

func (s *Server) handleNewForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "app_form", page{Title: "Nova aplicação", App: App{Branch: "main"}})
}

func appFromForm(r *http.Request, name string) (App, error) {
	port, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("port")))
	a := App{
		Name:       name,
		Repo:       strings.TrimSpace(r.PostFormValue("repo")),
		Branch:     strings.TrimSpace(r.PostFormValue("branch")),
		BuildCmd:   strings.TrimSpace(r.PostFormValue("build_cmd")),
		StartCmd:   strings.TrimSpace(r.PostFormValue("start_cmd")),
		Port:       port,
		Domain:     strings.ToLower(strings.TrimSpace(r.PostFormValue("domain"))),
		AutoDeploy: r.PostFormValue("auto_deploy") == "on",
		Env:        r.PostFormValue("env"),
		Status:     StatusIdle,
	}
	if a.Branch == "" {
		a.Branch = "main"
	}
	for _, err := range []error{
		validateName(a.Name), validateRepo(a.Repo), validateBranch(a.Branch),
		validateCommand("Comando de build", a.BuildCmd, false),
		validateCommand("Comando de início", a.StartCmd, true),
		validatePort(a.Port), validateDomain(a.Domain),
	} {
		if err != nil {
			return a, err
		}
	}
	if a.Domain != "" && a.Port == 0 {
		return a, errors.New("informe a porta da aplicação para publicar um domínio")
	}
	if _, ok := githubSlug(a.Repo); a.AutoDeploy && !ok {
		return a, errors.New("deploy automático exige um repositório do GitHub")
	}
	_, err := parseEnv(a.Env)
	return a, err
}

// domainTaken reports whether another app (or the panel) already uses the domain.
func (s *Server) domainTaken(domain, except string) bool {
	if domain == "" {
		return false
	}
	if domain == s.cfg.PanelDomain {
		return true
	}
	for _, o := range s.store.List() {
		if o.Name != except && o.Domain == domain {
			return true
		}
	}
	return false
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	a, err := appFromForm(r, strings.TrimSpace(r.PostFormValue("name")))
	if err == nil {
		if _, exists := s.store.Get(a.Name); exists {
			err = errors.New("já existe uma aplicação com esse nome")
		} else if s.domainTaken(a.Domain, a.Name) {
			err = errors.New("domínio já usado por outra aplicação")
		}
	}
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.render(w, r, "app_form", page{Title: "Nova aplicação", App: a, Error: err.Error()})
		return
	}
	if err := s.store.Put(a); err != nil {
		http.Error(w, "erro ao salvar", http.StatusInternalServerError)
		return
	}
	s.startDeploy(a.Name, "criação")
	http.Redirect(w, r, "/apps/"+a.Name, http.StatusSeeOther)
}

// appFor loads the app named in the path, replying 404 if absent.
func (s *Server) appFor(w http.ResponseWriter, r *http.Request) (App, bool) {
	name := r.PathValue("name")
	if validateName(name) == nil {
		if a, ok := s.store.Get(name); ok {
			return a, true
		}
	}
	http.NotFound(w, r)
	return App{}, false
}

func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.appFor(w, r); ok {
		s.render(w, r, "app", page{Title: a.Name, App: a, State: serviceState(a.Name), SSL: a.Domain != "" && s.hasCert(a.Domain)})
	}
}

func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.appFor(w, r); ok {
		s.render(w, r, "app_form", page{Title: "Editar " + a.Name, App: a, Editing: true})
	}
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	old, ok := s.appFor(w, r)
	if !ok {
		return
	}
	a, err := appFromForm(r, old.Name)
	if err == nil && s.domainTaken(a.Domain, a.Name) {
		err = errors.New("domínio já usado por outra aplicação")
	}
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.render(w, r, "app_form", page{Title: "Editar " + old.Name, App: a, Editing: true, Error: err.Error()})
		return
	}
	a.Status, a.LastDeploy = old.Status, old.LastDeploy
	if err := s.store.Put(a); err != nil {
		http.Error(w, "erro ao salvar", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/apps/"+a.Name+"?msg=Configura%C3%A7%C3%A3o+salva.+Fa%C3%A7a+um+novo+deploy+para+aplicar.", http.StatusSeeOther)
}

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appFor(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Deploying", strconv.FormatBool(a.Deploying()))
	if r.URL.Query().Get("kind") == "service" {
		_, _ = w.Write([]byte(journal(a.Name)))
		return
	}
	_, _ = w.Write([]byte(tailFile(s.paths.LogFile(a.Name), 64<<10)))
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appFor(w, r)
	if !ok {
		return
	}
	msg := ""
	switch action := r.PathValue("action"); action {
	case "deploy":
		if !s.startDeploy(a.Name, "manual") {
			msg = "Já existe um deploy em andamento."
		}
	case "start", "stop", "restart":
		if out, err := systemctl(action, unitName(a.Name)); err != nil {
			msg = "Falha: " + out
		} else {
			msg = "Ação executada: " + action
		}
	case "delete":
		if a.Deploying() {
			msg = "Aguarde o deploy terminar antes de excluir."
			break
		}
		if err := s.removeApp(a.Name); err != nil {
			msg = "Falha ao excluir: " + err.Error()
			break
		}
		http.Redirect(w, r, "/?msg=Aplica%C3%A7%C3%A3o+exclu%C3%ADda.", http.StatusSeeOther)
		return
	default:
		http.NotFound(w, r)
		return
	}
	q := ""
	if msg != "" {
		q = "?msg=" + url.QueryEscape(msg)
	}
	http.Redirect(w, r, "/apps/"+a.Name+q, http.StatusSeeOther)
}

func (s *Server) handleGitHub(w http.ResponseWriter, r *http.Request) {
	scheme, host := "http", r.Host
	if s.cfg.PanelDomain != "" {
		scheme, host = "https", s.cfg.PanelDomain
	} else if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	s.render(w, r, "github", page{Title: "GitHub", GH: ghStatus{
		WebhookEnabled: s.cfg.GitHubWebhookSecret != "",
		AppEnabled:     s.gh != nil,
		WebhookURL:     scheme + "://" + host + "/webhooks/github",
	}})
}

// publishPanel exposes the panel itself on PANEL_DOMAIN (needed to receive GitHub webhooks).
func (s *Server) publishPanel() {
	host, port, err := net.SplitHostPort(s.cfg.Addr)
	if err != nil {
		log.Printf("PANEL_DOMAIN: PANEL_ADDR inválido: %v", err)
		return
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	site := Site{Key: "_panel", Domain: s.cfg.PanelDomain, Upstream: net.JoinHostPort(host, port), MaxBody: "6m"}
	if err := s.ensureSite(ctx, os.Stderr, site); err != nil {
		log.Printf("PANEL_DOMAIN: %v", err)
		return
	}
	log.Printf("painel publicado em %s", s.cfg.PanelDomain)
}
