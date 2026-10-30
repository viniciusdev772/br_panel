// Package githubapp implements the parts of GitHub's App and webhook protocols the panel needs:
// webhook signature checks, App JWTs and short-lived, repository-scoped installation tokens.
package githubapp

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
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	githubAPI        = "https://api.github.com"
	githubAPIVersion = "2022-11-28"
)

var ErrNotInstalled = errors.New("GitHub App não está instalado neste repositório")

var ghRepoRe = regexp.MustCompile(`^(?:https://github\.com/|ssh://git@github\.com/|git@github\.com:)([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// Slug returns the lowercase "owner/repo" for GitHub repository URLs.
func Slug(repo string) (string, bool) {
	m := ghRepoRe.FindStringSubmatch(repo)
	if m == nil {
		return "", false
	}
	return strings.ToLower(m[1] + "/" + m[2]), true
}

// ValidSignature checks GitHub's X-Hub-Signature-256 header (HMAC-SHA256 of the raw body).
func ValidSignature(secret string, body []byte, header string) bool {
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

// Client authenticates as a GitHub App to obtain short-lived, repo-scoped
// installation tokens (used to clone private repositories).
type Client struct {
	AppID  string
	key    *rsa.PrivateKey
	client *http.Client
	api    string
}

func Parse(appID string, keyPEM []byte) (*Client, error) {
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
	return &Client{AppID: appID, key: key, client: &http.Client{Timeout: 15 * time.Second}, api: githubAPI}, nil
}

// UseAPI points the client at another API base URL (tests only).
func (g *Client) UseAPI(base string) { g.api = base }

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// jwt builds the RS256 token GitHub requires (exp at most 10 minutes ahead).
func (g *Client) jwt(now time.Time) (string, error) {
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

func (g *Client) call(ctx context.Context, method, path, auth string, body any, out any) (int, error) {
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
func (g *Client) InstallationToken(ctx context.Context, owner, repo string) (string, error) {
	jwt, err := g.jwt(time.Now())
	if err != nil {
		return "", err
	}
	var inst struct {
		ID int64 `json:"id"`
	}
	code, err := g.call(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/installation", jwt, nil, &inst)
	if code == http.StatusNotFound {
		return "", ErrNotInstalled
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

// Repo is a repository the GitHub App can see, as offered in the "new application" form.
type Repo struct {
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

const (
	repoPageSize     = 100
	maxRepoPages     = 5 // per installation
	maxInstallations = 20
)

// Repositories lists the repositories of every installation of the App, sorted by name.
// The per-installation token asks for metadata:read only (enough to list) and is used
// here and dropped: it never reaches git, builds or the app (see AuthEnv for cloning).
func (g *Client) Repositories(ctx context.Context) ([]Repo, error) {
	jwt, err := g.jwt(time.Now())
	if err != nil {
		return nil, err
	}
	var insts []struct {
		ID int64 `json:"id"`
	}
	if _, err := g.call(ctx, http.MethodGet, fmt.Sprintf("/app/installations?per_page=%d", maxInstallations), jwt, nil, &insts); err != nil {
		return nil, err
	}
	var all []Repo
	for _, in := range insts {
		var tok struct {
			Token string `json:"token"`
		}
		if _, err := g.call(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", in.ID), jwt,
			map[string]any{"permissions": map[string]string{"metadata": "read"}}, &tok); err != nil {
			return nil, err
		}
		for page := 1; page <= maxRepoPages; page++ {
			var res struct {
				Repositories []Repo `json:"repositories"`
			}
			if _, err := g.call(ctx, http.MethodGet, fmt.Sprintf("/installation/repositories?per_page=%d&page=%d", repoPageSize, page), tok.Token, nil, &res); err != nil {
				return nil, err
			}
			all = append(all, res.Repositories...)
			if len(res.Repositories) < repoPageSize {
				break
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return strings.ToLower(all[i].FullName) < strings.ToLower(all[j].FullName) })
	return all, nil
}

// AuthEnv returns env plus, for private GitHub repos reachable through the GitHub App,
// a scoped credential header. It is meant for git only, never for the build or the app.
// A nil client (no App configured) returns env unchanged.
func AuthEnv(ctx context.Context, c *Client, repo string, env []string, logw io.Writer) []string {
	out := append([]string(nil), env...)
	if tok := token(ctx, c, repo, logw); tok != "" {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + tok))
		out = append(out, "GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic)
	}
	return out
}

// token returns an installation token for private HTTPS GitHub repos, or "" to clone anonymously.
func token(ctx context.Context, gh *Client, repo string, logw io.Writer) string {
	if gh == nil || !strings.HasPrefix(repo, "https://") {
		return ""
	}
	slug, ok := Slug(repo)
	if !ok {
		return ""
	}
	owner, name, _ := strings.Cut(slug, "/")
	tok, err := gh.InstallationToken(ctx, owner, name)
	switch {
	case errors.Is(err, ErrNotInstalled):
		fmt.Fprintln(logw, "GitHub App não instalado neste repositório; clonando sem autenticação.")
	case err != nil:
		fmt.Fprintf(logw, "AVISO: token do GitHub App indisponível (%v); clonando sem autenticação.\n", err)
	default:
		fmt.Fprintln(logw, "autenticado como GitHub App (token somente leitura de 1h)")
	}
	return tok
}

// ---- GitHub App manifest flow (one-click App creation) ----

// Manifest is the JSON a browser posts to github.com/settings/apps/new. The webhook
// and permissions match what the panel needs: push events and read-only contents.
func Manifest(name, panelURL string) (string, error) {
	b, err := json.Marshal(map[string]any{
		"name":                name,
		"url":                 panelURL,
		"hook_attributes":     map[string]any{"url": panelURL + "/webhooks/github", "active": true},
		"redirect_url":        panelURL + "/github/app/callback",
		"public":              false,
		"default_permissions": map[string]string{"contents": "read", "metadata": "read"},
		"default_events":      []string{"push"},
	})
	return string(b), err
}

// Created is what GitHub returns once the user approves the manifest.
type Created struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	PEM           string `json:"pem"`
	WebhookSecret string `json:"webhook_secret"`
}

var (
	manifestCodeRe = regexp.MustCompile(`^[A-Za-z0-9]{1,100}$`)
	// AppSlugRe is the shape of a GitHub App slug (used in its install URL).
	AppSlugRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,60}$`)
)

// ConvertManifest exchanges the temporary code GitHub sent to the redirect URL for the
// new App's credentials. The call is unauthenticated; api is empty outside tests.
func ConvertManifest(ctx context.Context, api, code string) (Created, error) {
	var c Created
	if !manifestCodeRe.MatchString(code) {
		return c, errors.New("código do GitHub inválido")
	}
	if api == "" {
		api = githubAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+"/app-manifests/"+code+"/conversions", nil)
	if err != nil {
		return c, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "brpanel")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return c, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return c, fmt.Errorf("GitHub recusou o código (%s); refaça a criação do App", resp.Status)
	}
	if err := json.Unmarshal(data, &c); err != nil || c.ID == 0 || c.PEM == "" || c.WebhookSecret == "" || !AppSlugRe.MatchString(c.Slug) {
		return Created{}, errors.New("resposta inesperada do GitHub ao criar o App")
	}
	return c, nil
}
