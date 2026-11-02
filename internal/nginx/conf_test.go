package nginx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The generated vhosts must be accepted by the real nginx (skipped where it is not installed).
func TestRenderedVhostsPassNginxT(t *testing.T) {
	bin, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx não instalado")
	}
	sites := map[string]Site{
		"simples":   {Key: "simples", Domain: "a.exemplo.com", Upstream: "127.0.0.1:3000", Gzip: true},
		"limitado":  {Key: "limitado", Domain: "b.exemplo.com", Upstream: "127.0.0.1:3000", RateLimit: 10, Gzip: true, MaxBody: "6m"},
		"replicas":  {Key: "replicas", Domain: "c.exemplo.com", Upstream: "127.0.0.1:3001", Upstreams: []string{"127.0.0.1:3001", "127.0.0.1:3002", "127.0.0.1:3003"}, RateLimit: 5, Gzip: true},
		"com-hifen": {Key: "api-v2", Domain: "d.exemplo.com", Upstream: "127.0.0.1:3010", Upstreams: []string{"127.0.0.1:3010", "127.0.0.1:3011"}},
	}
	for name, site := range sites {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			var b strings.Builder
			b.WriteString("pid " + dir + "/nginx.pid;\nerror_log " + dir + "/error.log;\nevents {}\nhttp {\n    access_log off;\n")
			for _, d := range []string{"client_body_temp_path", "proxy_temp_path", "fastcgi_temp_path", "uwsgi_temp_path", "scgi_temp_path"} {
				b.WriteString("    " + d + " " + dir + "/" + d + ";\n")
			}
			b.WriteString(commonConf + renderVhost(site, dir, "", "", false) + "}\n")
			conf := filepath.Join(dir, "nginx.conf")
			if err := os.WriteFile(conf, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(bin, "-t", "-p", dir, "-c", conf).CombinedOutput(); err != nil {
				t.Fatalf("nginx -t: %v\n%s\n--- config ---\n%s", err, out, b.String())
			}
		})
	}
}
