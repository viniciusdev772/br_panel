package server

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/auth"
	"github.com/viniciusdev772/br_panel/internal/cloudflare"
	"github.com/viniciusdev772/br_panel/internal/githubapp"
	"github.com/viniciusdev772/br_panel/internal/nginx"
	"github.com/viniciusdev772/br_panel/internal/settings"
)

// The GitHub page walks the admin through creating the panel's GitHub App with GitHub's
// "manifest" flow: panel domain (DNS via Cloudflare when configured) -> manifest posted to
// github.com -> GitHub redirects back with a code -> the panel exchanges it for the App's
// ID, private key and webhook secret and saves them.

const ghStateTTL = 15 * time.Minute

func hostOnly(h string) string {
	if x, _, err := net.SplitHostPort(h); err == nil {
		return x
	}
	return h
}

func (s *Server) handleGitHub(w http.ResponseWriter, r *http.Request) {
	s.githubPage(w, r, http.StatusOK, "")
}

// githubPage renders /github with its current step; code/errMsg report a failed action.
func (s *Server) githubPage(w http.ResponseWriter, r *http.Request, code int, errMsg string) {
	set := s.settings()
	scheme, host := "http", r.Host
	if set.PanelDomain != "" {
		scheme, host = "https", set.PanelDomain
	} else if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	st := ghStatus{
		WebhookEnabled: set.GitHubWebhookSecret != "",
		AppEnabled:     set.GitHubKey != "",
		WebhookURL:     scheme + "://" + host + "/webhooks/github",
		HasCloudflare:  set.CloudflareToken != "",
		PanelDomain:    set.PanelDomain,
		PanelReady:     set.PanelDomain != "" && nginx.FromSettings(set).HasCert(set.PanelDomain),
		OnPanelDomain:  set.PanelDomain != "" && strings.EqualFold(hostOnly(r.Host), set.PanelDomain),
		NeedEmail:      set.AcmeEmail == "",
	}
	s.pubMu.Lock()
	st.Publishing, st.PublishErr = s.pubRunning, s.pubErr
	s.pubMu.Unlock()
	if set.GitHubAppSlug != "" {
		st.InstallURL = "https://github.com/apps/" + set.GitHubAppSlug + "/installations/new"
	}
	if code != http.StatusOK {
		w.WriteHeader(code)
	}
	s.render(w, r, "github", page{Title: "GitHub", GH: st, Error: errMsg})
}

// handleGitHubRepos lists the repositories the GitHub App can access, for the "new
// application" form. Read-only; the response carries no credentials.
func (s *Server) handleGitHubRepos(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	reply := func(code int, v any) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	gh := s.githubApp()
	if gh == nil {
		reply(http.StatusNotFound, map[string]string{"error": "O GitHub App não está configurado."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	repos, err := gh.Repositories(ctx)
	if err != nil {
		reply(http.StatusBadGateway, map[string]string{"error": "Não foi possível listar os repositórios do GitHub App: " + err.Error()})
		return
	}
	if repos == nil {
		repos = []githubapp.Repo{}
	}
	reply(http.StatusOK, map[string]any{"repos": repos})
}

// handleGitHubDomain creates the panel's DNS record on Cloudflare, saves the panel domain
// and publishes the panel (Nginx + SSL) so GitHub can reach the webhook.
func (s *Server) handleGitHubDomain(w http.ResponseWriter, r *http.Request) {
	set := s.settings()
	cf := cloudflare.New(set.CloudflareEmail, set.CloudflareToken)
	if cf == nil {
		s.githubPage(w, r, http.StatusUnprocessableEntity, "Configure a Cloudflare em Configurações antes de criar o registro DNS.")
		return
	}
	set.PanelDomain = strings.ToLower(strings.TrimSpace(r.PostFormValue("domain")))
	if set.AcmeEmail == "" {
		set.AcmeEmail = strings.TrimSpace(r.PostFormValue("acme_email"))
	}
	if set.PanelDomain == "" || set.AcmeEmail == "" {
		s.githubPage(w, r, http.StatusUnprocessableEntity, "Informe o domínio do painel e o e-mail do Let's Encrypt.")
		return
	}
	if err := settings.Validate(set); err != nil {
		s.githubPage(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ip, res, err := cf.EnsureServerA(ctx, set.PanelDomain)
	if err != nil {
		s.githubPage(w, r, http.StatusBadGateway, err.Error())
		return
	}
	if err := s.applySettings(set); err != nil {
		s.githubPage(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	go s.publishPanel()
	msg := "Registro DNS " + res + " (" + set.PanelDomain + " → " + ip + "). Publicando o painel e emitindo o SSL; recarregue em instantes."
	http.Redirect(w, r, "/github?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

// handleGitHubAppNew shows the page that posts the App manifest to github.com. It must be
// opened on the panel's HTTPS domain: GitHub sends the browser back to that domain.
func (s *Server) handleGitHubAppNew(w http.ResponseWriter, r *http.Request) {
	set := s.settings()
	switch {
	case set.GitHubAppID != "":
		s.githubPage(w, r, http.StatusConflict, "O GitHub App já está configurado. Remova-o em Configurações para criar outro.")
		return
	case set.PanelDomain == "" || !nginx.FromSettings(set).HasCert(set.PanelDomain):
		s.githubPage(w, r, http.StatusConflict, "O painel ainda não tem um domínio com SSL. Conclua o passo anterior primeiro.")
		return
	case !strings.EqualFold(hostOnly(r.Host), set.PanelDomain):
		s.githubPage(w, r, http.StatusConflict, "Abra esta página em https://"+set.PanelDomain+"/github para continuar.")
		return
	}
	state := auth.RandomToken(16)
	s.ghMu.Lock()
	s.ghState, s.ghStateExp = state, time.Now().Add(ghStateTTL)
	s.ghMu.Unlock()

	name := "BR Panel " + strings.ReplaceAll(set.PanelDomain, ".", "-")
	if len(name) > 34 { // GitHub's limit; the admin can still rename it on GitHub
		name = strings.TrimRight(name[:34], "-")
	}
	manifest, err := githubapp.Manifest(name, "https://"+set.PanelDomain)
	if err != nil {
		s.githubPage(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	// The form posts to github.com, which the global CSP (form-action 'self') forbids.
	csp := strings.Replace(w.Header().Get("Content-Security-Policy"), "form-action 'self'", "form-action 'self' https://github.com", 1)
	w.Header().Set("Content-Security-Policy", csp)
	s.render(w, r, "github_create", page{Title: "Criar GitHub App", GH: ghStatus{
		Manifest:  manifest,
		CreateURL: "https://github.com/settings/apps/new?state=" + state,
	}})
}

// handleGitHubCallback receives the browser coming back from github.com. The session cookie
// is SameSite=Strict and is not sent on that cross-site navigation, so the request is
// authorized by the single-use state generated for the logged-in admin instead.
func (s *Server) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		s.render(w, r, "github_done", page{Title: "GitHub App", Error: msg})
	}
	s.ghMu.Lock()
	ok := s.ghState != "" && time.Now().Before(s.ghStateExp) && auth.ConstEqual(r.URL.Query().Get("state"), s.ghState)
	if ok {
		s.ghState = "" // single use
	}
	s.ghMu.Unlock()
	if !ok {
		fail(http.StatusBadRequest, "Pedido inválido ou expirado. Volte ao painel e inicie a criação do GitHub App de novo.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	c, err := githubapp.ConvertManifest(ctx, s.ghAPI, r.URL.Query().Get("code"))
	if err != nil {
		fail(http.StatusBadGateway, err.Error())
		return
	}
	set := s.settings()
	set.GitHubAppID, set.GitHubKey = strconv.FormatInt(c.ID, 10), strings.TrimSpace(c.PEM)+"\n"
	set.GitHubWebhookSecret, set.GitHubAppSlug = c.WebhookSecret, c.Slug
	if err := settings.Validate(set); err != nil {
		fail(http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := s.applySettings(set); err != nil {
		fail(http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("GitHub App %q (id %d) criado pelo fluxo de manifest", c.Slug, c.ID)
	s.render(w, r, "github_done", page{Title: "GitHub App", GH: ghStatus{InstallURL: "https://github.com/apps/" + c.Slug + "/installations/new"}})
}
