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
	"strings"
	"time"
)

const (
	apiBase = "https://api.cloudflare.com/client/v4"
	timeout = 20 * time.Second
)

// Client talks to the Cloudflare v4 API with a scoped API token (Bearer auth) or,
// when Email is set, with the account's Global API Key.
type Client struct {
	Token string       // scoped API token, or the Global API Key when Email is set
	Email string       // account e-mail; selects X-Auth-Email/X-Auth-Key instead of Bearer
	Base  string       // overridden in tests
	HTTP  *http.Client // optional
}

// New returns a Client, or nil when no credential is configured.
func New(email, token string) *Client {
	if token == "" {
		return nil
	}
	return &Client{Token: token, Email: email, Base: apiBase}
}

type envelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
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
	if c.Email != "" {
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
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if len(env.Errors) > 0 {
			msg = env.Errors[0].Message
		}
		return fmt.Errorf("cloudflare: %s", msg)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
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
