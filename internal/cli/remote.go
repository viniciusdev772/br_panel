package cli

import (
	"bufio"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultPort is where the panel listens until it gets a domain with SSL.
const DefaultPort = "8080"

// config is what `brpanel login` saves on the machine running the CLI (never on the server).
type config struct {
	Server string `json:"server"` // base URL, e.g. https://painel.example.com
	Token  string `json:"token"`
}

// configDir is a variable so tests can use a temporary directory.
var configDir = os.UserConfigDir

func configPath() (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "brpanel", "config.json"), nil
}

func loadConfig() (config, bool) {
	p, err := configPath()
	if err != nil {
		return config{}, false
	}
	b, err := os.ReadFile(p)
	var c config
	if err != nil || json.Unmarshal(b, &c) != nil || c.Server == "" || c.Token == "" {
		return config{}, false
	}
	return c, true
}

func saveConfig(c config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(c)
	return os.WriteFile(p, b, 0o600) // the token is a password: owner only (Windows keeps it in the user profile)
}

// bearerRT adds the access token to every request.
type bearerRT struct {
	token string
	next  http.RoundTripper
}

func (b bearerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// normalizeServer accepts what people type: a domain (https), an IP (http on the panel's port 8080)
// or a full URL. plain is true when the token would cross the network unencrypted.
func normalizeServer(in string) (base string, plain bool, err error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", false, errors.New("o endereço do servidor está vazio")
	}
	if !strings.Contains(in, "://") {
		u, perr := url.Parse("//" + in)
		if perr != nil || u.Hostname() == "" {
			return "", false, fmt.Errorf("endereço inválido: %q", in)
		}
		host := u.Host // path, query and spaces typed after the address are dropped
		if net.ParseIP(u.Hostname()) != nil || u.Hostname() == "localhost" {
			if u.Port() == "" {
				host = net.JoinHostPort(u.Hostname(), DefaultPort)
			}
			in = "http://" + host
		} else {
			in = "https://" + host
		}
	}
	u, perr := url.Parse(in)
	if perr != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false, fmt.Errorf("endereço inválido: %q (use um domínio, um IP ou http(s)://…)", in)
	}
	return u.Scheme + "://" + u.Host, u.Scheme == "http" && !isLoopback(u.Hostname()), nil
}

// prompt asks one line on stderr and reads the answer; secret turns off the echo when stdin is a terminal.
func prompt(in *bufio.Reader, src io.Reader, label string, secret bool) string {
	fmt.Fprint(os.Stderr, label)
	tty := false
	if f, ok := src.(*os.File); ok {
		if st, err := f.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
			tty = true
		}
	}
	if secret && tty {
		off := exec.Command("stty", "-echo")
		off.Stdin = os.Stdin
		if off.Run() == nil {
			defer func() {
				on := exec.Command("stty", "echo")
				on.Stdin = os.Stdin
				_ = on.Run()
				fmt.Fprintln(os.Stderr)
			}()
		}
	}
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

func isTerminal(src io.Reader) bool {
	f, ok := src.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// explain turns connection failures of a remote server into something an agent can act on.
func explain(err error, base string) error {
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var inv x509.CertificateInvalidError
	switch {
	case errors.As(err, &unknown), errors.As(err, &host), errors.As(err, &inv):
		return fail("o certificado SSL de "+base+" não é confiável ou não é do endereço usado",
			"DICA: use o domínio do painel (com SSL válido) em vez do IP, ou corrija o certificado")
	}
	var op *net.OpError
	if errors.As(err, &op) || strings.Contains(err.Error(), "no such host") {
		return fail("não consegui conectar a "+base,
			"DICA: confira o endereço, a porta (padrão "+DefaultPort+") e se o firewall do servidor deixa passar",
			"DICA: para trocar de servidor: brpanel login ENDEREÇO")
	}
	return err
}
