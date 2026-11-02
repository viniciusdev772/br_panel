package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/viniciusdev772/br_panel/internal/nginx"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

// handleSite changes the Nginx options of an app (extra domains, redirects, password, maintenance
// page, upload and timeout limits). Each action validates its input, applies the result to nginx
// and only then saves it, so a rejected configuration is never stored. Passwords are hashed here
// and only the hash is kept; no page ever shows it.
func (s *Server) handleSite(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appFor(w, r)
	if !ok {
		return
	}
	web := a.Web
	web.Domains = slices.Clone(web.Domains) // never edit the slices of the loaded app in place
	web.Users = slices.Clone(web.Users)
	back := func(msg string) {
		http.Redirect(w, r, "/apps/"+a.Name+"?msg="+url.QueryEscape(msg)+"#site", http.StatusSeeOther)
	}
	var (
		err   error
		certs bool // the change adds a domain, which needs a certificate
		done  string
	)
	form := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	switch r.PathValue("action") {
	case "domain-add":
		certs, done = true, "Domínio adicionado."
		err = s.addDomain(a, &web, strings.ToLower(form("domain")), strings.ToLower(form("to")), form("kind") == "redirect", r.PostFormValue("permanent") == "on")
	case "domain-delete":
		name := form("domain")
		if !web.HasDomain(name) {
			err = errors.New("domínio não encontrado")
			break
		}
		web.Domains = slices.DeleteFunc(web.Domains, func(d store.WebDomain) bool { return d.Name == name })
		done = "Domínio removido."
	case "auth-add":
		err = addUser(&web, form("user"), r.PostFormValue("password"))
		done = "Usuário salvo. O site passa a pedir usuário e senha."
	case "auth-delete":
		name := form("user")
		if !web.HasUser(name) {
			err = errors.New("usuário não encontrado")
			break
		}
		web.Users = slices.DeleteFunc(web.Users, func(u store.WebUser) bool { return u.Name == name })
		done = "Usuário removido."
		if len(web.Users) == 0 {
			done = "Usuário removido. O site não pede mais senha."
		}
	case "maintenance":
		err = validate.MaintenanceText(form("title"), r.PostFormValue("text"))
		web.MaintTitle, web.MaintText = form("title"), strings.ReplaceAll(strings.TrimSpace(r.PostFormValue("text")), "\r\n", "\n")
		web.Maintenance = r.PostFormValue("state") == "on"
		done = "Manutenção desligada."
		if web.Maintenance {
			done = "Página de manutenção ligada: os visitantes veem o aviso."
		}
	case "limits":
		web.MaxBodyMB, err = optionalInt(form("upload_mb"))
		if err == nil {
			web.ReadTimeout, err = optionalInt(form("read_timeout"))
		}
		if err == nil {
			err = errors.Join(validate.UploadMB(web.MaxBodyMB), validate.ReadTimeout(web.ReadTimeout))
		}
		done = "Limites salvos."
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		back("Não salvo: " + err.Error())
		return
	}
	a.Web = web
	applied, err := s.dep.ApplySite(a, certs)
	if err != nil {
		back("O Nginx recusou a configuração e nada foi salvo: " + err.Error())
		return
	}
	if err := s.store.SetWeb(a.Name, web); err != nil {
		back("Falha ao salvar: " + err.Error())
		return
	}
	switch {
	case applied && certs:
		done += " O certificado é emitido em segundo plano (pode levar alguns minutos); acompanhe em Logs."
	case !applied:
		done += " Vale a partir do próximo deploy."
	}
	back(done)
}

func (s *Server) addDomain(a store.App, web *store.Web, name, to string, redirect, permanent bool) error {
	if !redirect {
		to, permanent = "", false
	}
	switch {
	case a.Domain == "":
		return errors.New("defina o domínio principal da aplicação antes (em Configurações)")
	case len(web.Domains) >= validate.MaxExtraDomains:
		return fmt.Errorf("o limite é de %d domínios extras por aplicação", validate.MaxExtraDomains)
	case redirect && to == "":
		return errors.New("informe para qual domínio redirecionar")
	case web.HasDomain(name):
		return errors.New("esse domínio já foi adicionado")
	}
	if err := validate.ExtraDomain(name, to, a.Domain); err != nil {
		return err
	}
	if s.domainTaken(name, a.Name) {
		return errors.New("domínio já usado por outra aplicação")
	}
	web.Domains = append(web.Domains, store.WebDomain{Name: name, To: to, Permanent: permanent})
	return nil
}

// addUser adds a basic-auth user, or changes the password of an existing one.
func addUser(web *store.Web, name, password string) error {
	if err := errors.Join(validate.WebUser(name), validate.WebPassword(password)); err != nil {
		return err
	}
	if !web.HasUser(name) && len(web.Users) >= validate.MaxWebUsers {
		return fmt.Errorf("o limite é de %d usuários por aplicação", validate.MaxWebUsers)
	}
	hash, err := nginx.HashPassword(password)
	if err != nil {
		return err
	}
	for i := range web.Users {
		if web.Users[i].Name == name {
			web.Users[i].Hash = hash
			return nil
		}
	}
	web.Users = append(web.Users, store.WebUser{Name: name, Hash: hash})
	return nil
}

// optionalInt reads a whole number from a form field; an empty field is 0 ("use the default").
func optionalInt(v string) (int, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, errors.New("use só números")
	}
	return n, nil
}
