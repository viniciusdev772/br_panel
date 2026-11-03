package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/cli"
	"github.com/viniciusdev772/br_panel/internal/store"
)

func cacheFile(t *testing.T, s *Server, rel string, size int, age time.Duration) string {
	t.Helper()
	p := filepath.Join(s.dep.Cache.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCacheRoutesNeedSessionAndCSRF(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	old := cacheFile(t, s, "npm/velho", 100, 90*24*time.Hour)
	for _, path := range []string{"/cache/prune", "/cache/clear"} {
		if w := do(h, "POST", path, url.Values{}); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Errorf("%s sem sessão: %d %s", path, w.Code, w.Header().Get("Location"))
		}
		if w := do(h, "POST", path, url.Values{}, c); w.Code != http.StatusForbidden {
			t.Errorf("%s sem CSRF: %d", path, w.Code)
		}
	}
	if w := do(h, "GET", "/cache/clear", nil, c); w.Code == http.StatusOK || w.Code == http.StatusSeeOther {
		t.Errorf("GET não pode executar a limpeza: %d", w.Code)
	}
	if _, err := os.Stat(old); err != nil {
		t.Error("nenhuma dessas chamadas pode ter apagado o cache")
	}
	if w := do(h, "GET", "/api/cache", nil); w.Code != http.StatusSeeOther {
		t.Errorf("/api/cache sem sessão: %d", w.Code)
	}
}

func TestCachePruneClearAndScreens(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	old := cacheFile(t, s, "npm/velho", 4096, 90*24*time.Hour) // before the first read: sizes are remembered for a while
	fresh := cacheFile(t, s, "gomod/novo", 2048, time.Hour)
	csrf := csrfOf(t, h, "/settings", c)

	var info cacheInfo
	if err := json.Unmarshal(do(h, "GET", "/api/cache", nil, c).Body.Bytes(), &info); err != nil || info.Total != 6144 || len(info.Items) == 0 {
		t.Fatalf("/api/cache: %+v %v", info, err)
	}
	if body := do(h, "GET", "/settings", nil, c).Body.String(); !strings.Contains(body, "Cache de builds") || !strings.Contains(body, "6.0 KB") && !strings.Contains(body, "KB") {
		t.Errorf("a tela de configurações deveria mostrar o cache: %.200s", body)
	}
	var ov struct {
		Cache int64 `json:"cache"`
	}
	if err := json.Unmarshal(do(h, "GET", "/api/overview", nil, c).Body.Bytes(), &ov); err != nil || ov.Cache != 6144 {
		t.Errorf("overview.cache = %d (%v)", ov.Cache, err)
	}

	// invalid number of days changes nothing
	for _, d := range []string{"abc", "-1", "99999"} {
		w := do(h, "POST", "/cache/prune", url.Values{"csrf": {csrf}, "days": {d}}, c)
		if !strings.Contains(w.Header().Get("Location"), "Falha") {
			t.Errorf("dias=%q deveria falhar: %s", d, w.Header().Get("Location"))
		}
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("dias inválidos não podem apagar nada")
	}

	w := do(h, "POST", "/cache/prune", url.Values{"csrf": {csrf}, "days": {"30"}}, c)
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "Cache+limpo") {
		t.Fatalf("prune: %d %s", w.Code, w.Header().Get("Location"))
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("o item antigo deveria sumir")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("o item recente deveria ficar")
	}

	w = do(h, "POST", "/cache/clear", url.Values{"csrf": {csrf}}, c)
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "esvaziado") {
		t.Fatalf("clear: %d %s", w.Code, w.Header().Get("Location"))
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Error("o cache deveria ter sido esvaziado")
	}
}

func TestCacheCleaningWaitsForRunningDeploys(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("precisa do git")
	}
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	csrf := csrfOf(t, h, "/settings", c)
	keep := cacheFile(t, s, "npm/arquivo", 10, 90*24*time.Hour)

	// a local repository and a slow build keep a deploy running long enough to observe it
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	app := store.App{Name: "web", Repo: repo, Branch: "main", BuildCmd: "sleep 4", StartCmd: "x", Port: 3001, Replicas: 1}
	if err := s.store.Create(app); err != nil {
		t.Fatal(err)
	}
	if !s.dep.Start("web", "teste") {
		t.Fatal("Start")
	}
	t.Cleanup(func() { waitIdle(t, s, "web") })
	deadline := time.Now().Add(10 * time.Second)
	for !s.dep.Busy() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !s.dep.Busy() {
		t.Fatal("o deploy deveria estar rodando")
	}
	for _, path := range []string{"/cache/prune", "/cache/clear"} {
		w := do(h, "POST", path, url.Values{"csrf": {csrf}, "days": {"0"}}, c)
		if loc := w.Header().Get("Location"); !strings.Contains(loc, "deploy+em+andamento") {
			t.Errorf("%s com deploy rodando deveria recusar: %s", path, loc)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("nada pode ser apagado enquanto um deploy roda")
	}
}

func TestDeployCleanActionNeedsCSRFAndRebuilds(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if err := s.store.Create(store.App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3001, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	src := s.dep.Paths.SrcDir("web")
	if err := os.MkdirAll(filepath.Join(src, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	csrf := csrfOf(t, h, "/apps/web", c)
	if w := do(h, "POST", "/apps/web/deploy-clean", url.Values{}, c); w.Code != http.StatusForbidden {
		t.Errorf("sem CSRF: %d", w.Code)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("sem CSRF nada pode ser apagado")
	}
	w := do(h, "POST", "/apps/web/deploy-clean", url.Values{"csrf": {csrf}}, c)
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "do+zero") {
		t.Fatalf("deploy-clean: %d %s", w.Code, w.Header().Get("Location"))
	}
	waitIdle(t, s, "web") // the deploy fails offline, after the clean-up
	if _, err := os.Stat(filepath.Join(src, "node_modules")); !os.IsNotExist(err) {
		t.Error("o node_modules antigo deveria ter sido apagado pelo deploy do zero")
	}
	if body := do(h, "GET", "/apps/web", nil, c).Body.String(); !strings.Contains(body, "deploy-clean") || !strings.Contains(body, "Refazer do zero") {
		t.Error("a página do app deveria ter o botão Refazer do zero")
	}
}

// The CLI uses the same routes as the screen: status, prune, clear and deploy --clean.
func TestCLICacheAndCleanDeploy(t *testing.T) {
	s, h := newTestServer(t)
	setup(t, s, h, nil)
	cacheFile(t, s, "npm/velho", 4096, 90*24*time.Hour)
	cacheFile(t, s, "cargo/novo", 1024, time.Hour)
	if err := s.store.Create(store.App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3001, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) { return runCLI(t, s, "", args...) }

	code, out, errs := run("cache", "status")
	if code != 0 || !strings.Contains(out, "npm") || !strings.Contains(out, "5.0 KB") {
		t.Fatalf("cache status: %d %q %q", code, out, errs)
	}
	var info cli.CacheInfo
	if _, j, _ := run("cache", "status", "--json"); json.Unmarshal([]byte(j), &info) != nil || info.Total != 5120 {
		t.Errorf("cache status --json: %+v", info)
	}
	if _, out, _ := run("status"); !strings.Contains(out, "Cache de builds: 5.0 KB") {
		t.Errorf("status deveria mostrar o cache: %q", out)
	}

	if code, _, errs := run("cache", "prune", "--days", "abc"); code == 0 {
		t.Errorf("dias inválidos deveriam falhar: %q", errs)
	}
	if code, out, errs := run("cache", "prune", "--days", "30"); code != 0 || !strings.Contains(out, "Cache limpo") {
		t.Fatalf("cache prune: %d %q %q", code, out, errs)
	}
	if code, out, _ := run("cache", "clear"); code == 0 || strings.Contains(out, "esvaziado") {
		t.Error("cache clear sem --yes não pode apagar")
	}
	if info := s.cacheInfo(); info.Total != 1024 {
		t.Fatalf("o prune deveria ter deixado só o item recente: %d", info.Total)
	}
	if code, out, errs := run("cache", "clear", "--yes"); code != 0 || !strings.Contains(out, "esvaziado") {
		t.Fatalf("cache clear: %d %q %q", code, out, errs)
	}
	if s.cacheInfo().Total != 0 {
		t.Error("o cache deveria estar vazio")
	}

	src := s.dep.Paths.SrcDir("web")
	if err := os.MkdirAll(filepath.Join(src, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := run("apps", "deploy", "web", "--clean"); code != 0 || !strings.Contains(out, "do zero") {
		t.Fatalf("apps deploy --clean: %d %q %q", code, out, errs)
	}
	waitIdle(t, s, "web")
	if _, err := os.Stat(filepath.Join(src, "node_modules")); !os.IsNotExist(err) {
		t.Error("--clean deveria apagar as dependências instaladas")
	}
}
