package nginx

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"
)

// Timings for the certificate step. Variables so tests do not wait.
var (
	dnsPollEvery  = 5 * time.Second
	dnsPollMax    = 2 * time.Minute
	certAttempts  = 3
	certRetryWait = 30 * time.Second
)

// publicLookup asks public resolvers directly (not the host's cache), the way a
// certificate authority will see the name. One answer from either is enough.
func publicLookup(ctx context.Context, host string) ([]string, error) {
	var lastErr error
	for _, server := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "udp", server)
		}}
		ips, err := r.LookupHost(ctx, host)
		if err == nil && len(ips) > 0 {
			return ips, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// waitDNS polls until domain resolves, up to max. A record created seconds ago (for
// example by the Cloudflare integration) is not visible everywhere yet, and Let's
// Encrypt answers a premature request with NXDOMAIN and burns a failed validation.
// It reports whether the name resolved; callers go on either way.
func waitDNS(ctx context.Context, log io.Writer, domain string, lookup func(context.Context, string) ([]string, error), every, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for announced := false; ; {
		if ips, err := lookup(ctx, domain); err == nil && len(ips) > 0 {
			return true
		}
		if time.Now().Add(every).After(deadline) {
			return false
		}
		if !announced {
			fmt.Fprintf(log, "Aguardando o DNS de %s propagar…\n", domain)
			announced = true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(every):
		}
	}
}

// retry runs fn up to attempts times, waiting between failures, and returns the last error.
func retry(ctx context.Context, log io.Writer, attempts int, wait time.Duration, fn func() error) error {
	var err error
	for i := 1; i <= attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i == attempts {
			break
		}
		fmt.Fprintf(log, "Tentativa %d/%d falhou (%v); nova tentativa em %s.\n", i, attempts, err, wait)
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
	}
	return err
}
