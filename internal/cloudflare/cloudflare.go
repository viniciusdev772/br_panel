// Package cloudflare keeps the DNS A record of an app domain pointing at this server,
// using a Cloudflare API token. It only needs the standard library.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	apiBase = "https://api.cloudflare.com/client/v4"
	timeout = 20 * time.Second
)

// GlobalKeyRe matches a Global API Key: 37 lowercase hex characters. Cloudflare rejects
// anything else sent as X-Auth-Key, and rejects a Global API Key sent as a Bearer token
// ("Invalid request headers"), so the header style must follow the credential's format.
var GlobalKeyRe = regexp.MustCompile(`^[0-9a-f]{37}$`)

// Client talks to the Cloudflare v4 API with either a scoped API token (Bearer) or the
// account's Global API Key (X-Auth-Email + X-Auth-Key), chosen by the credential's format.
type Client struct {
	Token string       // API token or Global API Key
	Email string       // account e-mail; used only with a Global API Key
	Base  string       // overridden in tests
	HTTP  *http.Client // optional
}

// New returns a Client, or nil when no credential is configured.
func New(email, token string) *Client {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return &Client{Token: token, Email: strings.TrimSpace(email), Base: apiBase}
}

func (c *Client) isGlobalKey() bool { return GlobalKeyRe.MatchString(c.Token) }

// Check makes one cheap authenticated read, so a wrong credential is reported when it is
// saved instead of during a deploy. Listing zones needs Zone:Read, which EnsureA needs anyway.
func (c *Client) Check(ctx context.Context) error {
	if c.isGlobalKey() && c.Email == "" {
		return errors.New("a Global API Key exige o e-mail da conta Cloudflare")
	}
	var zones []struct{}
	return c.do(ctx, http.MethodGet, "/zones?per_page=1", nil, &zones)
}

type envelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code       int    `json:"code"`
		Message    string `json:"message"`
		ErrorChain []struct {
			Message string `json:"message"`
		} `json:"error_chain"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
}

// do sends one request and decodes the "result" field into out. The token is
// only ever placed in request headers, never in the URL or in errors.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return err
	}
	if c.isGlobalKey() {
		if c.Email == "" {
			return errors.New("cloudflare: a Global API Key exige o e-mail da conta")
		}
		req.Header.Set("X-Auth-Email", c.Email)
		req.Header.Set("X-Auth-Key", c.Token)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare: %w", err)
	}
	defer resp.Body.Close()
	var env envelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&env); err != nil {
		return fmt.Errorf("cloudflare: resposta inválida (HTTP %d)", resp.StatusCode)
	}
	if !env.Success {
		return errors.New("cloudflare: " + apiError(resp.StatusCode, env))
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// apiError turns Cloudflare's error list into a message that says what to fix.
func apiError(status int, env envelope) string {
	if len(env.Errors) == 0 {
		return fmt.Sprintf("HTTP %d", status)
	}
	e := env.Errors[0]
	msg := e.Message
	for _, c := range e.ErrorChain {
		msg += " (" + c.Message + ")"
	}
	switch e.Code {
	case 9103, 9106:
		msg += ": confira o e-mail da conta e a Global API Key"
	case 9109:
		msg += ": o API Token está incorreto, expirou ou foi revogado"
	case 10000:
		msg += ": a credencial não tem permissão para isso (precisa de Zone:Read e DNS:Edit)"
	}
	return msg
}

// zoneID finds the zone that contains domain by trying the domain and then its
// parents ("a.b.example.com" -> "b.example.com" -> "example.com").
func (c *Client) zoneID(ctx context.Context, domain string) (string, error) {
	labels := strings.Split(domain, ".")
	for i := 0; i < len(labels)-1; i++ {
		var zones []struct {
			ID string `json:"id"`
		}
		name := strings.Join(labels[i:], ".")
		if err := c.do(ctx, http.MethodGet, "/zones?per_page=1&name="+url.QueryEscape(name), nil, &zones); err != nil {
			return "", err
		}
		if len(zones) > 0 {
			return zones[0].ID, nil
		}
	}
	return "", fmt.Errorf("nenhuma zona de %s nesta conta da Cloudflare (o token precisa de Zone:Read e DNS:Edit nessa zona)", domain)
}

type record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

// EnsureA makes domain resolve to ip with an A record (DNS only, TTL automatic).
// It creates the record when missing, updates it when the IP differs and does
// nothing when it is already right. It never deletes anything.
func (c *Client) EnsureA(ctx context.Context, domain, ip string) (string, error) {
	if net.ParseIP(ip) == nil || strings.Contains(ip, ":") {
		return "", fmt.Errorf("IPv4 do servidor inválido: %q", ip)
	}
	zone, err := c.zoneID(ctx, domain)
	if err != nil {
		return "", err
	}
	var recs []record
	q := "/zones/" + zone + "/dns_records?type=A&name=" + url.QueryEscape(domain)
	if err := c.do(ctx, http.MethodGet, q, nil, &recs); err != nil {
		return "", err
	}
	want := record{Type: "A", Name: domain, Content: ip, TTL: 1}
	switch {
	case len(recs) == 0:
		return "criado", c.do(ctx, http.MethodPost, "/zones/"+zone+"/dns_records", want, nil)
	case recs[0].Content == ip:
		return "já estava correto", nil
	default:
		want.Proxied = recs[0].Proxied // keep the user's orange-cloud choice
		return "atualizado", c.do(ctx, http.MethodPut, "/zones/"+zone+"/dns_records/"+recs[0].ID, want, nil)
	}
}

// EnsureServerA points domain at this machine's public IPv4 and reports that IP.
func (c *Client) EnsureServerA(ctx context.Context, domain string) (ip, result string, err error) {
	if ip, err = ServerIP(); err != nil {
		return "", "", err
	}
	result, err = c.EnsureA(ctx, domain, ip)
	return ip, result, err
}

// ServerIP returns the IPv4 this machine uses to reach the internet (a UDP
// "connect" sends no packet). On a VPS this is the public address; behind NAT
// it would be private, which EnsureA callers should treat as a misconfiguration.
func ServerIP() (string, error) {
	c, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer c.Close()
	a, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok || a.IP.IsUnspecified() {
		return "", errors.New("não foi possível descobrir o IP do servidor")
	}
	if a.IP.IsPrivate() || a.IP.IsLoopback() {
		return "", fmt.Errorf("o IP do servidor (%s) não é público; o servidor está atrás de NAT?", a.IP)
	}
	return a.IP.String(), nil
}
