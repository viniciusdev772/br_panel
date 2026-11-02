// Package nginx publishes domains as Nginx reverse-proxy virtual hosts and obtains
// Let's Encrypt certificates for them with certbot.
package nginx

import (
	"context"
	"fmt"
	"html"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/viniciusdev772/br_panel/internal/execx"
	"github.com/viniciusdev772/br_panel/internal/settings"
)

// Site is one nginx virtual host that reverse-proxies a domain to a local upstream.
type Site struct {
	Key      string // file name suffix: app name, or "_panel" for the panel itself
	Domain   string
	Upstream string // host:port
	MaxBody  string // client_max_body_size, optional

	// Upstreams lists the instances of an app (host:port each). With two or more, the vhost gets
	// an upstream group that balances between them and retries another one when an instance fails.
	Upstreams []string
	RateLimit int  // requests per second per client IP; 0 = no limit
	Gzip      bool // compress text responses

	// Extras are more domains of the same app, each with its own certificate.
	Extras []Extra
	// Users are the lines of the basic-auth file ("name:bcrypt-hash"); any entry turns it on.
	Users []string
	// Maintenance, when set, answers every request with the maintenance page (status 503).
	Maintenance *Maintenance
	ReadTimeout int // proxy_read_timeout in seconds; 0 = 300
}

// Extra is one more domain of a site: it serves the app like the main domain, or, with To set,
// redirects every request to another domain.
type Extra struct {
	Domain    string
	To        string // empty: serves the app
	Permanent bool   // redirect with 301 instead of 302
}

// Maintenance is the content of the maintenance page. Both texts are escaped when written.
type Maintenance struct{ Title, Text string }

// hosts lists the main domain first, then the extras.
func (s Site) hosts() []Extra {
	return append([]Extra{{Domain: s.Domain}}, s.Extras...)
}

// The map lives in one shared file; the variable is prefixed to avoid clashing with user configs.
const commonConf = `# Gerado pelo BR Panel — não edite.
map $http_upgrade $brpanel_connection_upgrade {
    default upgrade;
    ''      close;
}
# Same idea for upstream groups: an empty Connection lets nginx keep connections to the instances.
map $http_upgrade $brpanel_connection_keepalive {
    default upgrade;
    ''      "";
}
`

// Manager writes and reloads virtual hosts using one snapshot of the nginx-related settings.
type Manager struct {
	Dir      string // where vhost files live (e.g. /etc/nginx/conf.d)
	AcmeRoot string // webroot for ACME HTTP-01 challenges
	CertDir  string // Let's Encrypt "live" directory
	Email    string // ACME account e-mail; empty disables certificate issuance
}

// FromSettings builds a Manager from the panel settings.
func FromSettings(s settings.Settings) *Manager {
	return &Manager{Dir: s.NginxDir, AcmeRoot: s.AcmeRoot, CertDir: s.CertDir, Email: s.AcmeEmail}
}

func (m *Manager) vhostPath(key string) string {
	return filepath.Join(m.Dir, "brpanel-"+key+".conf")
}

// The two auxiliary files of a vhost sit next to it; nginx only includes *.conf, so they are
// never read as configuration.
func (m *Manager) htpasswdPath(key string) string {
	return filepath.Join(m.Dir, "brpanel-"+key+".htpasswd")
}

func maintenanceName(key string) string { return "brpanel-" + key + ".maintenance.html" }

func (m *Manager) certFile(domain, name string) string {
	return filepath.Join(m.CertDir, domain, name)
}

// HasCert reports whether a certificate exists for domain.
func (m *Manager) HasCert(domain string) bool {
	_, err := os.Stat(m.certFile(domain, "fullchain.pem"))
	return err == nil
}

// Published reports whether the vhost of key already exists.
func (m *Manager) Published(key string) bool {
	_, err := os.Stat(m.vhostPath(key))
	return err == nil
}

// vhostEnv is what rendering needs from the machine: where the auxiliary files are and which
// domains have a certificate.
type vhostEnv struct {
	AcmeRoot string
	Dir      string
	Cert     func(domain string) (fullchain, privkey string, ok bool)
}

func (m *Manager) env() vhostEnv {
	return vhostEnv{AcmeRoot: m.AcmeRoot, Dir: m.Dir, Cert: func(d string) (string, string, bool) {
		if !m.HasCert(d) {
			return "", "", false
		}
		return m.certFile(d, "fullchain.pem"), m.certFile(d, "privkey.pem"), true
	}}
}

const defaultReadTimeout = 300

const proxyBlock = `    location / {
        proxy_pass http://%s;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection %s;
%s        proxy_read_timeout %ds;
    }
`

// Retry on another instance only for errors and 502/503/504 (an instance restarting during a
// rolling deploy); POST/PATCH are never retried, which is nginx's default and avoids double writes.
const retryDirectives = `        proxy_connect_timeout 5s;
        proxy_next_upstream error timeout http_502 http_503 http_504;
        proxy_next_upstream_timeout 15s;
`

func (s Site) balanced() bool { return len(s.Upstreams) > 1 }

func (s Site) group() string { return "brpanel_" + s.Key }

func (s Site) readTimeout() int {
	if s.ReadTimeout > 0 {
		return s.ReadTimeout
	}
	return defaultReadTimeout
}

// proxyLocation is the location block that forwards to the app.
func proxyLocation(s Site) string {
	if s.balanced() {
		return fmt.Sprintf(proxyBlock, s.group(), "$brpanel_connection_keepalive", retryDirectives, s.readTimeout())
	}
	return fmt.Sprintf(proxyBlock, s.Upstream, "$brpanel_connection_upgrade", "", s.readTimeout())
}

// prelude holds what must live outside the server blocks (http context): the upstream group and
// the rate-limit zone. Both are named after the app, so vhosts never share them.
func prelude(s Site) string {
	var b strings.Builder
	if s.balanced() {
		fmt.Fprintf(&b, "upstream %s {\n    least_conn;\n", s.group())
		for _, u := range s.Upstreams {
			fmt.Fprintf(&b, "    server %s max_fails=3 fail_timeout=10s;\n", u)
		}
		b.WriteString("    keepalive 16;\n}\n\n")
	}
	if s.RateLimit > 0 {
		// 10 MB hold about 80,000 client addresses; the least recently used are evicted beyond that.
		fmt.Fprintf(&b, "limit_req_zone $binary_remote_addr zone=%s:10m rate=%dr/s;\n\n", s.group(), s.RateLimit)
	}
	return b.String()
}

const gzipBlock = `    gzip on;
    gzip_vary on;
    gzip_comp_level 5;
    gzip_min_length 1024;
    gzip_proxied any;
    gzip_types text/plain text/css text/xml application/json application/javascript application/xml application/rss+xml image/svg+xml;

`

// serverExtras are the per-vhost directives of a server block that serves the app.
func serverExtras(s Site, env vhostEnv) string {
	var b strings.Builder
	if s.MaxBody != "" {
		fmt.Fprintf(&b, "    client_max_body_size %s;\n", s.MaxBody)
	}
	if s.Gzip {
		b.WriteString(gzipBlock)
	}
	if s.RateLimit > 0 {
		// A page load fires many requests at once, so the burst is generous; beyond it, 429.
		fmt.Fprintf(&b, "    limit_req zone=%s burst=%d nodelay;\n    limit_req_status 429;\n\n", s.group(), max(s.RateLimit*5, 20))
	}
	if len(s.Users) > 0 && s.Maintenance == nil {
		// The ACME location turns it off again, or certificates could not be renewed.
		fmt.Fprintf(&b, "    auth_basic \"Acesso restrito\";\n    auth_basic_user_file %s/brpanel-%s.htpasswd;\n\n", env.Dir, s.Key)
	}
	return b.String()
}

// maintenanceBlock answers everything with 503 and the maintenance page, so search engines and
// monitors see a temporary outage rather than a page that moved.
func maintenanceBlock(s Site, env vhostEnv) string {
	return fmt.Sprintf(`    error_page 503 @brpanel_maintenance;
    location @brpanel_maintenance {
        root %s;
        rewrite ^ /%s break;
        add_header Retry-After 3600 always;
        add_header Cache-Control "no-store" always;
    }
    location / {
        return 503;
    }
`, env.Dir, maintenanceName(s.Key))
}

// hostContent is what a host serves once any HTTP-to-HTTPS redirect is out of the way.
func hostContent(s Site, h Extra, env vhostEnv) string {
	if h.To != "" {
		code, scheme := 302, "http"
		if h.Permanent {
			code = 301
		}
		if _, _, ok := env.Cert(h.To); ok {
			scheme = "https"
		}
		return fmt.Sprintf("    location / {\n        return %d %s://%s$request_uri;\n    }\n", code, scheme, h.To)
	}
	if s.Maintenance != nil {
		return serverExtras(s, env) + maintenanceBlock(s, env)
	}
	return serverExtras(s, env) + proxyLocation(s)
}

// renderHost builds the server block(s) of one domain. With a certificate, port 80 only serves
// ACME challenges and redirects to HTTPS.
func renderHost(site Site, h Extra, env vhostEnv) string {
	var b strings.Builder
	acme := fmt.Sprintf(`    location ^~ /.well-known/acme-challenge/ {
        auth_basic off;
        root %s;
    }
`, env.AcmeRoot)
	fmt.Fprintf(&b, "server {\n    listen 80;\n    listen [::]:80;\n    server_name %s;\n\n%s\n", h.Domain, acme)
	fullchain, privkey, tls := env.Cert(h.Domain)
	if !tls {
		b.WriteString(hostContent(site, h, env))
		b.WriteString("}\n")
		return b.String()
	}
	b.WriteString("    location / {\n        return 301 https://$host$request_uri;\n    }\n}\n\n")
	fmt.Fprintf(&b, "server {\n    listen 443 ssl;\n    listen [::]:443 ssl;\n    server_name %s;\n\n", h.Domain)
	fmt.Fprintf(&b, "    ssl_certificate %s;\n    ssl_certificate_key %s;\n", fullchain, privkey)
	b.WriteString("    ssl_protocols TLSv1.2 TLSv1.3;\n    ssl_session_cache shared:brpanel_ssl:10m;\n    ssl_session_timeout 1d;\n\n")
	b.WriteString(hostContent(site, h, env))
	b.WriteString("}\n")
	return b.String()
}

// renderVhost builds the whole file of a site: the upstream group, then one set of server blocks
// per domain (each with its own certificate).
func renderVhost(site Site, env vhostEnv) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Gerado pelo BR Panel para %s — não edite.\n%s", site.Domain, prelude(site))
	for i, h := range site.hosts() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(renderHost(site, h, env))
	}
	return b.String()
}

// maintenancePage is the HTML of the maintenance page; title and text come from the admin and are
// escaped, so they can hold any character.
func maintenancePage(m Maintenance) string {
	title, text := html.EscapeString(m.Title), strings.ReplaceAll(html.EscapeString(m.Text), "\n", "<br>\n")
	return `<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>` + title + `</title>
<style>body{font-family:system-ui,sans-serif;background:#f4f4f8;color:#222;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0;padding:16px}main{max-width:480px;text-align:center}h1{font-size:1.6rem;margin:0 0 12px}p{line-height:1.5;color:#555}</style>
</head>
<body><main><h1>` + title + `</h1><p>` + text + `</p></main></body>
</html>
`
}

// HashPassword returns a bcrypt hash that nginx accepts in a basic-auth file ("$2y$" is the
// same algorithm as Go's "$2a$", under the prefix libcrypt knows best).
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return "$2y$" + strings.TrimPrefix(string(h), "$2a$"), nil
}

// writeHtpasswd writes the basic-auth file readable by root and the nginx workers only, when the
// group is known; the apps run as another user and must not read the hashes.
func (m *Manager) writeHtpasswd(key string, users []string) error {
	path := m.htpasswdPath(key)
	if len(users) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := writeFile(path, strings.Join(users, "\n")+"\n", 0o640); err != nil {
		return err
	}
	if gid, ok := m.workerGroup(); ok && os.Chown(path, -1, gid) == nil {
		return os.Chmod(path, 0o640)
	}
	return os.Chmod(path, 0o644) // worker group unknown: nginx must still be able to read it
}

var userDirective = regexp.MustCompile(`(?m)^\s*user\s+([A-Za-z0-9_.-]+)(?:\s+([A-Za-z0-9_.-]+))?\s*;`)

// workerGroup is the group the nginx workers run as, read from the "user" directive of the main
// nginx.conf (it sits next to the conf directory). A guess could leave the file unreadable and
// turn every request into a 500, so without a clear answer the caller makes the file readable.
func (m *Manager) workerGroup() (int, bool) {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(m.Dir), "nginx.conf"))
	if err != nil {
		return 0, false
	}
	mm := userDirective.FindSubmatch(b)
	if mm == nil {
		return 0, false
	}
	name := string(mm[1]) // without a second word, the group has the user's name
	if len(mm[2]) > 0 {
		name = string(mm[2])
	}
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(g.Gid)
	return gid, err == nil
}

// writeAux writes or removes the files the vhost points at.
func (m *Manager) writeAux(site Site) error {
	if err := m.writeHtpasswd(site.Key, site.Users); err != nil {
		return err
	}
	page := filepath.Join(m.Dir, maintenanceName(site.Key))
	if site.Maintenance == nil {
		if err := os.Remove(page); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeFile(page, maintenancePage(*site.Maintenance), 0o644)
}

// applyVhost writes the vhost, validates the whole nginx config and reloads it;
// on failure the previous file (if any) is restored.
func (m *Manager) applyVhost(ctx context.Context, log io.Writer, site Site) error {
	path := m.vhostPath(site.Key)
	prev, readErr := os.ReadFile(path)
	if err := m.writeAux(site); err != nil {
		return err
	}
	if err := writeFile(path, renderVhost(site, m.env()), 0o644); err != nil {
		return err
	}
	if err := execx.Run(ctx, log, "nginx", "-t"); err != nil {
		if readErr == nil {
			_ = os.WriteFile(path, prev, 0o644)
		} else {
			_ = os.Remove(path)
		}
		return fmt.Errorf("configuração nginx inválida (revertida): %w", err)
	}
	return execx.Run(ctx, log, "systemctl", "reload", "nginx")
}

// Ensure publishes the domains over HTTP and, when an ACME e-mail is configured,
// obtains a Let's Encrypt certificate (webroot) for each one that lacks it and switches
// it to HTTPS. Certificate issuance failures (typically DNS not pointing here yet) are not
// fatal: the site stays on HTTP and the next deploy retries.
func (m *Manager) Ensure(ctx context.Context, log io.Writer, site Site) error {
	if err := os.MkdirAll(m.AcmeRoot, 0o755); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(m.Dir, "00-brpanel-common.conf"), commonConf, 0o644); err != nil {
		return err
	}
	if err := m.applyVhost(ctx, log, site); err != nil {
		return err
	}
	var missing []string
	for _, h := range site.hosts() {
		if !m.HasCert(h.Domain) {
			missing = append(missing, h.Domain)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if m.Email == "" {
		fmt.Fprintln(log, "SSL não configurado: informe o e-mail do Let's Encrypt em Configurações para emitir certificados automaticamente.")
		return nil
	}
	issued := false
	for _, domain := range missing {
		if !waitDNS(ctx, log, domain, publicLookup, dnsPollEvery, dnsPollMax) {
			fmt.Fprintf(log, "AVISO: o DNS de %s ainda não resolve nos resolvers públicos; tentando mesmo assim.\n", domain)
		}
		err := retry(ctx, log, certAttempts, certRetryWait, func() error {
			return execx.Run(ctx, log, "certbot", "certonly", "--webroot", "-w", m.AcmeRoot, "-d", domain,
				"--non-interactive", "--agree-tos", "--no-eff-email", "-m", m.Email,
				"--deploy-hook", "systemctl reload nginx")
		})
		if err != nil {
			fmt.Fprintf(log, "AVISO: não foi possível emitir o certificado (%v). Confira se o DNS de %s aponta para este servidor; o site segue em HTTP e o próximo deploy tenta de novo.\n", err, domain)
			continue
		}
		issued = true
	}
	if !issued {
		return nil
	}
	return m.applyVhost(ctx, log, site)
}

// Reload rewrites the vhost of an already published site (for example after its instances
// changed) and reloads nginx, without trying to obtain certificates.
func (m *Manager) Reload(ctx context.Context, log io.Writer, site Site) error {
	return m.applyVhost(ctx, log, site)
}

// Remove deletes the vhost and its auxiliary files, if present, and reloads nginx.
func (m *Manager) Remove(ctx context.Context, log io.Writer, key string) error {
	path := m.vhostPath(key)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	_ = os.Remove(m.htpasswdPath(key))
	_ = os.Remove(filepath.Join(m.Dir, maintenanceName(key)))
	if err := execx.Run(ctx, log, "nginx", "-t"); err != nil {
		return err
	}
	return execx.Run(ctx, log, "systemctl", "reload", "nginx")
}

func writeFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}
