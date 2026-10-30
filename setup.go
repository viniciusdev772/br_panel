package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var setupMu sync.Mutex

// announceSetup creates the one-time setup token and tells the operator where to find it.
func (s *Server) announceSetup() {
	s.setupToken = randomToken(12)
	s.suggestedWH = randomToken(20)
	file := filepath.Join(s.cfg.DataDir, "setup_token")
	if err := os.WriteFile(file, []byte(s.setupToken+"\n"), 0o600); err != nil {
		log.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(s.cfg.Addr)
	log.Printf("PRIMEIRO INÍCIO: abra http://<IP-do-servidor>:%s/setup e informe o token de instalação: %s", port, s.setupToken)
	log.Printf("(o token também está em %s e é removido ao concluir)", file)
}

func (s *Server) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	set := defaultSettings()
	set.GitHubWebhookSecret = s.suggestedWH
	s.render(w, r, "settings", page{Title: "Configuração inicial", Setup: true, Set: set, Checks: checkSystem(), Form: map[string]string{}})
}

func (s *Server) setupError(w http.ResponseWriter, r *http.Request, code int, set Settings, msg string) {
	w.WriteHeader(code)
	s.render(w, r, "settings", page{Title: "Configuração inicial", Setup: true, Set: set, Checks: checkSystem(), Error: msg,
		Form: map[string]string{"username": strings.TrimSpace(r.PostFormValue("username"))}})
}

// handleSetup completes the first-run wizard: admin account plus initial settings.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	setupMu.Lock()
	defer setupMu.Unlock()
	if s.configured.Load() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	set := readSettingsForm(r, defaultSettings(), true)
	ip := clientIP(r)
	if s.auth.locked(ip) {
		s.setupError(w, r, http.StatusTooManyRequests, set, "Muitas tentativas. Aguarde um minuto.")
		return
	}
	if !constEqual(strings.TrimSpace(r.PostFormValue("token")), s.setupToken) {
		s.auth.Fail(ip)
		s.setupError(w, r, http.StatusForbidden, set, "Token de instalação inválido. Veja o log do serviço ou o arquivo setup_token.")
		return
	}

	set.AdminUser = strings.TrimSpace(r.PostFormValue("username"))
	pw := r.PostFormValue("password")
	var err error
	switch {
	case !userNameRe.MatchString(set.AdminUser):
		err = errMsg("usuário inválido: 3 a 32 caracteres (a-z, 0-9, _ e -), começando com letra")
	case pw != r.PostFormValue("password2"):
		err = errMsg("as senhas não conferem")
	default:
		if err = validatePassword(pw); err == nil {
			err = validateSettings(set)
		}
	}
	if err == nil {
		err = ensureAppUser(set.AppUser)
	}
	if err == nil {
		set.AdminHash, err = hashPassword(pw)
	}
	if err == nil {
		err = s.applySettings(set)
	}
	if err != nil {
		s.setupError(w, r, http.StatusUnprocessableEntity, set, err.Error())
		return
	}

	s.setupToken = ""
	_ = os.Remove(filepath.Join(s.cfg.DataDir, "setup_token"))
	log.Printf("configuração inicial concluída (usuário %q)", set.AdminUser)
	if set.PanelDomain != "" {
		go s.publishPanel()
	}
	s.auth.Start(w, r)
	http.Redirect(w, r, "/?msg=Configura%C3%A7%C3%A3o+conclu%C3%ADda.+Bem-vindo%21", http.StatusSeeOther)
}

type errMsg string

func (e errMsg) Error() string { return string(e) }

// readSettingsForm overlays submitted values on cur. Empty secret fields keep the stored value.
func readSettingsForm(r *http.Request, cur Settings, setup bool) Settings {
	v := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	set := cur
	set.PanelDomain = strings.ToLower(v("panel_domain"))
	set.AcmeEmail = v("acme_email")
	set.AppUser = v("app_user")
	if !setup {
		for k, dst := range map[string]*string{"nginx_dir": &set.NginxDir, "acme_root": &set.AcmeRoot, "cert_dir": &set.CertDir, "unit_dir": &set.UnitDir} {
			if r.PostForm.Has(k) {
				*dst = v(k)
			}
		}
	}
	if !setup && r.PostFormValue("clear_github") == "on" {
		set.GitHubAppID, set.GitHubKey, set.GitHubWebhookSecret = "", "", ""
		return set
	}
	set.GitHubAppID = v("github_app_id")
	if key := strings.TrimSpace(strings.ReplaceAll(r.PostFormValue("github_key"), "\r\n", "\n")); key != "" {
		set.GitHubKey = key + "\n"
	} else if setup {
		set.GitHubKey = ""
	}
	if secret := v("webhook_secret"); secret != "" || setup {
		set.GitHubWebhookSecret = secret
	}
	return set
}

// viewSettings hides stored secrets before they are sent to the browser.
func viewSettings(set Settings) Settings {
	set.AdminHash, set.GitHubKey, set.GitHubWebhookSecret = "", "", ""
	return set
}

func (s *Server) handleSettingsForm(w http.ResponseWriter, r *http.Request) {
	cur := s.settings()
	s.render(w, r, "settings", page{Title: "Configurações", Set: viewSettings(cur), Checks: checkSystem(),
		HasKey: cur.GitHubKey != "", HasSecret: cur.GitHubWebhookSecret != "", Form: map[string]string{"username": cur.AdminUser}})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	cur := s.settings()
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		s.render(w, r, "settings", page{Title: "Configurações", Set: viewSettings(readSettingsForm(r, cur, false)), Checks: checkSystem(),
			HasKey: cur.GitHubKey != "", HasSecret: cur.GitHubWebhookSecret != "", Error: msg, Form: map[string]string{"username": cur.AdminUser}})
	}
	set := readSettingsForm(r, cur, false)
	passwordChanged := false
	if np := r.PostFormValue("new_password"); np != "" {
		switch {
		case !verifyPassword(r.PostFormValue("current_password"), cur.AdminHash):
			fail(http.StatusForbidden, "senha atual incorreta")
			return
		case np != r.PostFormValue("new_password2"):
			fail(http.StatusUnprocessableEntity, "as novas senhas não conferem")
			return
		}
		if err := validatePassword(np); err != nil {
			fail(http.StatusUnprocessableEntity, err.Error())
			return
		}
		h, err := hashPassword(np)
		if err != nil {
			fail(http.StatusInternalServerError, err.Error())
			return
		}
		set.AdminHash, passwordChanged = h, true
	}
	if err := validateSettings(set); err != nil {
		fail(http.StatusUnprocessableEntity, err.Error())
		return
	}
	if set.AppUser != cur.AppUser {
		if err := ensureAppUser(set.AppUser); err != nil {
			fail(http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if err := s.applySettings(set); err != nil {
		fail(http.StatusInternalServerError, err.Error())
		return
	}
	if passwordChanged { // end every session, including attackers', then keep this one
		s.auth.RevokeAll()
		s.auth.Start(w, r)
	}
	if set.PanelDomain != cur.PanelDomain || set.AcmeEmail != cur.AcmeEmail {
		if set.PanelDomain != "" {
			go s.publishPanel()
		} else {
			go func() { _ = s.removeSite(context.Background(), os.Stderr, "_panel") }()
		}
	}
	http.Redirect(w, r, "/settings?msg=Configura%C3%A7%C3%B5es+salvas.", http.StatusSeeOther)
}

func (s *Server) handleGitHub(w http.ResponseWriter, r *http.Request) {
	set := s.settings()
	scheme, host := "http", r.Host
	if set.PanelDomain != "" {
		scheme, host = "https", set.PanelDomain
	} else if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	s.render(w, r, "github", page{Title: "GitHub", GH: ghStatus{
		WebhookEnabled: set.GitHubWebhookSecret != "",
		AppEnabled:     set.GitHubKey != "",
		WebhookURL:     scheme + "://" + host + "/webhooks/github",
	}})
}

// publishPanel exposes the panel itself on the configured panel domain (needed to receive GitHub webhooks).
func (s *Server) publishPanel() {
	set := s.settings()
	host, port, err := net.SplitHostPort(s.cfg.Addr)
	if err != nil {
		log.Printf("publicação do painel: endereço de escuta inválido: %v", err)
		return
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	site := Site{Key: "_panel", Domain: set.PanelDomain, Upstream: net.JoinHostPort(host, port), MaxBody: "6m"}
	if err := s.ensureSite(ctx, os.Stderr, site); err != nil {
		log.Printf("publicação do painel: %v", err)
		return
	}
	log.Printf("painel publicado em %s", set.PanelDomain)
}
