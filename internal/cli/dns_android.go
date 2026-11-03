//go:build android

package cli

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Android (Termux) has no /etc/resolv.conf, and Go's pure resolver then asks 127.0.0.1:53, which fails.
// Point the default resolver at the servers Termux keeps under $PREFIX/etc, or at public ones.
func init() {
	servers := termuxNameservers()
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := net.Dialer{Timeout: 5 * time.Second}
		var last error
		for _, s := range servers {
			c, err := d.DialContext(ctx, network, net.JoinHostPort(s, "53"))
			if err == nil {
				return c, nil
			}
			last = err
		}
		return nil, last
	}}
}

func termuxNameservers() []string {
	files := []string{"/etc/resolv.conf"}
	if p := os.Getenv("PREFIX"); p != "" {
		files = append(files, filepath.Join(p, "etc", "resolv.conf"))
	}
	var out []string
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			if fs := strings.Fields(sc.Text()); len(fs) >= 2 && fs[0] == "nameserver" && net.ParseIP(fs[1]) != nil {
				out = append(out, fs[1])
			}
		}
		fh.Close()
	}
	return append(out, "1.1.1.1", "8.8.8.8")
}
