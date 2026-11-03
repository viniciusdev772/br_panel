package deploy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
)

// gitRepo makes a local repository with the given files and symbolic links, committed on main.
func gitRepo(t *testing.T, files map[string]string, links map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	for name, body := range files {
		p := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range links {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(repo, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "x"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return repo
}

func waitDeployed(t *testing.T, d *Deployer, name string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if a, ok := d.Store.Get(name); ok && !a.Deploying() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("o deploy de %s não terminou", name)
}

// An app of a monorepo builds and runs from its own folder; a folder that is missing, or that is a
// link out of the checkout, fails with a clear message instead of deploying something else.
func TestMonorepoAppBuildsAndRunsInItsFolder(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("precisa do git")
	}
	d, st := mountDeployer(t)
	d.gitProtocols = "file:https:ssh" // the repository is a local folder
	if err := st.Delete("web"); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	repo := gitRepo(t, map[string]string{
		"apps/api/brpanel.json": `{"build":"echo ok > built.txt","start":"echo rodando","port":3555}`,
		"apps/web/index.html":   "<h1>oi</h1>",
		"README.md":             "x",
	}, map[string]string{"apps/atalho": outside})

	deploy := func(name, root string) string {
		t.Helper()
		if err := st.Create(store.App{Name: name, Repo: repo, Branch: "main", RootDir: root, Replicas: 1}); err != nil {
			t.Fatal(err)
		}
		if !d.Start(name, "teste") {
			t.Fatal("Start")
		}
		waitDeployed(t, d, name)
		b, _ := os.ReadFile(d.Paths.LogFile(name))
		return string(b)
	}
	if err := os.MkdirAll(filepath.Join(d.DataDir, "logs"), 0o750); err != nil {
		t.Fatal(err)
	}

	log := deploy("api", "apps/api")
	src := d.Paths.SrcDir("api")
	proj := filepath.Join(src, "apps", "api")
	if _, err := os.Stat(filepath.Join(proj, "built.txt")); err != nil {
		t.Errorf("o build deveria ter rodado na pasta da aplicação: %v\n%s", err, log)
	}
	if _, err := os.Stat(filepath.Join(src, "built.txt")); err == nil {
		t.Error("o build não pode rodar na raiz do repositório")
	}
	unit, _ := os.ReadFile(filepath.Join(d.Settings().UnitDir, "brpanel-api.service"))
	if !strings.Contains(string(unit), "WorkingDirectory="+proj+"\n") {
		t.Errorf("o serviço deveria rodar na pasta da aplicação (%s):\n%s", proj, unit)
	}
	if !strings.Contains(log, "monorepo: a aplicação vive em apps/api") || !strings.Contains(log, "detectado") {
		t.Errorf("log: %s", log)
	}
	if a, _ := st.Get("api"); a.BuildCmd != "echo ok > built.txt" || a.StartCmd != "echo rodando" || a.Kind != "brpanel.json" {
		t.Errorf("a detecção deveria ter lido o brpanel.json da pasta: %+v", a)
	}

	for _, c := range []struct{ name, root, want string }{
		{"falta", "apps/nao-existe", "não existe no repositório"},
		{"atalho", "apps/atalho", "atalho"},
	} {
		log := deploy(c.name, c.root)
		if !strings.Contains(log, c.want) || !strings.Contains(log, "FALHOU") {
			t.Errorf("%s: o deploy deveria falhar com %q:\n%s", c.name, c.want, log)
		}
		if a, _ := st.Get(c.name); a.Status != store.StatusFailed {
			t.Errorf("%s: status %s", c.name, a.Status)
		}
	}
}

func TestDetectRepoFindsTheAppsOfAMonorepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("precisa do git")
	}
	d, _ := mountDeployer(t)
	d.gitProtocols = "file:https:ssh"
	if err := os.MkdirAll(filepath.Join(d.DataDir, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	repo := gitRepo(t, map[string]string{
		"pnpm-lock.yaml":           "x",
		"pnpm-workspace.yaml":      "packages:\n  - 'apps/*'\n  - 'packages/*'\n",
		"apps/web/package.json":    `{"name":"@acme/web","scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"14"}}`,
		"apps/api/package.json":    `{"name":"api","scripts":{"start":"node index.js"}}`,
		"packages/ui/package.json": `{"name":"@acme/ui"}`,
	}, map[string]string{"apps/atalho": outside})

	// whole repository: the apps are listed, in the order deployable first
	res, err := d.DetectRepo(context.Background(), repo, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range res.Apps {
		got = append(got, a.Path)
	}
	if strings.Join(got, ",") != "apps/api,apps/web,packages/ui" {
		t.Errorf("apps: %v", got)
	}
	// one app: the commands install at the workspace root and build only that package
	res, err = d.DetectRepo(context.Background(), repo, "main", "apps/web")
	if err != nil || res.Det == nil {
		t.Fatalf("apps/web: %v %+v", err, res)
	}
	if res.Det.Build != "cd ../.. && pnpm install --frozen-lockfile && pnpm --filter '@acme/web...' run build" {
		t.Errorf("build: %q", res.Det.Build)
	}
	if _, err := d.DetectRepo(context.Background(), repo, "main", "apps/nao-existe"); err == nil || !strings.Contains(err.Error(), "não existe") {
		t.Errorf("pasta inexistente: %v", err)
	}
	if _, err := d.DetectRepo(context.Background(), repo, "main", "apps/atalho"); err == nil || !strings.Contains(err.Error(), "atalho") {
		t.Errorf("pasta que é um link: %v", err)
	}
}

func TestSuggestNameIn(t *testing.T) {
	for _, c := range []struct{ repo, root, want string }{
		{"https://github.com/o/mono.git", "", "mono"},
		{"https://github.com/o/mono.git", "apps/web", "mono-web"},
		{"https://github.com/o/mono.git", "services/Api_V2", "mono-api-v2"},
		{"https://github.com/o/a-very-long-repository-name-for-tests.git", "apps/frontend-site", "a-very-long-repository-name-for"},
	} {
		if got := SuggestNameIn(c.repo, c.root); got != c.want {
			t.Errorf("SuggestNameIn(%q, %q) = %q, quero %q", c.repo, c.root, got, c.want)
		}
	}
}
