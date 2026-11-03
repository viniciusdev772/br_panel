package deploy

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
)

func TestSplitInstall(t *testing.T) {
	for _, c := range []struct {
		build, cmd, rest string
	}{
		{"npm ci", "npm ci", ""},
		{"npm ci && npm run build", "npm ci", "npm run build"},
		{"  pnpm install --frozen-lockfile && pnpm run build  ", "pnpm install --frozen-lockfile", "pnpm run build"},
		{"yarn install --frozen-lockfile && yarn build && echo ok", "yarn install --frozen-lockfile", "yarn build && echo ok"},
		{"composer install --no-dev --optimize-autoloader", "composer install --no-dev --optimize-autoloader", ""},
		{"bun install && bun run build", "bun install", "bun run build"},
		// not skippable: another command, extra flags, other separators, injection attempts
		{"npm run build", "", "npm run build"},
		{"npm ci --ignore-scripts && npm run build", "", "npm ci --ignore-scripts && npm run build"},
		{"npm ci; rm -rf /", "", "npm ci; rm -rf /"},
		{"npm ci || true", "", "npm ci || true"},
		{"npm cit && x", "", "npm cit && x"},
		{"echo hi && npm ci", "", "echo hi && npm ci"},
		{"", "", ""},
		// a monorepo app: the install runs at the workspace root and the "cd" stays on what is left
		{"cd ../.. && pnpm install --frozen-lockfile && pnpm --filter 'a...' run build", "pnpm install --frozen-lockfile", "cd ../.. && pnpm --filter 'a...' run build"},
		{"cd ../.. && npm ci", "npm ci", ""},
		{"cd ../../.. && yarn install --immutable && yarn workspace x run build", "yarn install --immutable", "cd ../../.. && yarn workspace x run build"},
		{"cd ../.. && echo x && npm ci", "", "cd ../.. && echo x && npm ci"},
		{"cd apps/web && npm ci", "", "cd apps/web && npm ci"},
		{"cd /etc && npm ci", "", "cd /etc && npm ci"},
	} {
		s, _, rest := splitInstall(c.build)
		got := ""
		if s != nil {
			got = s.cmd
		}
		if got != c.cmd || rest != c.rest {
			t.Errorf("splitInstall(%q) = (%q, %q), quero (%q, %q)", c.build, got, rest, c.cmd, c.rest)
		}
	}
}

func TestPlanDepsSkipsOnlyWhenNothingChanged(t *testing.T) {
	const build = "npm ci && npm run build"
	src := project(t, map[string]string{"package.json": `{"name":"x"}`, "package-lock.json": `{"v":1}`, "node_modules/a/index.js": "x"})
	first := planDeps(build, src, src, "node=24", "")
	if first.skip || first.step == nil || first.hash == "" || first.rest != "npm run build" {
		t.Fatalf("o primeiro build precisa instalar: %+v", first)
	}
	same := planDeps(build, src, src, "node=24", first.hash)
	if !same.skip {
		t.Fatalf("lockfile igual e node_modules presente deveria pular: %+v", same)
	}

	// each of these must force the install again
	if planDeps(build, src, src, "node=22", first.hash).skip {
		t.Error("outra versão do Node.js não pode pular")
	}
	if planDeps("npm install && npm run build", src, src, "node=24", first.hash).skip {
		t.Error("outro comando de instalação não pode pular")
	}
	if err := os.WriteFile(filepath.Join(src, "package-lock.json"), []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if planDeps(build, src, src, "node=24", first.hash).skip {
		t.Error("lockfile alterado não pode pular")
	}
	if err := os.WriteFile(filepath.Join(src, "package-lock.json"), []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "package.json"), []byte(`{"name":"y"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if planDeps(build, src, src, "node=24", first.hash).skip {
		t.Error("package.json alterado não pode pular")
	}
	if err := os.WriteFile(filepath.Join(src, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(src, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if planDeps(build, src, src, "node=24", first.hash).skip {
		t.Error("sem node_modules não pode pular")
	}
}

func TestPlanDepsNeedsALockfile(t *testing.T) {
	src := project(t, map[string]string{"package.json": `{}`, "node_modules/a": "x"})
	p := planDeps("npm install && npm run build", src, src, "", "qualquer")
	if p.skip || p.hash != "" {
		t.Errorf("sem lockfile não há prova de que nada mudou: %+v", p)
	}
}

func TestPlanDepsIgnoresUnknownBuilds(t *testing.T) {
	src := project(t, map[string]string{"package.json": `{}`, "package-lock.json": `{}`, "node_modules/a": "x"})
	p := planDeps("make all", src, src, "", "x")
	if p.step != nil || p.skip || p.rest != "make all" {
		t.Errorf("%+v", p)
	}
}

func TestDepsExtra(t *testing.T) {
	a := depsExtra("24", []string{"PATH=/bin", "NODE_ENV=production", "npm_config_cache=/c", "SECRET=1"})
	b := depsExtra("24", []string{"PATH=/bin", "NODE_ENV=development", "npm_config_cache=/c", "SECRET=2"})
	if a == b {
		t.Error("NODE_ENV muda o que o npm instala e precisa entrar na impressão digital")
	}
	if strings.Contains(a, "SECRET") || strings.Contains(a, "PATH") {
		t.Errorf("variáveis irrelevantes (e possíveis segredos) não entram: %q", a)
	}
}

func TestBuildEnvUsesTheSharedCacheOnlyWhenAsked(t *testing.T) {
	d, _ := mountDeployer(t)
	a := store.App{Name: "web", Port: 3001}
	with, err := d.buildEnv(a, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(with, "\n"), "npm_config_cache="+filepath.Join(d.DataDir, "cache", "npm")) {
		t.Errorf("faltou o cache compartilhado: %v", with)
	}
	without, _ := d.buildEnv(a, false)
	if strings.Contains(strings.Join(without, "\n"), "npm_config_cache") {
		t.Error("sem cache compartilhado, as ferramentas ficam com o próprio cache")
	}
	// the app's own variables come last, so they can override the cache location
	a.Env = "npm_config_cache=/meu"
	over, _ := d.buildEnv(a, true)
	if over[len(over)-1] != "npm_config_cache=/meu" {
		t.Errorf("a variável do app deveria vir por último: %v", over)
	}
}

func TestCleanAppRemovesCodeDependenciesAndPrivateCaches(t *testing.T) {
	d, _ := mountDeployer(t)
	app := d.Paths.AppDir("web")
	for _, f := range []string{"src/node_modules/a/i.js", "src/index.js", ".npm/_cacache/x", ".cache/pip/y", "go/pkg/z"} {
		p := filepath.Join(app, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vol := d.Paths.VolumeDir("web", "uploads") // data of the app must survive
	if err := os.MkdirAll(vol, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vol, "foto.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.depsFile("web"), []byte("abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	d.cleanApp("web", &log)
	for _, f := range []string{"src", ".npm", ".cache", "go", "deps.sha256"} {
		if _, err := os.Stat(filepath.Join(app, f)); !os.IsNotExist(err) {
			t.Errorf("%s deveria ter sido apagado (%v)", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(vol, "foto.png")); err != nil {
		t.Errorf("o volume do app não pode ser apagado: %v", err)
	}
	if !strings.Contains(log.String(), "do zero") {
		t.Errorf("o log deveria avisar: %q", log.String())
	}
}

func TestBusyFollowsRunningDeploys(t *testing.T) {
	d, _ := mountDeployer(t)
	if d.Busy() {
		t.Fatal("sem deploy rodando")
	}
	r := d.beginRun("web", func() {})
	if !d.Busy() {
		t.Error("deploy em andamento")
	}
	d.endRun("web", r)
	if d.Busy() {
		t.Error("depois do fim")
	}
}

func TestPlanDepsForAMonorepoApp(t *testing.T) {
	repo := project(t, map[string]string{
		"pnpm-lock.yaml": "x", "package.json": "{}", "node_modules/.bin/x": "x",
		"apps/web/package.json": "{}",
	})
	proj := filepath.Join(repo, "apps", "web")
	const build = "cd ../.. && pnpm install --frozen-lockfile && pnpm --filter 'web...' run build"
	first := planDeps(build, proj, repo, "node=24", "")
	if first.skip || first.step == nil || first.hash == "" || first.rest != "cd ../.. && pnpm --filter 'web...' run build" {
		t.Fatalf("primeiro build: %+v", first)
	}
	again := planDeps(build, proj, repo, "node=24", first.hash)
	if !again.skip || again.rest != first.rest {
		t.Fatalf("mesmo lockfile na raiz do workspace deveria pular: %+v", again)
	}
	if err := os.WriteFile(filepath.Join(repo, "pnpm-lock.yaml"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if planDeps(build, proj, repo, "node=24", first.hash).skip {
		t.Error("lockfile da raiz alterado não pode pular")
	}
	// a cd that leaves the checkout is never fingerprinted
	esc := planDeps("cd ../../../.. && pnpm install --frozen-lockfile", proj, repo, "", "x")
	if esc.step != nil || esc.skip || esc.rest != "cd ../../../.. && pnpm install --frozen-lockfile" {
		t.Errorf("cd para fora do repositório: %+v", esc)
	}
}

func TestMonorepoSiblingsDeployOneAtATime(t *testing.T) {
	d := &Deployer{}
	web := store.App{Name: "web", Repo: "https://github.com/o/mono", RootDir: "apps/web"}
	api := store.App{Name: "api", Repo: "https://github.com/o/mono", RootDir: "apps/api"}
	other := store.App{Name: "other", Repo: "https://github.com/o/outro", RootDir: "x"}
	whole := store.App{Name: "whole", Repo: "https://github.com/o/mono"} // no folder: never held up
	ctx := context.Background()

	var log bytes.Buffer
	rel1, ok := d.waitSiblings(ctx, web, &log)
	if !ok {
		t.Fatal("o primeiro entra")
	}
	if rel, ok := d.waitSiblings(ctx, whole, &log); !ok {
		t.Fatal("app sem pasta não espera")
	} else {
		rel()
	}
	if rel, ok := d.waitSiblings(ctx, other, &log); !ok {
		t.Fatal("outro repositório não espera")
	} else {
		rel()
	}

	got := make(chan bool, 1)
	go func() {
		rel2, ok := d.waitSiblings(ctx, api, &log)
		if ok {
			rel2()
		}
		got <- ok
	}()
	select {
	case <-got:
		t.Fatal("o irmão do mesmo monorepo deveria esperar")
	case <-time.After(100 * time.Millisecond):
	}
	rel1()
	select {
	case ok := <-got:
		if !ok {
			t.Error("deveria entrar depois que o primeiro terminou")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("o irmão não entrou")
	}

	// cancelled while waiting: gives up without taking the slot
	hold, _ := d.waitSiblings(ctx, web, &log)
	cctx, cancel := context.WithCancel(ctx)
	res := make(chan bool, 1)
	go func() { _, ok := d.waitSiblings(cctx, api, &log); res <- ok }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if ok := <-res; ok {
		t.Error("cancelado na fila não pode entrar")
	}
	hold()
	if rel, ok := d.waitSiblings(ctx, api, &log); !ok {
		t.Error("a vaga deveria estar livre")
	} else {
		rel()
	}
	if !strings.Contains(log.String(), "aguardando outro deploy do mesmo repositório") {
		t.Errorf("o log deveria explicar a espera: %q", log.String())
	}
}
