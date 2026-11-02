package nginx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// testEnv is an environment where every domain has (tls) or lacks a certificate.
func testEnv(acme string, tls bool) vhostEnv {
	return vhostEnv{AcmeRoot: acme, Dir: "/etc/nginx/conf.d", Cert: func(string) (string, string, bool) {
		return "/c/full.pem", "/c/key.pem", tls
	}}
}

func TestExtraDomainsServeTheApp(t *testing.T) {
	site := Site{Key: "loja", Domain: "loja.exemplo.com", Upstream: "127.0.0.1:3000",
		Extras: []Extra{{Domain: "shop.exemplo.com"}}}
	out := renderVhost(site, testEnv("/acme", false))
	for _, want := range []string{"server_name loja.exemplo.com;", "server_name shop.exemplo.com;"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "proxy_pass http://127.0.0.1:3000;") != 2 {
		t.Errorf("os dois domínios devem apontar para o app:\n%s", out)
	}
}

func TestEachDomainUsesItsOwnCertificate(t *testing.T) {
	site := Site{Key: "loja", Domain: "loja.exemplo.com", Upstream: "127.0.0.1:3000",
		Extras: []Extra{{Domain: "shop.exemplo.com"}, {Domain: "novo.exemplo.com"}}}
	env := vhostEnv{AcmeRoot: "/acme", Dir: "/d", Cert: func(d string) (string, string, bool) {
		if d == "novo.exemplo.com" {
			return "", "", false // still waiting for its certificate
		}
		return "/live/" + d + "/full.pem", "/live/" + d + "/key.pem", true
	}}
	out := renderVhost(site, env)
	for _, want := range []string{"ssl_certificate /live/loja.exemplo.com/full.pem;", "ssl_certificate /live/shop.exemplo.com/full.pem;"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q", want)
		}
	}
	if strings.Count(out, "listen 443 ssl;") != 2 {
		t.Errorf("só os dois domínios com certificado ouvem em 443:\n%s", out)
	}
	if strings.Count(out, "proxy_pass") != 3 {
		t.Errorf("o domínio sem certificado serve o app em HTTP:\n%s", out)
	}
}

func TestRedirects(t *testing.T) {
	site := Site{Key: "loja", Domain: "loja.exemplo.com", Upstream: "127.0.0.1:3000", Extras: []Extra{
		{Domain: "www.loja.exemplo.com", To: "loja.exemplo.com", Permanent: true},
		{Domain: "velho.exemplo.com", To: "loja.exemplo.com"},
	}}
	// Target with a certificate: redirect to https.
	out := renderVhost(site, testEnv("/acme", true))
	if !strings.Contains(out, "return 301 https://loja.exemplo.com$request_uri;") || !strings.Contains(out, "return 302 https://loja.exemplo.com$request_uri;") {
		t.Errorf("redirecionamentos para https:\n%s", out)
	}
	if strings.Count(out, "proxy_pass") != 1 {
		t.Errorf("domínio de redirecionamento não serve o app:\n%s", out)
	}
	// Target still on HTTP: do not send visitors to a port that is not listening.
	plain := renderVhost(site, testEnv("/acme", false))
	if !strings.Contains(plain, "return 301 http://loja.exemplo.com$request_uri;") {
		t.Errorf("sem certificado o destino é http:\n%s", plain)
	}
}

func TestBasicAuthDirectives(t *testing.T) {
	site := Site{Key: "loja", Domain: "loja.exemplo.com", Upstream: "127.0.0.1:3000"}
	if out := renderVhost(site, testEnv("/acme", false)); strings.Contains(out, "auth_basic \"") {
		t.Errorf("sem usuários não há senha:\n%s", out)
	}
	site.Users = []string{"ana:$2y$10$abc"}
	out := renderVhost(site, testEnv("/acme", false))
	for _, want := range []string{`auth_basic "Acesso restrito";`, "auth_basic_user_file /etc/nginx/conf.d/brpanel-loja.htpasswd;", "auth_basic off;"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "$2y$") {
		t.Error("o hash nunca vai para o arquivo de configuração do vhost")
	}
}

func TestMaintenanceReplacesTheApp(t *testing.T) {
	site := Site{Key: "loja", Domain: "loja.exemplo.com", Upstream: "127.0.0.1:3000", Users: []string{"ana:x"},
		Maintenance: &Maintenance{Title: "Já voltamos", Text: "x"}}
	out := renderVhost(site, testEnv("/acme", true))
	for _, want := range []string{"return 503;", "error_page 503 @brpanel_maintenance;", "brpanel-loja.maintenance.html"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "proxy_pass") || strings.Contains(out, "auth_basic \"") {
		t.Errorf("em manutenção não há app nem senha:\n%s", out)
	}
}

func TestReadTimeout(t *testing.T) {
	site := Site{Key: "a", Domain: "a.exemplo.com", Upstream: "127.0.0.1:1"}
	if out := renderVhost(site, testEnv("/a", false)); !strings.Contains(out, "proxy_read_timeout 300s;") {
		t.Error("padrão de 300 s")
	}
	site.ReadTimeout = 45
	if out := renderVhost(site, testEnv("/a", false)); !strings.Contains(out, "proxy_read_timeout 45s;") {
		t.Error("timeout configurado")
	}
}

func TestMaintenancePageEscapesTheText(t *testing.T) {
	page := maintenancePage(Maintenance{Title: `<script>alert(1)</script>`, Text: "linha 1\n<b>linha 2</b> & \"fim\""})
	if strings.Contains(page, "<script>") || strings.Contains(page, "<b>") {
		t.Errorf("HTML do administrador não pode entrar sem escape:\n%s", page)
	}
	for _, want := range []string{"&lt;script&gt;", "linha 1<br>", "&amp;"} {
		if !strings.Contains(page, want) {
			t.Errorf("faltou %q", want)
		}
	}
}

func TestHashPassword(t *testing.T) {
	h, err := HashPassword("uma senha longa")
	if err != nil || !strings.HasPrefix(h, "$2y$") {
		t.Fatalf("hash = %q, %v", h, err)
	}
	if bcrypt.CompareHashAndPassword([]byte("$2a$"+strings.TrimPrefix(h, "$2y$")), []byte("uma senha longa")) != nil {
		t.Error("o hash deve confirmar a senha")
	}
	if bcrypt.CompareHashAndPassword([]byte("$2a$"+strings.TrimPrefix(h, "$2y$")), []byte("outra")) == nil {
		t.Error("senha errada não pode confirmar")
	}
}

func TestWriteAuxFiles(t *testing.T) {
	m := &Manager{Dir: t.TempDir()}
	site := Site{Key: "loja", Users: []string{"ana:hash"}, Maintenance: &Maintenance{Title: "t", Text: "x"}}
	if err := m.writeAux(site); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"brpanel-loja.htpasswd", "brpanel-loja.maintenance.html"} {
		if _, err := os.Stat(filepath.Join(m.Dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if err := m.writeAux(Site{Key: "loja"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"brpanel-loja.htpasswd", "brpanel-loja.maintenance.html"} {
		if _, err := os.Stat(filepath.Join(m.Dir, f)); !os.IsNotExist(err) {
			t.Errorf("%s deveria ter sido removido: %v", f, err)
		}
	}
}

func TestWorkerGroupComesFromNginxConf(t *testing.T) {
	cur, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	g, err := user.LookupGroupId(cur.Gid)
	if err != nil {
		t.Skip(err)
	}
	want, err := strconv.Atoi(cur.Gid)
	if err != nil {
		t.Skip("sem gid numérico neste sistema")
	}
	root := t.TempDir()
	m := &Manager{Dir: filepath.Join(root, "conf.d")}
	if _, ok := m.workerGroup(); ok {
		t.Error("sem nginx.conf não há grupo")
	}
	conf := filepath.Join(root, "nginx.conf")
	write := func(s string) {
		if err := os.WriteFile(conf, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("# user nobody;\nevents {}\n")
	if _, ok := m.workerGroup(); ok {
		t.Error("comentário não conta")
	}
	write("user " + cur.Username + " " + g.Name + ";\nevents {}\n")
	if gid, ok := m.workerGroup(); !ok || gid != want {
		t.Errorf("gid = %d, %v; queria %d", gid, ok, want)
	}
	write("user usuario-que-nao-existe;\n")
	if _, ok := m.workerGroup(); ok {
		t.Error("grupo inexistente não conta")
	}
}

// ---- real nginx (skipped where it is not installed) ----

func nginxConf(dir string, body string) string {
	var b strings.Builder
	b.WriteString("user root;\npid " + dir + "/nginx.pid;\nerror_log " + dir + "/error.log;\nevents {}\nhttp {\n    access_log off;\n")
	for _, d := range []string{"client_body_temp_path", "proxy_temp_path", "fastcgi_temp_path", "uwsgi_temp_path", "scgi_temp_path"} {
		b.WriteString("    " + d + " " + dir + "/" + d + ";\n")
	}
	b.WriteString(commonConf + body + "}\n")
	return b.String()
}

func selfSigned(t *testing.T, dir string) (cert, key string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "teste"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	cert, key = filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func TestExtrasPassNginxT(t *testing.T) {
	bin, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx não instalado")
	}
	dir := t.TempDir()
	cert, key := selfSigned(t, dir)
	hash, _ := HashPassword("senha-de-exemplo")
	all := Site{Key: "loja", Domain: "loja.exemplo.com", Upstream: "127.0.0.1:3000", Gzip: true, MaxBody: "20m", ReadTimeout: 60,
		Users: []string{"ana:" + hash},
		Extras: []Extra{
			{Domain: "shop.exemplo.com"},
			{Domain: "www.loja.exemplo.com", To: "loja.exemplo.com", Permanent: true},
			{Domain: "velho.exemplo.com", To: "outro.exemplo.net"},
		}}
	maint := all
	maint.Maintenance = &Maintenance{Title: "Voltamos já", Text: "Estamos atualizando."}
	for name, tls := range map[string]bool{"http": false, "https": true} {
		for sn, site := range map[string]Site{"completo": all, "manutencao": maint} {
			t.Run(name+"-"+sn, func(t *testing.T) {
				env := vhostEnv{AcmeRoot: dir, Dir: dir, Cert: func(string) (string, string, bool) { return cert, key, tls }}
				m := &Manager{Dir: dir}
				if err := m.writeAux(site); err != nil {
					t.Fatal(err)
				}
				conf := filepath.Join(dir, "nginx.conf")
				text := nginxConf(dir, renderVhost(site, env))
				if err := os.WriteFile(conf, []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.Command(bin, "-t", "-p", dir, "-c", conf).CombinedOutput(); err != nil {
					t.Fatalf("nginx -t: %v\n%s\n--- config ---\n%s", err, out, text)
				}
			})
		}
	}
}

// It starts a real nginx on a high port and checks what visitors actually get.
func TestRealNginxBehaviour(t *testing.T) {
	bin, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx não instalado")
	}
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "olá do app") }))
	defer app.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	dir := t.TempDir()
	hash, err := HashPassword("senha-de-exemplo")
	if err != nil {
		t.Fatal(err)
	}
	site := Site{Key: "loja", Domain: "loja.exemplo.com", Upstream: app.Listener.Addr().String(), MaxBody: "1m",
		Users: []string{"ana:" + hash},
		Extras: []Extra{
			{Domain: "shop.exemplo.com"},
			{Domain: "www.loja.exemplo.com", To: "loja.exemplo.com", Permanent: true},
			{Domain: "velho.exemplo.com", To: "outro.exemplo.net"},
		}}
	noCert := vhostEnv{AcmeRoot: filepath.Join(dir, "acme"), Dir: dir, Cert: func(string) (string, string, bool) { return "", "", false }}
	conf := filepath.Join(dir, "nginx.conf")
	write := func(s Site) {
		t.Helper()
		if err := (&Manager{Dir: dir}).writeAux(s); err != nil {
			t.Fatal(err)
		}
		text := strings.NewReplacer("listen 80;", fmt.Sprintf("listen %d;", port), "listen [::]:80;", "").Replace(renderVhost(s, noCert))
		if err := os.WriteFile(conf, []byte(nginxConf(dir, text)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(site)
	if out, err := exec.Command(bin, "-t", "-p", dir, "-c", conf).CombinedOutput(); err != nil {
		t.Fatalf("nginx -t: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, "-p", dir, "-c", conf, "-g", "daemon off;")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = exec.Command(bin, "-p", dir, "-c", conf, "-s", "quit").Run()
		_ = cmd.Wait()
	}()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 5 * time.Second}
	get := func(method, host, path, user, pass string, body io.Reader) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), body)
		req.Host = host
		if user != "" {
			req.SetBasicAuth(user, pass)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	// Wait for nginx to accept connections.
	for i := 0; ; i++ {
		if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			_ = c.Close()
			break
		}
		if i > 100 {
			t.Fatal("nginx não subiu")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if r, _ := get("GET", "loja.exemplo.com", "/", "", "", nil); r.StatusCode != 401 {
		t.Errorf("sem senha: %d, queria 401", r.StatusCode)
	}
	if r, _ := get("GET", "loja.exemplo.com", "/", "ana", "errada", nil); r.StatusCode != 401 {
		t.Errorf("senha errada: %d, queria 401", r.StatusCode)
	}
	if r, b := get("GET", "loja.exemplo.com", "/", "ana", "senha-de-exemplo", nil); r.StatusCode != 200 || b != "olá do app" {
		t.Errorf("senha certa: %d %q", r.StatusCode, b)
	}
	if r, b := get("GET", "shop.exemplo.com", "/", "ana", "senha-de-exemplo", nil); r.StatusCode != 200 || b != "olá do app" {
		t.Errorf("domínio extra: %d %q", r.StatusCode, b)
	}
	if r, _ := get("GET", "loja.exemplo.com", "/.well-known/acme-challenge/x", "", "", nil); r.StatusCode != 404 {
		t.Errorf("desafio do Let's Encrypt não pode pedir senha: %d", r.StatusCode)
	}
	r, _ := get("GET", "www.loja.exemplo.com", "/a/b?c=1", "", "", nil)
	if r.StatusCode != 301 || r.Header.Get("Location") != "http://loja.exemplo.com/a/b?c=1" {
		t.Errorf("www: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	r, _ = get("GET", "velho.exemplo.com", "/x", "", "", nil)
	if r.StatusCode != 302 || r.Header.Get("Location") != "http://outro.exemplo.net/x" {
		t.Errorf("velho: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if r, _ := get("POST", "loja.exemplo.com", "/", "ana", "senha-de-exemplo", strings.NewReader(strings.Repeat("x", 2<<20))); r.StatusCode != 413 {
		t.Errorf("upload acima do limite: %d, queria 413", r.StatusCode)
	}

	// Maintenance on: every domain of the app answers 503 with the page, no password asked.
	site.Maintenance = &Maintenance{Title: "Voltamos <já>", Text: "Estamos atualizando."}
	write(site)
	if out, err := exec.Command(bin, "-p", dir, "-c", conf, "-s", "reload").CombinedOutput(); err != nil {
		t.Fatalf("reload: %v\n%s", err, out)
	}
	var resp *http.Response
	var body string
	for i := 0; i < 60; i++ {
		resp, body = get("GET", "loja.exemplo.com", "/qualquer", "", "", nil)
		if resp.StatusCode == 503 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if resp.StatusCode != 503 || !strings.Contains(body, "Voltamos &lt;já&gt;") || !strings.Contains(body, "Estamos atualizando.") {
		t.Errorf("manutenção: %d %q", resp.StatusCode, body)
	}
	if resp.Header.Get("Retry-After") == "" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("cabeçalhos da manutenção: %v", resp.Header)
	}
	if r, _ := get("GET", "shop.exemplo.com", "/", "", "", nil); r.StatusCode != 503 {
		t.Errorf("domínio extra em manutenção: %d", r.StatusCode)
	}
}
