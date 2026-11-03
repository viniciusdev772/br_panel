package detect

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func paths(cs []Candidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Path)
	}
	return out
}

const webPkg = `{"name":"@acme/web","scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"14"}}`

func TestWorkspaceBuildCommands(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string
		build string
		start string
	}{
		{"pnpm", map[string]string{"pnpm-lock.yaml": "x", "pnpm-workspace.yaml": "packages:\n  - 'apps/*'\n", "apps/web/package.json": webPkg},
			"cd ../.. && pnpm install --frozen-lockfile && pnpm --filter '@acme/web...' run build", "NODE_ENV=production pnpm start"},
		{"npm", map[string]string{"package-lock.json": "x", "package.json": `{"workspaces":["apps/*"]}`, "apps/web/package.json": webPkg},
			"cd ../.. && npm ci && npm run build --workspace='@acme/web' --if-present", "NODE_ENV=production npm start"},
		{"yarn clássico", map[string]string{"yarn.lock": "x", "package.json": `{"workspaces":["apps/*"]}`, "apps/web/package.json": webPkg},
			"cd ../.. && yarn install --frozen-lockfile && yarn workspace '@acme/web' run build", "NODE_ENV=production yarn start"},
		{"yarn berry", map[string]string{"yarn.lock": "x", ".yarnrc.yml": "nodeLinker: node-modules\n", "package.json": `{"workspaces":["apps/*"]}`, "apps/web/package.json": webPkg},
			"cd ../.. && yarn install --immutable && yarn workspaces foreach -Rt --from '@acme/web' run build", "NODE_ENV=production yarn start"},
		{"bun", map[string]string{"bun.lock": "x", "package.json": `{"workspaces":["apps/*"]}`, "apps/web/package.json": webPkg},
			"cd ../.. && bun install && bun run --filter '@acme/web' build", "NODE_ENV=production bun run start"},
		{"turbo + pnpm", map[string]string{"pnpm-lock.yaml": "x", "turbo.json": "{}", "apps/web/package.json": webPkg},
			"cd ../.. && pnpm install --frozen-lockfile && pnpm exec turbo run build --filter='@acme/web...'", "NODE_ENV=production pnpm start"},
		{"turbo + npm", map[string]string{"package-lock.json": "x", "turbo.json": "{}", "apps/web/package.json": webPkg},
			"cd ../.. && npm ci && npx turbo run build --filter='@acme/web...'", "NODE_ENV=production npm start"},
		{"nx", map[string]string{"package-lock.json": "x", "nx.json": "{}", "apps/web/package.json": webPkg},
			"cd ../.. && npm ci && npx nx build '@acme/web'", "NODE_ENV=production npm start"},
		{"pasta mais funda", map[string]string{"pnpm-lock.yaml": "x", "services/api/v2/package.json": `{"name":"api","scripts":{"build":"tsc","start":"node dist/index.js"}}`},
			"cd ../../.. && pnpm install --frozen-lockfile && pnpm --filter 'api...' run build", "NODE_ENV=production pnpm start"},
		{"sem script de build", map[string]string{"pnpm-lock.yaml": "x", "apps/web/package.json": `{"name":"web","scripts":{"start":"node a.js"}}`},
			"cd ../.. && pnpm install --frozen-lockfile", "NODE_ENV=production pnpm start"},
	} {
		repo := project(t, c.files)
		root := "apps/web"
		if c.name == "pasta mais funda" {
			root = "services/api/v2"
		}
		d := DetectApp(repo, root)
		if d == nil || d.Build != c.build || d.Start != c.start {
			t.Errorf("%s:\n  build  %q\n  quero  %q\n  start  %q\n  quero  %q", c.name, buildOf(d), c.build, startOf(d), c.start)
		}
	}
}

func buildOf(d *Detection) string {
	if d == nil {
		return "<nil>"
	}
	return d.Build
}

func startOf(d *Detection) string {
	if d == nil {
		return "<nil>"
	}
	return d.Start
}

func TestWorkspaceBuildRefusesUnsafePackageNames(t *testing.T) {
	for _, name := range []string{"a'; rm -rf / #", "$(id)", "a b", "../x", "`id`", "A"} {
		pkg := `{"name":` + quote(name) + `,"scripts":{"build":"x","start":"y"}}`
		repo := project(t, map[string]string{"pnpm-lock.yaml": "x", "apps/web/package.json": pkg})
		d := DetectApp(repo, "apps/web")
		if d == nil {
			t.Fatalf("%q: sem detecção", name)
		}
		if strings.Contains(d.Build, "filter") || strings.ContainsAny(d.Build, "`$;'") {
			t.Errorf("nome %q chegou ao comando: %q", name, d.Build)
		}
	}
}

func quote(s string) string {
	b := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	return `"` + b + `"`
}

func TestStandaloneProjectInSubfolderKeepsItsOwnCommands(t *testing.T) {
	repo := project(t, map[string]string{
		"frontend/package.json":      `{"name":"front","scripts":{"build":"vite build"},"devDependencies":{"vite":"5"}}`,
		"frontend/package-lock.json": "x",
		"backend/go.mod":             "module x\n",
		"backend/main.go":            "package main\n",
	})
	if d := DetectApp(repo, "frontend"); d == nil || d.Build != "npm ci && npm run build" {
		t.Errorf("frontend: %+v", d)
	}
	if d := DetectApp(repo, "backend"); d == nil || d.Kind != "Go" || d.Build != "go build -o app ." {
		t.Errorf("backend: %+v", d)
	}
}

func TestRustWorkspaceMemberStartsFromTheWorkspaceTarget(t *testing.T) {
	repo := project(t, map[string]string{
		"Cargo.toml":             "[workspace]\nmembers = [\"crates/api\"]\n",
		"crates/api/Cargo.toml":  "[package]\nname = \"api\"\nversion = \"0.1.0\"\n",
		"crates/api/src/main.rs": "fn main(){}",
		"standalone/Cargo.toml":  "[package]\nname = \"solo\"\n",
		"standalone/src/main.rs": "fn main(){}",
	})
	if d := DetectApp(repo, "crates/api"); d == nil || d.Start != "../../target/release/api" || d.Build != "cargo build --release" {
		t.Errorf("membro: %+v", d)
	}
	if d := DetectApp(repo, "standalone"); d == nil || d.Start != "../target/release/solo" {
		// "standalone" sits below a workspace root but is not listed: cargo would refuse it, and the
		// path is still the nearest workspace's target; the note tells the person.
		t.Errorf("fora da lista: %+v", d)
	}
}

func TestWorkspacesDiscovery(t *testing.T) {
	lib := `{"name":"@acme/ui","main":"dist/index.js","types":"dist/index.d.ts"}`
	repo := project(t, map[string]string{
		"package.json":                         `{"name":"root","private":true,"workspaces":["apps/*","packages/*"]}`,
		"apps/web/package.json":                webPkg,
		"apps/api/package.json":                `{"name":"api","scripts":{"start":"node server.js"}}`,
		"apps/docs/package.json":               `{"name":"docs","dependencies":{"vite":"5"}}`,
		"packages/ui/package.json":             lib,
		"packages/ui/readme.md":                "x",
		"apps/web/node_modules/x/package.json": "{}",
	})
	got := Workspaces(repo)
	if !slices.Equal(paths(got), []string{"apps/api", "apps/docs", "apps/web", "packages/ui"}) {
		t.Fatalf("caminhos: %v", paths(got))
	}
	byPath := map[string]Candidate{}
	for _, c := range got {
		byPath[c.Path] = c
	}
	if !byPath["apps/web"].Deployable || byPath["apps/web"].Name != "@acme/web" || byPath["apps/web"].Kind != "Node.js (npm)" {
		t.Errorf("apps/web: %+v", byPath["apps/web"])
	}
	if byPath["packages/ui"].Deployable {
		t.Errorf("uma biblioteca não é uma aplicação: %+v", byPath["packages/ui"])
	}
	if !IsMonorepo(repo) {
		t.Error("IsMonorepo")
	}
}

func TestWorkspacesPnpmYamlWithExclusions(t *testing.T) {
	repo := project(t, map[string]string{
		"pnpm-workspace.yaml": "# comentário\npackages:\n  - 'apps/*'\n  - \"services/**\"\n  - '!apps/legacy'\nonlyBuiltDependencies:\n  - esbuild\n",
		"apps/a/package.json": `{"name":"a","scripts":{"start":"x"}}`, "apps/legacy/package.json": `{"name":"legacy","scripts":{"start":"x"}}`,
		"services/x/y/package.json": `{"name":"y","scripts":{"start":"x"}}`,
	})
	if got := paths(Workspaces(repo)); !slices.Equal(got, []string{"apps/a", "services/x/y"}) {
		t.Errorf("pnpm: %v", got)
	}
}

func TestWorkspacesObjectFormGoWorkAndCargo(t *testing.T) {
	obj := project(t, map[string]string{"package.json": `{"workspaces":{"packages":["web"]}}`, "web/package.json": `{"name":"w","scripts":{"start":"x"}}`})
	if got := paths(Workspaces(obj)); !slices.Equal(got, []string{"web"}) {
		t.Errorf("formato objeto: %v", got)
	}
	gw := project(t, map[string]string{
		"go.work":      "go 1.22\n\nuse (\n\t./svc/a\n\t./svc/b\n)\nuse ./tools\n",
		"svc/a/go.mod": "module ex.com/a\n", "svc/a/main.go": "package main\n",
		"svc/b/go.mod": "module ex.com/b\n", "svc/b/lib.go": "package b\n",
		"tools/go.mod": "module ex.com/tools\n", "tools/cmd/t/main.go": "package main\n",
	})
	got := Workspaces(gw)
	if !slices.Equal(paths(got), []string{"svc/a", "tools", "svc/b"}) {
		t.Errorf("go.work: %v (deployáveis primeiro)", paths(got))
	}
	cg := project(t, map[string]string{
		"Cargo.toml":          "[workspace]\nmembers = [\n  \"crates/*\",\n]\nexclude = [\"crates/old\"]\n",
		"crates/a/Cargo.toml": "[package]\nname = \"a\"\n", "crates/a/src/main.rs": "fn main(){}",
		"crates/b/Cargo.toml": "[package]\nname = \"b\"\n", "crates/b/src/lib.rs": "",
	})
	if got := paths(Workspaces(cg)); len(got) != 2 || got[0] != "crates/a" {
		t.Errorf("cargo: %v", got)
	}
}

func TestWorkspacesFallbackScanAndSingleProjects(t *testing.T) {
	fs := project(t, map[string]string{"frontend/package.json": `{"name":"f","scripts":{"start":"x"}}`, "backend/go.mod": "module b\n", "backend/main.go": "package main\n", "docs/readme.md": "x"})
	if got := paths(Workspaces(fs)); !slices.Equal(got, []string{"backend", "frontend"}) {
		t.Errorf("sem workspaces, com pastas de projeto: %v", got)
	}
	if got := Workspaces(project(t, map[string]string{"package.json": `{"name":"solo"}`, "src/index.js": "x", "lib/package.json": "{}"})); got != nil {
		t.Errorf("um repositório de projeto único não é monorepo: %v", got)
	}
	if Workspaces(t.TempDir()) != nil {
		t.Error("repositório vazio")
	}
}

// The repository is not trusted: a committed link must never make the panel (root) read elsewhere.
func TestWorkspacesIgnoreLinks(t *testing.T) {
	outside := project(t, map[string]string{"package.json": `{"name":"secreto","scripts":{"start":"x"}}`})
	repo := project(t, map[string]string{"package.json": `{"workspaces":["apps/*","ext"]}`, "apps/ok/package.json": `{"name":"ok","scripts":{"start":"x"}}`})
	if err := os.Symlink(outside, filepath.Join(repo, "apps", "mau")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "ext")); err != nil {
		t.Fatal(err)
	}
	if got := paths(Workspaces(repo)); !slices.Equal(got, []string{"apps/ok"}) {
		t.Errorf("links para fora entraram na lista: %v", got)
	}
}
