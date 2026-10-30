package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAPI serves one zone "example.com" and records the writes it receives.
type fakeAPI struct {
	records []record
	writes  []string
	auth    string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.auth = r.Header.Get("Authorization")
	reply := func(v any) {
		b, _ := json.Marshal(v)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": json.RawMessage(b)})
	}
	switch {
	case r.URL.Path == "/zones":
		if r.URL.Query().Get("name") == "example.com" {
			reply([]map[string]string{{"id": "z1"}})
			return
		}
		reply([]map[string]string{})
	case r.URL.Path == "/zones/z1/dns_records" && r.Method == http.MethodGet:
		reply(f.records)
	case strings.HasPrefix(r.URL.Path, "/zones/z1/dns_records"):
		var rec record
		_ = json.NewDecoder(r.Body).Decode(&rec)
		f.writes = append(f.writes, r.Method+" "+rec.Content)
		reply(rec)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": "nope"}}})
	}
}

func TestEnsureA(t *testing.T) {
	cases := []struct {
		name    string
		have    []record
		want    string
		writes  []string
		domain  string
		wantErr bool
	}{
		{"cria", nil, "criado", []string{"POST 1.2.3.4"}, "app.example.com", false},
		{"atualiza", []record{{ID: "r1", Content: "9.9.9.9", Proxied: true}}, "atualizado", []string{"PUT 1.2.3.4"}, "app.example.com", false},
		{"igual", []record{{ID: "r1", Content: "1.2.3.4"}}, "já estava correto", nil, "app.example.com", false},
		{"zona fora da conta", nil, "", nil, "app.outro.org", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAPI{records: tc.have}
			srv := httptest.NewServer(f)
			defer srv.Close()
			c := &Client{Token: "tok", Base: srv.URL}
			got, err := c.EnsureA(context.Background(), tc.domain, "1.2.3.4")
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
			if strings.Join(f.writes, ";") != strings.Join(tc.writes, ";") {
				t.Errorf("writes = %v, want %v", f.writes, tc.writes)
			}
			if f.auth != "Bearer tok" && !tc.wantErr {
				t.Errorf("auth header = %q", f.auth)
			}
		})
	}
}

func TestEnsureARejectsBadIP(t *testing.T) {
	c := &Client{Token: "tok", Base: "http://unused.invalid"}
	for _, ip := range []string{"", "not-an-ip", "::1", "1.2.3.4; rm"} {
		if _, err := c.EnsureA(context.Background(), "a.example.com", ip); err == nil {
			t.Errorf("ip %q aceito", ip)
		}
	}
}

func TestNewWithoutToken(t *testing.T) {
	if New("a@b.com", "") != nil {
		t.Error("sem token deveria devolver nil")
	}
}

func TestAuthHeaderFollowsCredentialFormat(t *testing.T) {
	global := strings.Repeat("a1", 18) + "f" // 37 hex
	cases := []struct {
		name, email, token             string
		wantEmail, wantKey, wantBearer string
	}{
		{"global key com e-mail", "a@b.com", global, "a@b.com", global, ""},
		{"api token ignora o e-mail", "a@b.com", strings.Repeat("T", 40), "", "", "Bearer " + strings.Repeat("T", 40)},
		{"api token sem e-mail", "", strings.Repeat("T", 40), "", "", "Bearer " + strings.Repeat("T", 40)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
			}))
			defer srv.Close()
			c := &Client{Email: tc.email, Token: tc.token, Base: srv.URL}
			if err := c.Check(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got.Get("X-Auth-Email") != tc.wantEmail || got.Get("X-Auth-Key") != tc.wantKey || got.Get("Authorization") != tc.wantBearer {
				t.Errorf("headers = %v", got)
			}
		})
	}
}

func TestGlobalKeyWithoutEmailIsRefusedBeforeTheNetwork(t *testing.T) {
	c := &Client{Token: strings.Repeat("a", 37), Base: "http://unused.invalid"}
	if err := c.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "e-mail") {
		t.Errorf("err = %v", err)
	}
}

func TestCheckExplainsCloudflareErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Invalid access token"}]}`))
	}))
	defer srv.Close()
	err := (&Client{Token: "tok", Base: srv.URL}).Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Invalid access token") || !strings.Contains(err.Error(), "revogado") {
		t.Errorf("err = %v", err)
	}
}

func TestApiErrorChain(t *testing.T) {
	var env envelope
	_ = json.Unmarshal([]byte(`{"errors":[{"code":6003,"message":"Invalid request headers","error_chain":[{"code":6103,"message":"Invalid format for X-Auth-Key header"}]}]}`), &env)
	if got := apiError(400, env); got != "Invalid request headers (Invalid format for X-Auth-Key header)" {
		t.Errorf("got %q", got)
	}
}
