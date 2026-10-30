package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	githubAPI        = "https://api.github.com"
	githubAPIVersion = "2022-11-28"
	webhookMaxBody   = 5 << 20
)

var errNotInstalled = errors.New("GitHub App não está instalado neste repositório")

var ghRepoRe = regexp.MustCompile(`^(?:https://github\.com/|ssh://git@github\.com/|git@github\.com:)([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// githubSlug returns the lowercase "owner/repo" for GitHub repository URLs.
func githubSlug(repo string) (string, bool) {
	m := ghRepoRe.FindStringSubmatch(repo)
	if m == nil {
		return "", false
	}
	return strings.ToLower(m[1] + "/" + m[2]), true
}

// validSignature checks GitHub's X-Hub-Signature-256 header (HMAC-SHA256 of the raw body).
func validSignature(secret string, body []byte, header string) bool {
	hexSig, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	got, err := hex.DecodeString(hexSig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), got)
}

// GitHubApp authenticates as a GitHub App to obtain short-lived, repo-scoped
// installation tokens (used to clone private repositories).
type GitHubApp struct {
	AppID  string
	key    *rsa.PrivateKey
	client *http.Client
	api    string
}

func ParseGitHubApp(appID string, keyPEM []byte) (*GitHubApp, error) {
	blk, _ := pem.Decode(keyPEM)
	if blk == nil {
		return nil, errors.New("chave privada: PEM inválido")
	}
	var key *rsa.PrivateKey
	if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
		key = k
	} else if k, err := x509.ParsePKCS8PrivateKey(blk.Bytes); err == nil {
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("chave privada deve ser RSA")
		}
		key = rk
	} else {
		return nil, errors.New("chave privada: formato não suportado")
	}
	return &GitHubApp{AppID: appID, key: key, client: &http.Client{Timeout: 15 * time.Second}, api: githubAPI}, nil
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// jwt builds the RS256 token GitHub requires (exp at most 10 minutes ahead).
func (g *GitHubApp) jwt(now time.Time) (string, error) {
	claims, _ := json.Marshal(map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(), // tolerate clock drift
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": g.AppID,
	})
	input := b64url([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + b64url(claims)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, g.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return input + "." + b64url(sig), nil
}

func (g *GitHubApp) call(ctx context.Context, method, path, auth string, body any, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.api+path, rdr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+auth)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "brpanel")
	resp, err := g.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("GitHub API %s %s: %s", method, path, resp.Status)
	}
	return resp.StatusCode, json.Unmarshal(data, out)
}

// InstallationToken returns a one-hour token limited to read access on a single repository.
func (g *GitHubApp) InstallationToken(ctx context.Context, owner, repo string) (string, error) {
	jwt, err := g.jwt(time.Now())
	if err != nil {
		return "", err
	}
	var inst struct {
		ID int64 `json:"id"`
	}
	code, err := g.call(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/installation", jwt, nil, &inst)
	if code == http.StatusNotFound {
		return "", errNotInstalled
	}
	if err != nil {
		return "", err
	}
	var tok struct {
		Token string `json:"token"`
	}
	_, err = g.call(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", inst.ID), jwt,
		map[string]any{"repositories": []string{repo}, "permissions": map[string]string{"contents": "read"}}, &tok)
	return tok.Token, err
}

// githubToken returns an installation token for private HTTPS GitHub repos, or "" to clone anonymously.
func (s *Server) githubToken(ctx context.Context, a App, logw io.Writer) string {
	gh := s.githubApp()
	if gh == nil || !strings.HasPrefix(a.Repo, "https://") {
		return ""
	}
	slug, ok := githubSlug(a.Repo)
	if !ok {
		return ""
	}
	owner, repo, _ := strings.Cut(slug, "/")
	tok, err := gh.InstallationToken(ctx, owner, repo)
	switch {
	case errors.Is(err, errNotInstalled):
		fmt.Fprintln(logw, "GitHub App não instalado neste repositório; clonando sem autenticação.")
	case err != nil:
		fmt.Fprintf(logw, "AVISO: token do GitHub App indisponível (%v); clonando sem autenticação.\n", err)
	default:
		fmt.Fprintln(logw, "autenticado como GitHub App (token somente leitura de 1h)")
	}
	return tok
}

// handleGitHubWebhook receives push events and redeploys matching apps.
func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	secret := s.settings().GitHubWebhookSecret
	if secret == "" {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "corpo inválido", http.StatusRequestEntityTooLarge)
		return
	}
	if !validSignature(secret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "assinatura inválida", http.StatusUnauthorized)
		return
	}
	switch ev := r.Header.Get("X-GitHub-Event"); ev {
	case "ping":
		_, _ = io.WriteString(w, "pong\n")
	case "push":
		var p struct {
			Ref        string `json:"ref"`
			After      string `json:"after"`
			Deleted    bool   `json:"deleted"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			http.Error(w, "JSON inválido (use Content-Type application/json)", http.StatusBadRequest)
			return
		}
		branch, ok := strings.CutPrefix(p.Ref, "refs/heads/")
		if !ok || p.Deleted {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		n := 0
		for _, a := range s.store.List() {
			slug, isGH := githubSlug(a.Repo)
			if !a.AutoDeploy || !isGH || slug != strings.ToLower(p.Repository.FullName) || a.Branch != branch {
				continue
			}
			n++
			reason := fmt.Sprintf("webhook push %s", shortSHA(p.After))
			if !s.startDeploy(a.Name, reason) {
				s.markPending(a.Name)
			}
			log.Printf("webhook: deploy de %s (%s)", a.Name, reason)
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, "%d deploy(s) disparado(s)\n", n)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func (s *Server) markPending(name string) {
	s.pendMu.Lock()
	s.pending[name] = true
	s.pendMu.Unlock()
}

func (s *Server) takePending(name string) bool {
	s.pendMu.Lock()
	defer s.pendMu.Unlock()
	p := s.pending[name]
	delete(s.pending, name)
	return p
}
