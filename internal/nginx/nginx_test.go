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
	http := renderVhost(site, testEnv("/var/www/acme", false))
	for _, want := range []string{"server_name app.exemplo.com;", "proxy_pass http://127.0.0.1:3000;", "acme-challenge", "$brpanel_connection_upgrade"} {
		if !strings.Contains(http, want) {
			t.Errorf("http: faltou %q", want)
		}
	}
	if strings.Contains(http, "443") {
		t.Error("vhost sem TLS não deve ouvir em 443")
	}
	tls := renderVhost(site, testEnv("/var/www/acme", true))
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

func TestRenderVhostSingleInstanceHasNoUpstreamGroup(t *testing.T) {
	site := Site{Key: "web", Domain: "app.exemplo.com", Upstream: "127.0.0.1:3000"}
	out := renderVhost(site, testEnv("/acme", true))
	for _, bad := range []string{"upstream ", "limit_req", "gzip", "$brpanel_connection_keepalive"} {
		if strings.Contains(out, bad) {
			t.Errorf("app simples não deve ter %q:\n%s", bad, out)
		}
	}
}

func TestRenderVhostBalancesReplicas(t *testing.T) {
	site := Site{Key: "api", Domain: "api.exemplo.com", Upstream: "127.0.0.1:3001",
		Upstreams: []string{"127.0.0.1:3001", "127.0.0.1:3002", "127.0.0.1:3003"}}
	out := renderVhost(site, testEnv("/acme", false))
	for _, want := range []string{
		"upstream brpanel_api {", "least_conn;", "server 127.0.0.1:3002 max_fails=3 fail_timeout=10s;", "keepalive 16;",
		"proxy_pass http://brpanel_api;", "proxy_set_header Connection $brpanel_connection_keepalive;",
		"proxy_next_upstream error timeout http_502 http_503 http_504;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	// nginx < 1.29.7 needs the balancing method before keepalive
	if strings.Index(out, "least_conn;") > strings.Index(out, "keepalive 16;") {
		t.Error("least_conn deve vir antes de keepalive")
	}
	if strings.Contains(out, "non_idempotent") {
		t.Error("POST não pode ser repetido em outra instância")
	}
	if strings.Index(out, "upstream brpanel_api") > strings.Index(out, "server {") {
		t.Error("upstream deve ficar fora dos blocos server")
	}
}

func TestRenderVhostRateLimitAndGzip(t *testing.T) {
	site := Site{Key: "loja", Domain: "loja.exemplo.com", Upstream: "127.0.0.1:3000", RateLimit: 10, Gzip: true}
	tls := renderVhost(site, testEnv("/acme", true))
	for _, want := range []string{
		"limit_req_zone $binary_remote_addr zone=brpanel_loja:10m rate=10r/s;",
		"limit_req zone=brpanel_loja burst=50 nodelay;", "limit_req_status 429;", "gzip on;", "gzip_vary on;", "application/json",
	} {
		if !strings.Contains(tls, want) {
			t.Errorf("faltou %q:\n%s", want, tls)
		}
	}
	if strings.Count(tls, "limit_req zone") != 1 || strings.Count(tls, "gzip on;") != 1 {
		t.Error("limite e gzip só no bloco 443 que serve o app")
	}
	if strings.Index(tls, "limit_req_zone") > strings.Index(tls, "server {") {
		t.Error("limit_req_zone só vale no contexto http, antes dos blocos server")
	}
	low := renderVhost(Site{Key: "x", Domain: "x.com", Upstream: "127.0.0.1:1", RateLimit: 1}, testEnv("/a", false))
	if !strings.Contains(low, "burst=20 ") {
		t.Errorf("burst mínimo de 20:\n%s", low)
	}
}
