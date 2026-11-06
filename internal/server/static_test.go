package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestStaticAssetsAreCachedAndCompressed(t *testing.T) {
	s, h := newTestServer(t)
	setup(t, s, h, nil)
	login := do(h, "POST", "/login", url.Values{"username": {"admin"}, "password": {testPassword}})
	c := sessionCookie(login)
	if c == nil {
		t.Fatal("login não abriu sessão")
	}

	// The pages point at hashed addresses, so the files can be cached for a year.
	page := do(h, "GET", "/settings", nil, c)
	m := regexp.MustCompile(`/static/app\.css\?v=([0-9a-f]{12})`).FindStringSubmatch(page.Body.String())
	if m == nil || !strings.Contains(page.Body.String(), `app.js?v=`) || !strings.Contains(page.Body.String(), `theme.js?v=`) {
		t.Fatalf("a página deveria apontar para arquivos versionados:\n%.400s", page.Body.String())
	}
	// Pages themselves stay uncacheable: they carry the CSRF token.
	if cc := page.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("páginas continuam no-store, veio %q", cc)
	}

	get := func(target string, hdr map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", target, nil)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	zipped := get(m[0], map[string]string{"Accept-Encoding": "gzip"})
	if zipped.Code != 200 || zipped.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		zipped.Header().Get("Content-Encoding") != "gzip" || !strings.HasPrefix(zipped.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("css versionado: %d %v", zipped.Code, zipped.Header())
	}
	zlen := zipped.Body.Len()
	zr, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(zr)

	plain := get("/static/app.css", nil)
	if plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != string(raw) {
		t.Error("sem Accept-Encoding o arquivo vai sem compressão e com o mesmo conteúdo")
	}
	if plain.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("endereço sem versão deve revalidar, veio %q", plain.Header().Get("Cache-Control"))
	}
	if zlen*2 > len(raw) {
		t.Errorf("comprimido (%d bytes) deveria ser bem menor que o original (%d)", zlen, len(raw))
	}
	if nm := get(m[0], map[string]string{"If-None-Match": `"` + m[1] + `"`}); nm.Code != http.StatusNotModified || nm.Body.Len() != 0 {
		t.Errorf("ETag igual deveria dar 304: %d", nm.Code)
	}
	if get("/static/nao-existe.css", nil).Code != 404 {
		t.Error("arquivo que não está embutido deve dar 404")
	}
}
