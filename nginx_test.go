package main

import (
	"strings"
	"testing"
)

func TestRenderVhost(t *testing.T) {
	site := Site{Key: "web", Domain: "app.exemplo.com", Upstream: "127.0.0.1:3000"}
	http := renderVhost(site, "/var/www/acme", "/c/full.pem", "/c/key.pem", false)
	for _, want := range []string{"server_name app.exemplo.com;", "proxy_pass http://127.0.0.1:3000;", "acme-challenge", "$brpanel_connection_upgrade"} {
		if !strings.Contains(http, want) {
			t.Errorf("http: faltou %q", want)
		}
	}
	if strings.Contains(http, "443") {
		t.Error("vhost sem TLS não deve ouvir em 443")
	}
	tls := renderVhost(site, "/var/www/acme", "/c/full.pem", "/c/key.pem", true)
	for _, want := range []string{"listen 443 ssl;", "ssl_certificate /c/full.pem;", "ssl_certificate_key /c/key.pem;", "return 301 https://$host$request_uri;"} {
		if !strings.Contains(tls, want) {
			t.Errorf("tls: faltou %q", want)
		}
	}
	if strings.Count(tls, "proxy_pass") != 1 {
		t.Error("proxy_pass deve aparecer só no bloco 443")
	}
}

func TestValidateDomain(t *testing.T) {
	for _, d := range []string{"", "exemplo.com", "app.exemplo.com.br", "a-b.xn--p1ai"} {
		if validateDomain(d) != nil {
			t.Errorf("%q deveria ser válido", d)
		}
	}
	for _, d := range []string{"localhost", "1.2.3.4", "*.exemplo.com", "Exemplo.com", "a b.com", "exemplo.com;x", "-a.com", "a.com:80", "a..com"} {
		if validateDomain(d) == nil {
			t.Errorf("%q deveria ser inválido", d)
		}
	}
}
