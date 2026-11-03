package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

func TestWebhookDeploysOnlyTheAffectedMonorepoApps(t *testing.T) {
	s, h := newTestServer(t)
	const secret = "segredo-do-webhook-123456"
	setup(t, s, h, url.Values{"webhook_secret": {secret}})
	repo := "https://github.com/octo/mono"
	for _, a := range []store.App{
		{Name: "web", RootDir: "apps/web", WatchPaths: "packages/ui/**"},
		{Name: "api", RootDir: "apps/api"},
		{Name: "tudo"}, // no folder: the whole repository, every push counts
	} {
		a.Repo, a.Branch, a.StartCmd, a.AutoDeploy = repo, "main", "x", true
		if err := s.store.Create(a); err != nil {
			t.Fatal(err)
		}
	}
	push := func(commits string) string {
		body := `{"ref":"refs/heads/main","after":"abcdef1234567","repository":{"full_name":"octo/mono"},"commits":` + commits + `}`
		r := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
		r.RemoteAddr = "140.82.112.1:1"
		r.Header.Set("X-GitHub-Event", "push")
		r.Header.Set("X-Hub-Signature-256", sign(secret, body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		for _, n := range []string{"web", "api", "tudo"} {
			waitIdle(t, s, n)
		}
		return w.Body.String()
	}
	commit := func(modified ...string) string {
		q := make([]string, len(modified))
		for i, m := range modified {
			q[i] = fmt.Sprintf("%q", m)
		}
		return `{"added":[],"modified":[` + strings.Join(q, ",") + `],"removed":[]}`
	}
	deployed := func(names ...string) (got []string) {
		for _, n := range names {
			if a, _ := s.store.Get(n); !a.LastDeploy.IsZero() {
				got = append(got, n)
			}
		}
		return
	}

	// 1: only apps/web changed -> web and the whole-repo app, not api
	if out := push("[" + commit("apps/web/src/a.ts") + "]"); !strings.HasPrefix(out, "2 deploy(s) disparado(s), 1 ignorado(s)") {
		t.Errorf("push em apps/web: %q", out)
	}
	if got := deployed("web", "api", "tudo"); len(got) != 2 || got[0] != "web" || got[1] != "tudo" {
		t.Errorf("deveriam ter rodado web e tudo: %v", got)
	}
	// 2: a watched sibling package -> web again (api still untouched)
	if out := push("[" + commit("packages/ui/button.tsx") + "]"); !strings.HasPrefix(out, "2 deploy(s)") {
		t.Errorf("push em packages/ui: %q", out)
	}
	// 3: a shared root file -> every app
	if out := push("[" + commit("pnpm-lock.yaml") + "]"); !strings.HasPrefix(out, "3 deploy(s) disparado(s)") {
		t.Errorf("lockfile: %q", out)
	}
	// 4: a push that proves nothing (no commits) -> every app, never a silent skip
	if out := push("[]"); !strings.HasPrefix(out, "3 deploy(s) disparado(s)") {
		t.Errorf("sem commits: %q", out)
	}
	// 4b: forced pushes and new branches never trust the commit list
	for _, flag := range []string{"forced", "created"} {
		body := `{"ref":"refs/heads/main","after":"abcdef1234567","` + flag + `":true,"repository":{"full_name":"octo/mono"},"commits":[` + commit("README.md") + `]}`
		r := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
		r.RemoteAddr = "140.82.112.1:1"
		r.Header.Set("X-GitHub-Event", "push")
		r.Header.Set("X-Hub-Signature-256", sign(secret, body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		for _, n := range []string{"web", "api", "tudo"} {
			waitIdle(t, s, n)
		}
		if out := w.Body.String(); !strings.HasPrefix(out, "3 deploy(s) disparado(s)") {
			t.Errorf("push %s: %q", flag, out)
		}
	}
	// 5: a README in the root affects nobody except the whole-repo app
	if out := push("[" + commit("README.md") + "]"); !strings.HasPrefix(out, "1 deploy(s) disparado(s), 2 ignorado(s)") {
		t.Errorf("README: %q", out)
	}
}

// The form and the CLI validate the folder like any other field that reaches a shell command.
func TestMonorepoFieldsInTheFormAndCLI(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	csrf := csrfOf(t, h, "/apps/new", c)
	base := func(extra url.Values) url.Values {
		f := url.Values{"csrf": {csrf}, "repo": {"https://github.com/octocat/mono.git"}, "start_cmd": {"./run"}, "build_cmd": {"true"}}
		for k, v := range extra {
			f[k] = v
		}
		return f
	}
	for _, bad := range []string{"../etc", "/etc", "apps/..", "apps/web; id", "apps/$(id)", ".git"} {
		w := do(h, "POST", "/apps", base(url.Values{"name": {"x"}, "root_dir": {bad}}), c)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("root_dir %q: %d", bad, w.Code)
		}
	}
	if w := do(h, "POST", "/apps", base(url.Values{"name": {"x"}, "watch_paths": {"../x"}}), c); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("watch_paths inválido: %d", w.Code)
	}
	if len(s.store.List()) != 0 {
		t.Fatal("nada deveria ter sido criado")
	}

	w := do(h, "POST", "/apps", base(url.Values{"name": {"mono-web"}, "root_dir": {"./apps/web/"}, "watch_paths": {"packages/ui/**\n!**/*.md"}}), c)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("criar: %d %s", w.Code, w.Body.String())
	}
	waitIdle(t, s, "mono-web")
	if a, _ := s.store.Get("mono-web"); a.RootDir != "apps/web" || a.WatchPaths != "packages/ui/**\n!**/*.md" {
		t.Fatalf("o valor deveria ser normalizado: %+v", a)
	}
	if body := do(h, "GET", "/apps/mono-web", nil, c).Body.String(); !strings.Contains(body, "apps/web") {
		t.Error("a página da aplicação deveria mostrar a pasta")
	}
	if body := do(h, "GET", "/apps/mono-web/edit", nil, c).Body.String(); !strings.Contains(body, `value="apps/web"`) || !strings.Contains(body, "packages/ui/**") {
		t.Error("o formulário de edição deveria trazer a pasta e os caminhos extras")
	}

	// the CLI sees and changes the same fields
	if code, out, errs := runCLI(t, s, "", "apps", "show", "mono-web"); code != 0 || !strings.Contains(out, "pasta no repositório:") || !strings.Contains(out, "apps/web") {
		t.Fatalf("apps show: %d %q %q", code, out, errs)
	}
	if code, out, errs := runCLI(t, s, "", "apps", "set", "mono-web", "--root", "apps/site", "--watch", "libs/**,!**/*.md"); code != 0 {
		t.Fatalf("apps set: %d %q %q", code, out, errs)
	}
	waitIdle(t, s, "mono-web")
	if a, _ := s.store.Get("mono-web"); a.RootDir != "apps/site" || a.WatchPaths != "libs/**\n!**/*.md" {
		t.Errorf("apps set: %+v", a)
	}
	if code, _, errs := runCLI(t, s, "", "apps", "set", "mono-web", "--root", "../fora"); code == 0 || !strings.Contains(errs, "ERRO:") {
		t.Errorf("--root inválido deveria falhar: %q", errs)
	}
}
