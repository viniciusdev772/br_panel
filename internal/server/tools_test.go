package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestNodeInstallRoutesRequireSessionAndCSRF(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if w := do(h, "POST", "/tools/node/install", url.Values{}); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("sem sessão: %d", w.Code)
	}
	if w := do(h, "POST", "/tools/node/install", url.Values{}, c); w.Code != http.StatusForbidden {
		t.Errorf("sem CSRF: %d", w.Code)
	}
	if w := do(h, "GET", "/tools/node/status", nil); w.Code != http.StatusSeeOther {
		t.Errorf("status sem sessão: %d", w.Code)
	}
	if s.node.Dir == "" || s.dep.Node != s.node {
		t.Error("o instalador deve estar ligado ao servidor e ao deploy")
	}

	w := do(h, "GET", "/tools/node/status", nil, c)
	var st struct {
		Running   bool   `json:"running"`
		Error     string `json:"error"`
		Log       string `json:"log"`
		Available *bool  `json:"available"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &st) != nil || st.Running || st.Available == nil {
		t.Errorf("status: %d %s", w.Code, w.Body.String())
	}
}
