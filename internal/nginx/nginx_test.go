package nginx

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
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

func TestWaitDNSWaitsUntilTheNameResolves(t *testing.T) {
	calls := 0
	lookup := func(context.Context, string) ([]string, error) {
		if calls++; calls < 3 {
			return nil, errors.New("NXDOMAIN")
		}
		return []string{"203.0.113.9"}, nil
	}
	var log strings.Builder
	if !waitDNS(context.Background(), &log, "p.exemplo.com", lookup, time.Millisecond, time.Second) || calls != 3 {
		t.Fatalf("calls = %d", calls)
	}
	if !strings.Contains(log.String(), "Aguardando o DNS") {
		t.Errorf("log: %q", log.String())
	}
}

func TestWaitDNSGivesUp(t *testing.T) {
	never := func(context.Context, string) ([]string, error) { return nil, errors.New("NXDOMAIN") }
	if waitDNS(context.Background(), io.Discard, "p.exemplo.com", never, 5*time.Millisecond, 20*time.Millisecond) {
		t.Error("deveria desistir depois do limite")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitDNS(ctx, io.Discard, "p.exemplo.com", never, time.Hour, time.Hour) {
		t.Error("contexto cancelado deveria interromper a espera")
	}
}

func TestRetry(t *testing.T) {
	n := 0
	err := retry(context.Background(), io.Discard, 3, time.Millisecond, func() error {
		if n++; n < 3 {
			return errors.New("falhou")
		}
		return nil
	})
	if err != nil || n != 3 {
		t.Errorf("n=%d err=%v", n, err)
	}
	n = 0
	err = retry(context.Background(), io.Discard, 2, time.Millisecond, func() error { n++; return errors.New("sempre") })
	if err == nil || n != 2 {
		t.Errorf("n=%d err=%v", n, err)
	}
}
