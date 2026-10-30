package main

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
}

type Server struct {
	cfg   Config
	paths Paths
	store *Store
	auth  *Auth
	tmpl  map[string]*template.Template
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
	srv := &Server{cfg: cfg, paths: paths, store: store, auth: NewAuth(cfg.Password, cfg.Secure)}
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
	for _, page := range []string{"login", "apps", "app_form", "app"} {
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
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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
		Name:     name,
		Repo:     strings.TrimSpace(r.PostFormValue("repo")),
		Branch:   strings.TrimSpace(r.PostFormValue("branch")),
		BuildCmd: strings.TrimSpace(r.PostFormValue("build_cmd")),
		StartCmd: strings.TrimSpace(r.PostFormValue("start_cmd")),
		Port:     port,
		Env:      r.PostFormValue("env"),
		Status:   StatusIdle,
	}
	if a.Branch == "" {
		a.Branch = "main"
	}
	for _, err := range []error{
		validateName(a.Name), validateRepo(a.Repo), validateBranch(a.Branch),
		validateCommand("Comando de build", a.BuildCmd, false),
		validateCommand("Comando de início", a.StartCmd, true),
		validatePort(a.Port),
	} {
		if err != nil {
			return a, err
		}
	}
	_, err := parseEnv(a.Env)
	return a, err
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	a, err := appFromForm(r, strings.TrimSpace(r.PostFormValue("name")))
	if err == nil {
		if _, exists := s.store.Get(a.Name); exists {
			err = errors.New("já existe uma aplicação com esse nome")
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
	s.startDeploy(a.Name)
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
		s.render(w, r, "app", page{Title: a.Name, App: a, State: serviceState(a.Name)})
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
		if !s.startDeploy(a.Name) {
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
