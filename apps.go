package main

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

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
	}
	if a.Branch == "" {
		a.Branch = "main"
	}
	for _, err := range []error{
		validateName(a.Name), validateRepo(a.Repo), validateBranch(a.Branch),
		validateCommand("Comando de build", a.BuildCmd, false),
		validateCommand("Comando de início", a.StartCmd, false),
		validatePort(a.Port), validateDomain(a.Domain),
	} {
		if err != nil {
			return a, err
		}
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
	if domain == s.settings().PanelDomain {
		return true
	}
	owner, taken := s.store.DomainOwner(domain)
	return taken && owner != except
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		name = suggestName(strings.TrimSpace(r.PostFormValue("repo")))
	}
	a, err := appFromForm(r, name)
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
	if a.Port == 0 {
		a.Port = s.pickPort("")
	}
	if err := s.store.Create(a); err != nil {
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
		s.render(w, r, "app", page{Title: a.Name, App: a, State: serviceState(a.Name), Info: serviceInfo(a.Name), SSL: a.Domain != "" && s.hasCert(a.Domain)})
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
	if a.Port == 0 {
		a.Port = s.pickPort(a.Name)
	}
	if err := s.store.UpdateConfig(a); err != nil {
		http.Error(w, "erro ao salvar", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/apps/"+a.Name+"?msg=Configura%C3%A7%C3%A3o+salva.+Fa%C3%A7a+um+novo+deploy+para+aplicar.", http.StatusSeeOther)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.appFor(w, r); ok {
		s.render(w, r, "logs", page{Title: "Logs de " + a.Name, App: a})
	}
}

// handleLog serves plain-text logs: kind=service (journal) or deploy, lines=N, download=1.
func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appFor(w, r)
	if !ok {
		return
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines < 10 || lines > 5000 {
		lines = 200
	}
	kind := r.URL.Query().Get("kind")
	if kind != "deploy" {
		kind = "service"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Deploying", strconv.FormatBool(a.Deploying()))
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+a.Name+"-"+kind+`.log"`)
	}
	if kind == "service" {
		_, _ = w.Write([]byte(journal(a.Name, lines)))
		return
	}
	_, _ = w.Write([]byte(lastLines(tailFile(s.paths.LogFile(a.Name), 512<<10), lines)))
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
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
