// Package settings defines the panel configuration that lives in the database
// (what used to be environment variables), its validation, and password hashing.
package settings

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/viniciusdev772/br_panel/internal/cloudflare"
	"github.com/viniciusdev772/br_panel/internal/githubapp"
	"github.com/viniciusdev772/br_panel/internal/nodeenv"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

// Settings is everything the panel used to read from environment variables.
// It lives in the SQLite "settings" table (key/value).
type Settings struct {
	AdminUser string
	AdminHash string

	PanelDomain string
	AcmeEmail   string
	AppUser     string
	DBUser      string // unprivileged system user that runs managed databases

	NginxDir string
	AcmeRoot string
	CertDir  string
	UnitDir  string

	GitHubAppID         string
	GitHubKey           string // PEM private key of the GitHub App
	GitHubWebhookSecret string

	GitHubAppSlug string // set when the App was created through the manifest flow; builds the install link

	NodeVersion string // Node.js version installed with nvm (nodeenv.ValidVersion)

	CloudflareEmail string // set only when CloudflareToken is the Global API Key
	CloudflareToken string // scoped API token (Zone:Read + DNS:Edit) or Global API Key; empty disables DNS automation
}

func Default() Settings {
	return Settings{
		NginxDir:    "/etc/nginx/conf.d",
		AcmeRoot:    "/var/www/brpanel-acme",
		CertDir:     "/etc/letsencrypt/live",
		UnitDir:     "/etc/systemd/system",
		DBUser:      "brdb",
		NodeVersion: nodeenv.DefaultVersion,
	}
}

// Fields maps each stored setting key to the struct field that holds it.
func (s *Settings) Fields() map[string]*string {
	return map[string]*string{
		"admin_user": &s.AdminUser, "admin_hash": &s.AdminHash,
		"panel_domain": &s.PanelDomain, "acme_email": &s.AcmeEmail, "app_user": &s.AppUser, "db_user": &s.DBUser,
		"nginx_dir": &s.NginxDir, "acme_root": &s.AcmeRoot, "cert_dir": &s.CertDir, "unit_dir": &s.UnitDir,
		"github_app_id": &s.GitHubAppID, "github_key": &s.GitHubKey, "github_webhook_secret": &s.GitHubWebhookSecret, "github_app_slug": &s.GitHubAppSlug,
		"cloudflare_token": &s.CloudflareToken, "node_version": &s.NodeVersion, "cloudflare_email": &s.CloudflareEmail,
	}
}

// ---- password hashing (Argon2id, OWASP-recommended family) ----

const (
	argonMem     = 32 * 1024 // KiB; above OWASP's 19 MiB minimum
	argonTime    = 3
	argonThreads = 1
	argonKeyLen  = 32
	MinPassword  = 12
	MaxPassword  = 128 // bounds hashing work for oversized input
)

func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMem, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMem, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(pw, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m > 256*1024 || t > 10 || p == 0 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func ValidatePassword(pw string) error {
	if len(pw) < MinPassword || len(pw) > MaxPassword {
		return fmt.Errorf("a senha deve ter entre %d e %d caracteres", MinPassword, MaxPassword)
	}
	return nil
}

// ---- validation ----

var (
	userNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,31}$`)
	sysUserRe  = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	appIDRe    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)
	cfTokenRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{20,128}$`)
)

func validateAbsDir(label, p string) error {
	if p == "" || !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\r\n\"';{}$ ") || filepath.Clean(p) != p {
		return fmt.Errorf("%s: informe um caminho absoluto simples (sem espaços ou caracteres especiais)", label)
	}
	return nil
}

// Validate checks everything except the admin credentials.
func Validate(s Settings) error {
	if err := validate.Domain(s.PanelDomain); err != nil {
		return fmt.Errorf("domínio do painel: %w", err)
	}
	if s.AcmeEmail != "" {
		a, err := mail.ParseAddress(s.AcmeEmail)
		if err != nil || a.Address != s.AcmeEmail || len(s.AcmeEmail) > 254 || strings.ContainsAny(s.AcmeEmail, " \"'") {
			return errors.New("e-mail do Let's Encrypt inválido")
		}
	}
	if s.AppUser != "" && !sysUserRe.MatchString(s.AppUser) {
		return errors.New("usuário das aplicações inválido (use letras minúsculas, números, _ e -)")
	}
	if !sysUserRe.MatchString(s.DBUser) {
		return errors.New("usuário dos bancos de dados inválido (use letras minúsculas, números, _ e -)")
	}
	for label, p := range map[string]string{"diretório do Nginx": s.NginxDir, "webroot ACME": s.AcmeRoot, "diretório de certificados": s.CertDir, "diretório das units": s.UnitDir} {
		if err := validateAbsDir(label, p); err != nil {
			return err
		}
	}
	if (s.GitHubAppID == "") != (s.GitHubKey == "") {
		return errors.New("GitHub App: informe o App ID e a chave privada juntos (ou deixe ambos vazios)")
	}
	if s.GitHubAppID != "" {
		if !appIDRe.MatchString(s.GitHubAppID) {
			return errors.New("GitHub App ID inválido")
		}
		if _, err := githubapp.Parse(s.GitHubAppID, []byte(s.GitHubKey)); err != nil {
			return fmt.Errorf("GitHub App: %w", err)
		}
	}
	if !nodeenv.ValidVersion(s.NodeVersion) {
		return errors.New("versão do Node.js inválida (use 24, 22.12, 20.18.1 ou lts/*)")
	}
	if s.GitHubAppSlug != "" && !githubapp.AppSlugRe.MatchString(s.GitHubAppSlug) {
		return errors.New("slug do GitHub App inválido")
	}
	if s.GitHubWebhookSecret != "" && (len(s.GitHubWebhookSecret) < 16 || len(s.GitHubWebhookSecret) > 128 || strings.ContainsAny(s.GitHubWebhookSecret, " \t\r\n")) {
		return errors.New("segredo do webhook: use 16 a 128 caracteres, sem espaços")
	}
	if s.CloudflareToken != "" && !cfTokenRe.MatchString(s.CloudflareToken) {
		return errors.New("credencial da Cloudflare inválida (use um API Token ou a Global API Key, sem espaços)")
	}
	if s.CloudflareEmail != "" {
		a, err := mail.ParseAddress(s.CloudflareEmail)
		if err != nil || a.Address != s.CloudflareEmail || len(s.CloudflareEmail) > 254 || strings.ContainsAny(s.CloudflareEmail, " \"'") {
			return errors.New("e-mail da Cloudflare inválido")
		}
		if s.CloudflareToken == "" {
			return errors.New("Cloudflare: informe a credencial junto com o e-mail")
		}
	}
	if cloudflare.GlobalKeyRe.MatchString(s.CloudflareToken) && s.CloudflareEmail == "" {
		return errors.New("Cloudflare: a Global API Key exige o e-mail da conta (um API Token não precisa)")
	}
	return nil
}

// EnsureAppUser creates the unprivileged system user for apps if it does not exist.
func EnsureAppUser(name string) error {
	if name == "" {
		return nil
	}
	if _, err := user.Lookup(name); err == nil {
		return nil
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("o usuário %q não existe e o painel não roda como root para criá-lo", name)
	}
	out, err := exec.Command("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("falha ao criar o usuário %q: %s", name, strings.TrimSpace(string(out)))
	}
	return nil
}

// Check is one item of the server readiness checklist shown on the setup page.
type Check struct {
	Name, Hint string
	OK         bool
	Optional   bool
}

func CheckSystem() []Check {
	have := func(bin string) bool { _, err := exec.LookPath(bin); return err == nil }
	return []Check{
		{Name: "Executando como root", OK: os.Geteuid() == 0, Hint: "necessário para gerenciar systemd e Nginx"},
		{Name: "git", OK: have("git"), Hint: "apt install git"},
		{Name: "systemd (systemctl)", OK: have("systemctl"), Hint: "o painel gerencia serviços via systemd"},
		{Name: "runuser", OK: have("runuser"), Hint: "apt install util-linux", Optional: true},
		{Name: "nginx", OK: have("nginx"), Hint: "apt install nginx — necessário para domínios", Optional: true},
		{Name: "certbot", OK: have("certbot"), Hint: "apt install certbot — necessário para SSL automático", Optional: true},
	}
}

// ValidUsername reports whether name is acceptable as the admin username.
func ValidUsername(name string) bool { return userNameRe.MatchString(name) }
