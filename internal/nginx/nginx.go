// Package nginx publishes domains as Nginx reverse-proxy virtual hosts and obtains
// Let's Encrypt certificates for them with certbot.
package nginx

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

func (m *Manager) certFile(domain, name string) string {
	return filepath.Join(m.CertDir, domain, name)
}

// HasCert reports whether a certificate exists for domain.
func (m *Manager) HasCert(domain string) bool {
	_, err := os.Stat(m.certFile(domain, "fullchain.pem"))
	return err == nil
}

const proxyBlock = `    location / {
        proxy_pass http://%s;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection %s;
%s        proxy_read_timeout 300s;
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

// proxyLocation is the location block that forwards to the app.
func proxyLocation(s Site) string {
	if s.balanced() {
		return fmt.Sprintf(proxyBlock, s.group(), "$brpanel_connection_keepalive", retryDirectives)
	}
	return fmt.Sprintf(proxyBlock, s.Upstream, "$brpanel_connection_upgrade", "")
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

// serverExtras are the per-vhost directives of the server block that proxies to the app.
func serverExtras(s Site) string {
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
	return b.String()
}

// renderVhost builds the server block(s). With tls, port 80 only serves ACME
// challenges and redirects to HTTPS.
func renderVhost(site Site, acmeRoot, fullchain, privkey string, tls bool) string {
	var b strings.Builder
	acme := fmt.Sprintf(`    location ^~ /.well-known/acme-challenge/ {
        root %s;
    }
`, acmeRoot)
	body := serverExtras(site)
	fmt.Fprintf(&b, "# Gerado pelo BR Panel para %s — não edite.\n%sserver {\n    listen 80;\n    listen [::]:80;\n    server_name %s;\n\n%s\n", site.Domain, prelude(site), site.Domain, acme)
	if tls {
		b.WriteString("    location / {\n        return 301 https://$host$request_uri;\n    }\n}\n\n")
		fmt.Fprintf(&b, "server {\n    listen 443 ssl;\n    listen [::]:443 ssl;\n    server_name %s;\n\n", site.Domain)
		fmt.Fprintf(&b, "    ssl_certificate %s;\n    ssl_certificate_key %s;\n", fullchain, privkey)
		b.WriteString("    ssl_protocols TLSv1.2 TLSv1.3;\n    ssl_session_cache shared:brpanel_ssl:10m;\n    ssl_session_timeout 1d;\n\n")
		b.WriteString(body)
		b.WriteString(proxyLocation(site))
		b.WriteString("}\n")
		return b.String()
	}
	b.WriteString(body)
	b.WriteString(proxyLocation(site))
	b.WriteString("}\n")
	return b.String()
}

// applyVhost writes the vhost, validates the whole nginx config and reloads it;
// on failure the previous file (if any) is restored.
func (m *Manager) applyVhost(ctx context.Context, log io.Writer, site Site, tls bool) error {
	path := m.vhostPath(site.Key)
	prev, readErr := os.ReadFile(path)
	conf := renderVhost(site, m.AcmeRoot, m.certFile(site.Domain, "fullchain.pem"), m.certFile(site.Domain, "privkey.pem"), tls)
	if err := writeFile(path, conf, 0o644); err != nil {
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

// Ensure publishes the domain over HTTP and, when an ACME e-mail is configured,
// obtains a Let's Encrypt certificate (webroot) and switches to HTTPS.
// Certificate issuance failures (typically DNS not pointing here yet) are not
// fatal: the site stays on HTTP and the next deploy retries.
func (m *Manager) Ensure(ctx context.Context, log io.Writer, site Site) error {
	if err := os.MkdirAll(m.AcmeRoot, 0o755); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(m.Dir, "00-brpanel-common.conf"), commonConf, 0o644); err != nil {
		return err
	}
	hasCert := m.HasCert(site.Domain)
	if err := m.applyVhost(ctx, log, site, hasCert); err != nil {
		return err
	}
	if hasCert {
		return nil
	}
	if m.Email == "" {
		fmt.Fprintln(log, "SSL não configurado: informe o e-mail do Let's Encrypt em Configurações para emitir certificados automaticamente.")
		return nil
	}
	if !waitDNS(ctx, log, site.Domain, publicLookup, dnsPollEvery, dnsPollMax) {
		fmt.Fprintf(log, "AVISO: o DNS de %s ainda não resolve nos resolvers públicos; tentando mesmo assim.\n", site.Domain)
	}
	err := retry(ctx, log, certAttempts, certRetryWait, func() error {
		return execx.Run(ctx, log, "certbot", "certonly", "--webroot", "-w", m.AcmeRoot, "-d", site.Domain,
			"--non-interactive", "--agree-tos", "--no-eff-email", "-m", m.Email,
			"--deploy-hook", "systemctl reload nginx")
	})
	if err != nil {
		fmt.Fprintf(log, "AVISO: não foi possível emitir o certificado (%v). Confira se o DNS de %s aponta para este servidor; o site segue em HTTP e o próximo deploy tenta de novo.\n", err, site.Domain)
		return nil
	}
	return m.applyVhost(ctx, log, site, true)
}

// Remove deletes the vhost, if present, and reloads nginx.
func (m *Manager) Remove(ctx context.Context, log io.Writer, key string) error {
	path := m.vhostPath(key)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return err
	}
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
