package server

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

	"github.com/viniciusdev772/br_panel/internal/auth"
	"github.com/viniciusdev772/br_panel/internal/cloudflare"
	"github.com/viniciusdev772/br_panel/internal/nginx"
	"github.com/viniciusdev772/br_panel/internal/settings"
)

var setupMu sync.Mutex

// announceSetup creates the one-time setup token and tells the operator where to find it.
func (s *Server) announceSetup() {
	s.setupToken = auth.RandomToken(12)
	file := filepath.Join(s.cfg.DataDir, "setup_token")
	if err := os.WriteFile(file, []byte(s.setupToken+"\n"), 0o600); err != nil {
		log.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(s.cfg.Addr)
	log.Printf("PRIMEIRO INÍCIO: abra o link http://%s/setup?code=%s", net.JoinHostPort(linkHost(host), port), s.setupToken)
	log.Printf("(o token também está em %s e é removido ao concluir)", file)
}

// validCode returns code when it is the current setup token, so a wrong one never hides the field.
func (s *Server) validCode(code string) string {
	if code = strings.TrimSpace(code); auth.ConstEqual(code, s.setupToken) {
		return code
	}
	return ""
}

// linkHost picks the address to print in the setup link: the listen host when it is specific,
// otherwise the IP this machine uses to reach the internet (a UDP "connect" sends no packet).
func linkHost(listen string) string {
	if listen != "" && listen != "0.0.0.0" && listen != "::" {
		return listen
	}
	if c, err := net.Dial("udp4", "8.8.8.8:80"); err == nil {
		defer c.Close()
		if a, ok := c.LocalAddr().(*net.UDPAddr); ok && !a.IP.IsUnspecified() {
			return a.IP.String()
		}
	}
	return "<IP-do-servidor>"
}

func (s *Server) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	code := s.validCode(r.URL.Query().Get("code")) // the link printed at install time carries it
	if code != "" {
		w.Header().Set("Referrer-Policy", "no-referrer")
	}
	s.render(w, r, "settings", page{Title: "Configuração inicial", Setup: true, SetupCode: code, Set: settings.Default(), AppUserDefault: defaultAppUser(), Checks: settings.CheckSystem(), Form: map[string]string{}})
}

func (s *Server) setupError(w http.ResponseWriter, r *http.Request, code int, set settings.Settings, msg string) {
	w.WriteHeader(code)
	s.render(w, r, "settings", page{Title: "Configuração inicial", Setup: true, SetupCode: s.validCode(r.PostFormValue("token")), Set: set, AppUserDefault: defaultAppUser(), Checks: settings.CheckSystem(), Error: msg,
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
	set := readSettingsForm(r, settings.Default(), true)
	ip := auth.ClientIP(r)
	if s.auth.Locked(ip) {
		s.setupError(w, r, http.StatusTooManyRequests, set, "Muitas tentativas. Aguarde um minuto.")
		return
	}
	if !auth.ConstEqual(strings.TrimSpace(r.PostFormValue("token")), s.setupToken) {
		s.auth.Fail(ip)
		s.setupError(w, r, http.StatusForbidden, set, "Código de instalação inválido. Ele aparece na tela do terminal onde o painel foi instalado.")
		return
	}

	set.AdminUser = strings.TrimSpace(r.PostFormValue("username"))
	pw := r.PostFormValue("password")
	var err error
	switch {
	case !settings.ValidUsername(set.AdminUser):
		err = errMsg("usuário inválido: 3 a 32 caracteres (a-z, 0-9, _ e -), começando com letra")
	case pw != r.PostFormValue("password2"):
		err = errMsg("as senhas não conferem")
	default:
		if err = settings.ValidatePassword(pw); err == nil {
			err = settings.Validate(set)
		}
	}
	if err == nil {
		err = settings.EnsureAppUser(set.AppUser)
	}
	if err == nil {
		set.AdminHash, err = settings.HashPassword(pw)
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

// defaultAppUser is the unprivileged account apps run as; the setup form submits it without asking,
// and it can only be created when the panel runs as root.
func defaultAppUser() string {
	if os.Geteuid() == 0 {
		return "brapps"
	}
	return ""
}

type errMsg string

func (e errMsg) Error() string { return string(e) }

// readSettingsForm overlays submitted values on cur. Empty secret fields keep the stored value.
func readSettingsForm(r *http.Request, cur settings.Settings, setup bool) settings.Settings {
	v := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	set := cur
	set.PanelDomain = strings.ToLower(v("panel_domain"))
	set.AcmeEmail = v("acme_email")
	set.AppUser = v("app_user")
	if !setup {
		for k, dst := range map[string]*string{"nginx_dir": &set.NginxDir, "acme_root": &set.AcmeRoot, "cert_dir": &set.CertDir, "unit_dir": &set.UnitDir, "db_user": &set.DBUser} {
			if r.PostForm.Has(k) {
				*dst = v(k)
			}
		}
	}
	if !setup {
		if r.PostFormValue("clear_cloudflare") == "on" {
			set.CloudflareToken, set.CloudflareEmail = "", ""
		} else {
			set.CloudflareEmail = v("cloudflare_email")
			if t := v("cloudflare_token"); t != "" {
				set.CloudflareToken = t
			}
		}
	}
	if !setup && r.PostFormValue("clear_github") == "on" {
		set.GitHubAppID, set.GitHubKey, set.GitHubWebhookSecret, set.GitHubAppSlug = "", "", "", ""
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
func viewSettings(set settings.Settings) settings.Settings {
	set.AdminHash, set.GitHubKey, set.GitHubWebhookSecret, set.CloudflareToken = "", "", "", ""
	return set
}

func (s *Server) handleSettingsForm(w http.ResponseWriter, r *http.Request) {
	cur := s.settings()
	s.render(w, r, "settings", page{Title: "Configurações", Set: viewSettings(cur), Checks: settings.CheckSystem(),
		HasKey: cur.GitHubKey != "", HasSecret: cur.GitHubWebhookSecret != "", HasCF: cur.CloudflareToken != "", Form: map[string]string{"username": cur.AdminUser}})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	cur := s.settings()
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		s.render(w, r, "settings", page{Title: "Configurações", Set: viewSettings(readSettingsForm(r, cur, false)), Checks: settings.CheckSystem(),
			HasKey: cur.GitHubKey != "", HasSecret: cur.GitHubWebhookSecret != "", HasCF: cur.CloudflareToken != "", Error: msg, Form: map[string]string{"username": cur.AdminUser}})
	}
	set := readSettingsForm(r, cur, false)
	passwordChanged := false
	if np := r.PostFormValue("new_password"); np != "" {
		switch {
		case !settings.VerifyPassword(r.PostFormValue("current_password"), cur.AdminHash):
			fail(http.StatusForbidden, "senha atual incorreta")
			return
		case np != r.PostFormValue("new_password2"):
			fail(http.StatusUnprocessableEntity, "as novas senhas não conferem")
			return
		}
		if err := settings.ValidatePassword(np); err != nil {
			fail(http.StatusUnprocessableEntity, err.Error())
			return
		}
		h, err := settings.HashPassword(np)
		if err != nil {
			fail(http.StatusInternalServerError, err.Error())
			return
		}
		set.AdminHash, passwordChanged = h, true
	}
	if err := settings.Validate(set); err != nil {
		fail(http.StatusUnprocessableEntity, err.Error())
		return
	}
	if set.CloudflareToken != "" && (set.CloudflareToken != cur.CloudflareToken || set.CloudflareEmail != cur.CloudflareEmail) {
		// Reject a wrong credential now instead of failing silently at the next deploy.
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		err := cloudflare.New(set.CloudflareEmail, set.CloudflareToken).Check(ctx)
		cancel()
		if err != nil {
			fail(http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if set.AppUser != cur.AppUser {
		if err := settings.EnsureAppUser(set.AppUser); err != nil {
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
			go func() { _ = nginx.FromSettings(cur).Remove(context.Background(), os.Stderr, "_panel") }()
		}
	}
	http.Redirect(w, r, "/settings?msg=Configura%C3%A7%C3%B5es+salvas.", http.StatusSeeOther)
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
	site := nginx.Site{Key: "_panel", Domain: set.PanelDomain, Upstream: net.JoinHostPort(host, port), MaxBody: "6m"}
	if err := nginx.FromSettings(set).Ensure(ctx, os.Stderr, site); err != nil {
		log.Printf("publicação do painel: %v", err)
		return
	}
	log.Printf("painel publicado em %s", set.PanelDomain)
}
