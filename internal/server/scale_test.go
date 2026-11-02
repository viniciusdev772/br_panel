package server

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAppFromFormScaleFields(t *testing.T) {
	base := url.Values{"repo": {"https://github.com/o/r.git"}, "branch": {"main"}}
	with := func(extra url.Values) url.Values {
		out := url.Values{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		extra  url.Values
		ok     bool
		repl   int
		mem    int
		cpu    int
		rate   int
		errSub string
	}{
		{"campos vazios = uma instância sem limites", url.Values{"replicas": {""}, "memory_mb": {""}, "cpu_percent": {""}, "rate_limit": {""}}, true, 1, 0, 0, 0, ""},
		{"formulário antigo sem os campos", nil, true, 1, 0, 0, 0, ""},
		{"tudo preenchido", url.Values{"replicas": {"3"}, "memory_mb": {"512"}, "cpu_percent": {"200"}, "rate_limit": {"20"}}, true, 3, 512, 200, 20, ""},
		{"instâncias demais", url.Values{"replicas": {"17"}}, false, 0, 0, 0, 0, "instâncias"},
		{"instâncias negativas", url.Values{"replicas": {"-2"}}, false, 0, 0, 0, 0, "instâncias"},
		{"memória pequena demais", url.Values{"memory_mb": {"10"}}, false, 0, 0, 0, 0, "memória"},
		{"cpu pequena demais", url.Values{"cpu_percent": {"2"}}, false, 0, 0, 0, 0, "CPU"},
		{"limite de requisições negativo", url.Values{"rate_limit": {"-1"}}, false, 0, 0, 0, 0, "requisições"},
	} {
		req := httptest.NewRequest("POST", "/apps", strings.NewReader(with(tc.extra).Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		a, err := appFromForm(req, "web")
		if tc.ok != (err == nil) {
			t.Errorf("%s: err = %v", tc.name, err)
			continue
		}
		if !tc.ok {
			if !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("%s: mensagem %q deveria citar %q", tc.name, err, tc.errSub)
			}
			continue
		}
		if a.Replicas != tc.repl || a.MemoryMB != tc.mem || a.CPUPercent != tc.cpu || a.RateLimit != tc.rate {
			t.Errorf("%s: %+v", tc.name, a)
		}
	}
}
