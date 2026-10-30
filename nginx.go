package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Site is one nginx virtual host that reverse-proxies a domain to a local upstream.
type Site struct {
	Key      string // file name suffix: app name, or "_panel" for the panel itself
	Domain   string
	Upstream string // host:port
	MaxBody  string // client_max_body_size, optional
}

// The map lives in one shared file; the variable is prefixed to avoid clashing with user configs.
const commonConf = `# Gerado pelo BR Panel — não edite.
map $http_upgrade $brpanel_connection_upgrade {
    default upgrade;
    ''      close;
}
`

func (s *Server) vhostPath(key string) string {
	return filepath.Join(s.cfg.NginxDir, "brpanel-"+key+".conf")
}

func (s *Server) certFile(domain, name string) string {
	return filepath.Join(s.cfg.CertDir, domain, name)
}

func (s *Server) hasCert(domain string) bool {
	_, err := os.Stat(s.certFile(domain, "fullchain.pem"))
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
        proxy_set_header Connection $brpanel_connection_upgrade;
        proxy_read_timeout 300s;
    }
`

// renderVhost builds the server block(s). With tls, port 80 only serves ACME
// challenges and redirects to HTTPS.
func renderVhost(site Site, acmeRoot, fullchain, privkey string, tls bool) string {
	var b strings.Builder
	acme := fmt.Sprintf(`    location ^~ /.well-known/acme-challenge/ {
        root %s;
    }
`, acmeRoot)
	body := ""
	if site.MaxBody != "" {
		body = fmt.Sprintf("    client_max_body_size %s;\n", site.MaxBody)
	}
	fmt.Fprintf(&b, "# Gerado pelo BR Panel para %s — não edite.\nserver {\n    listen 80;\n    listen [::]:80;\n    server_name %s;\n\n%s\n", site.Domain, site.Domain, acme)
	if tls {
		b.WriteString("    location / {\n        return 301 https://$host$request_uri;\n    }\n}\n\n")
		fmt.Fprintf(&b, "server {\n    listen 443 ssl;\n    listen [::]:443 ssl;\n    server_name %s;\n\n", site.Domain)
		fmt.Fprintf(&b, "    ssl_certificate %s;\n    ssl_certificate_key %s;\n", fullchain, privkey)
		b.WriteString("    ssl_protocols TLSv1.2 TLSv1.3;\n    ssl_session_cache shared:brpanel_ssl:10m;\n    ssl_session_timeout 1d;\n\n")
		b.WriteString(body)
		fmt.Fprintf(&b, proxyBlock, site.Upstream)
		b.WriteString("}\n")
		return b.String()
	}
	b.WriteString(body)
	fmt.Fprintf(&b, proxyBlock, site.Upstream)
	b.WriteString("}\n")
	return b.String()
}

// applyVhost writes the vhost, validates the whole nginx config and reloads it;
// on failure the previous file (if any) is restored.
func (s *Server) applyVhost(ctx context.Context, log io.Writer, site Site, tls bool) error {
	path := s.vhostPath(site.Key)
	prev, readErr := os.ReadFile(path)
	conf := renderVhost(site, s.cfg.AcmeRoot, s.certFile(site.Domain, "fullchain.pem"), s.certFile(site.Domain, "privkey.pem"), tls)
	if err := writeFile(path, conf, 0o644); err != nil {
		return err
	}
	if err := sysRun(ctx, log, "nginx", "-t"); err != nil {
		if readErr == nil {
			_ = os.WriteFile(path, prev, 0o644)
		} else {
			_ = os.Remove(path)
		}
		return fmt.Errorf("configuração nginx inválida (revertida): %w", err)
	}
	return sysRun(ctx, log, "systemctl", "reload", "nginx")
}

// ensureSite publishes the domain over HTTP and, when PANEL_ACME_EMAIL is set,
// obtains a Let's Encrypt certificate (webroot) and switches to HTTPS.
// Certificate issuance failures (typically DNS not pointing here yet) are not
// fatal: the site stays on HTTP and the next deploy retries.
func (s *Server) ensureSite(ctx context.Context, log io.Writer, site Site) error {
	if err := os.MkdirAll(s.cfg.AcmeRoot, 0o755); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(s.cfg.NginxDir, "00-brpanel-common.conf"), commonConf, 0o644); err != nil {
		return err
	}
	hasCert := s.hasCert(site.Domain)
	if err := s.applyVhost(ctx, log, site, hasCert); err != nil {
		return err
	}
	if hasCert {
		return nil
	}
	if s.cfg.AcmeEmail == "" {
		fmt.Fprintln(log, "SSL não configurado: defina PANEL_ACME_EMAIL para emitir certificados automaticamente.")
		return nil
	}
	err := sysRun(ctx, log, "certbot", "certonly", "--webroot", "-w", s.cfg.AcmeRoot, "-d", site.Domain,
		"--non-interactive", "--agree-tos", "--no-eff-email", "-m", s.cfg.AcmeEmail,
		"--deploy-hook", "systemctl reload nginx")
	if err != nil {
		fmt.Fprintf(log, "AVISO: não foi possível emitir o certificado (%v). Confira se o DNS de %s aponta para este servidor; o site segue em HTTP e o próximo deploy tenta de novo.\n", err, site.Domain)
		return nil
	}
	return s.applyVhost(ctx, log, site, true)
}

// removeSite deletes the vhost, if present, and reloads nginx.
func (s *Server) removeSite(ctx context.Context, log io.Writer, key string) error {
	path := s.vhostPath(key)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := sysRun(ctx, log, "nginx", "-t"); err != nil {
		return err
	}
	return sysRun(ctx, log, "systemctl", "reload", "nginx")
}
