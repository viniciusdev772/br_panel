package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/viniciusdev772/br_panel/internal/cli"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/templates"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

func templateInfo(t templates.Template) cli.TemplateInfo {
	kind := "arquivos do painel"
	if t.Repo != "" {
		kind = "repositório Git"
	}
	return cli.TemplateInfo{ID: t.ID, Name: t.Name, Category: t.Category, Summary: t.Summary, Description: t.Description,
		Version: t.Version, VersionHint: t.VersionHint, Needs: t.Requires, Docs: t.Docs, After: t.After, Source: kind, MemoryMB: t.MemoryMB}
}

func (s *Server) handleAPITemplates(w http.ResponseWriter, r *http.Request) {
	out := []cli.TemplateInfo{}
	for _, t := range templates.All() {
		out = append(out, templateInfo(t))
	}
	writeJSON(w, out)
}

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "templates", page{Title: "Templates", Tpls: templates.All()})
}

// templateFor loads the template named in the path, replying 404 if absent.
func (s *Server) templateFor(w http.ResponseWriter, r *http.Request) (templates.Template, bool) {
	if t, ok := templates.Get(r.PathValue("id")); ok {
		return t, true
	}
	http.NotFound(w, r)
	return templates.Template{}, false
}

func (s *Server) handleTemplateForm(w http.ResponseWriter, r *http.Request) {
	if t, ok := s.templateFor(w, r); ok {
		s.render(w, r, "template", page{Title: t.Name, Tpl: t, Form: map[string]string{"name": s.freeName(t.ID), "version": t.Version}})
	}
}

// handleTemplateInstall creates an app from a template and starts its first deploy. It applies the
// same checks as creating an app by hand, so a template can never produce something the form
// would have refused.
func (s *Server) handleTemplateInstall(w http.ResponseWriter, r *http.Request) {
	t, ok := s.templateFor(w, r)
	if !ok {
		return
	}
	form := map[string]string{
		"name":    strings.TrimSpace(r.PostFormValue("name")),
		"domain":  strings.ToLower(strings.TrimSpace(r.PostFormValue("domain"))),
		"version": strings.TrimSpace(r.PostFormValue("version")),
	}
	if form["name"] == "" {
		form["name"] = s.freeName(t.ID)
	}
	fail := func(err error) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.render(w, r, "template", page{Title: t.Name, Tpl: t, Form: form, Error: err.Error()})
	}

	plan, err := t.Instantiate(templates.Input{Name: form["name"], Domain: form["domain"], Version: form["version"]})
	if err == nil {
		err = errors.Join(validate.Name(form["name"]), validate.Domain(form["domain"]))
	}
	a := store.App{Name: form["name"], Repo: plan.Repo, Branch: plan.Branch, BuildCmd: plan.Build, StartCmd: plan.Start,
		Domain: form["domain"], Env: plan.Env, Replicas: 1}
	if err == nil {
		err = errors.Join(validate.Repo(a.Repo), validate.Branch(a.Branch), validate.Command("Comando de build", a.BuildCmd, false),
			validate.Command("Comando de início", a.StartCmd, true))
		_, perr := validate.ParseEnv(a.Env)
		err = errors.Join(err, perr)
	}
	if err == nil {
		_, appTaken := s.store.Get(a.Name)
		_, dbTaken := s.store.GetDatabase(a.Name)
		switch {
		case appTaken || dbTaken:
			err = errors.New("já existe uma aplicação ou banco com esse nome")
		case s.domainTaken(a.Domain, a.Name):
			err = errors.New("domínio já usado por outra aplicação")
		}
	}
	if err == nil {
		if a.Port = s.dep.PickPortRange("", 1); a.Port == 0 {
			err = errors.New("não há porta livre para a aplicação")
		}
	}
	for _, m := range plan.Mounts {
		if err == nil {
			err = validate.Mount(store.MountVolume, m.Name, "", m.Target, "", s.dep.DataDir)
		}
	}
	if err != nil {
		fail(err)
		return
	}
	if err := s.store.Create(a); err != nil {
		fail(errors.New("não consegui salvar: " + err.Error()))
		return
	}
	for _, m := range plan.Mounts {
		if _, err := s.store.AddMount(store.Mount{App: a.Name, Name: m.Name, Kind: store.MountVolume, Target: m.Target}); err != nil {
			_ = s.store.Delete(a.Name) // never leave a half-made app that would lose its data on the first deploy
			fail(errors.New("não consegui criar o armazenamento: " + err.Error()))
			return
		}
	}
	s.store.SetKind(a.Name, "Template: "+t.Name)
	s.store.AddEvent("app", a.Name, "Aplicação criada a partir do template "+t.Name)
	s.dep.Start(a.Name, "template "+t.Name)
	http.Redirect(w, r, "/apps/"+a.Name+"?msg="+url.QueryEscape(t.Name+" instalando em segundo plano. "+t.After), http.StatusSeeOther)
}
