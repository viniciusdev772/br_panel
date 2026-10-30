// Package detect inspects a checked-out project and suggests how to build and start it.
package detect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/viniciusdev772/br_panel/internal/validate"
)

// Detection is a suggested configuration for a checked-out project.
// Commands use $PORT and $HOST, which the panel always provides:
// HOST is 127.0.0.1 behind a domain (Nginx) and 0.0.0.0 otherwise.
type Detection struct {
	Kind     string   `json:"kind"`
	Build    string   `json:"build"`
	Start    string   `json:"start"`
	Port     int      `json:"port"`
	Requires []string `json:"requires"` // executables the commands need
	Notes    []string `json:"notes"`
}

var requireHints = map[string]string{
	"node":     "instale com o nvm pelo botão abaixo",
	"npm":      "instale com o nvm pelo botão abaixo",
	"pnpm":     "vem junto com o Node.js do nvm; use o botão abaixo",
	"yarn":     "vem junto com o Node.js do nvm; use o botão abaixo",
	"bun":      "https://bun.sh",
	"python3":  "apt install python3 python3-venv python3-pip",
	"go":       "apt install golang-go",
	"cargo":    "https://rustup.rs",
	"bundle":   "apt install ruby-bundler",
	"composer": "apt install composer",
	"php":      "apt install php-cli",
}

var safeRelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// safeRel reports whether s is a plain relative path that is safe to put in a shell command.
func safeRel(s string) bool {
	return safeRelRe.MatchString(s) && !strings.Contains(s, "..") && len(s) <= 100
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func isDir(dir, name string) bool {
	st, err := os.Stat(filepath.Join(dir, name))
	return err == nil && st.IsDir()
}

func readSmall(dir, name string) string {
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, 1<<20)
	n, _ := f.Read(b)
	return string(b[:n])
}

// Detect inspects dir and returns the suggested build/start commands, or nil.
// Precedence: brpanel.json overrides, then Procfile's web process, then the language rules.
func Detect(dir string) *Detection {
	var d *Detection
	switch {
	case exists(dir, "package.json"):
		d = detectNode(dir)
	case exists(dir, "go.mod"):
		d = detectGo(dir)
	case exists(dir, "Cargo.toml"):
		d = detectRust(dir)
	case exists(dir, "requirements.txt"), exists(dir, "pyproject.toml"), exists(dir, "Pipfile"):
		d = detectPython(dir)
	case exists(dir, "Gemfile"):
		d = detectRuby(dir)
	case exists(dir, "composer.json"):
		d = detectPHP(dir)
	case exists(dir, "index.html"), exists(dir, "public/index.html"):
		d = detectStatic(dir)
	}
	if web := procfileWeb(dir); web != "" {
		if d == nil {
			d = &Detection{Kind: "Procfile", Port: 3000}
		}
		d.Start = web
		d.Notes = append(d.Notes, "comando de início lido do Procfile")
	}
	if o := readOverride(dir); o != nil {
		if d == nil {
			d = &Detection{Kind: "brpanel.json", Port: 3000}
		}
		if o.Build != nil {
			d.Build = *o.Build
		}
		if o.Start != nil {
			d.Start = *o.Start
		}
		if o.Port > 0 {
			d.Port = o.Port
		}
		d.Notes = append(d.Notes, "ajustado por brpanel.json")
	}
	if d != nil && d.Start == "" {
		d.Notes = append(d.Notes, "não foi possível descobrir o comando de início; informe-o manualmente")
	}
	return d
}

type override struct {
	Build *string `json:"build"`
	Start *string `json:"start"`
	Port  int     `json:"port"`
}

func readOverride(dir string) *override {
	raw := readSmall(dir, "brpanel.json")
	if raw == "" {
		return nil
	}
	var o override
	if json.Unmarshal([]byte(raw), &o) != nil {
		return nil
	}
	for _, c := range []*string{o.Build, o.Start} {
		if c != nil && validate.Command("comando", *c, false) != nil {
			return nil
		}
	}
	return &o
}

var procfileRe = regexp.MustCompile(`(?m)^web:\s*(.+?)\s*$`)

func procfileWeb(dir string) string {
	m := procfileRe.FindStringSubmatch(readSmall(dir, "Procfile"))
	if m == nil || validate.Command("web", m[1], true) != nil {
		return ""
	}
	return m[1]
}

// ---- Node.js ----

func detectNode(dir string) *Detection {
	var pkg struct {
		Main            string            `json:"main"`
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		Engines         map[string]string `json:"engines"`
	}
	_ = json.Unmarshal([]byte(readSmall(dir, "package.json")), &pkg)
	has := func(dep string) bool {
		_, a := pkg.Dependencies[dep]
		_, b := pkg.DevDependencies[dep]
		return a || b
	}

	pm, install, run, exec := "npm", "npm install", "npm run", "npx"
	switch {
	case exists(dir, "pnpm-lock.yaml"):
		pm, install, run, exec = "pnpm", "pnpm install --frozen-lockfile", "pnpm run", "pnpm exec"
	case exists(dir, "yarn.lock"):
		pm, install, run, exec = "yarn", "yarn install --frozen-lockfile", "yarn", "yarn"
	case exists(dir, "bun.lockb"), exists(dir, "bun.lock"):
		pm, install, run, exec = "bun", "bun install", "bun run", "bunx"
	case exists(dir, "package-lock.json"), exists(dir, "npm-shrinkwrap.json"):
		install = "npm ci"
	}

	d := &Detection{Kind: "Node.js (" + pm + ")", Port: 3000, Requires: []string{"node", pm}, Build: install}
	if pm == "bun" {
		d.Requires = []string{"bun"}
	}
	if _, ok := pkg.Scripts["build"]; ok {
		d.Build += " && " + run + " build"
	}
	if v := pkg.Engines["node"]; v != "" {
		d.Notes = append(d.Notes, "o projeto pede Node "+v+" (engines.node); confira a versão instalada")
	}

	switch {
	case pkg.Scripts["start"] != "":
		start := pm + " start"
		if pm == "bun" {
			start = "bun run start"
		}
		d.Start = "NODE_ENV=production " + start
	case has("next"):
		d.Start = "NODE_ENV=production " + exec + " next start -p $PORT -H $HOST"
	case has("nuxt"):
		d.Start = "node .output/server/index.mjs"
	case has("vite"):
		d.Start = exec + " vite preview --host $HOST --port $PORT"
		d.Notes = append(d.Notes, "site estático (Vite): servido com `vite preview`")
	case has("react-scripts") && pkg.Scripts["build"] != "":
		d.Start = "python3 -m http.server $PORT --bind $HOST --directory build"
		d.Requires = append(d.Requires, "python3")
		d.Notes = append(d.Notes, "site estático (CRA): servido por python3; rotas do SPA exigem fallback no Nginx")
	default:
		for _, f := range []string{pkg.Main, "server.js", "index.js", "app.js", "src/index.js", "src/server.js", "dist/index.js"} {
			if f != "" && safeRel(f) && exists(dir, f) {
				d.Start = "NODE_ENV=production node " + f
				break
			}
		}
	}
	return d
}

// ---- Go ----

var goMainRe = regexp.MustCompile(`(?m)^package main\b`)

func detectGo(dir string) *Detection {
	target := "."
	if !hasRootMain(dir) {
		if entries, err := os.ReadDir(filepath.Join(dir, "cmd")); err == nil {
			var cmds []string
			for _, e := range entries {
				if e.IsDir() && safeRel(e.Name()) {
					cmds = append(cmds, e.Name())
				}
			}
			if len(cmds) == 1 {
				target = "./cmd/" + cmds[0]
			}
		}
	}
	return &Detection{Kind: "Go", Port: 8080, Requires: []string{"go"},
		Build: "go build -o app " + target, Start: "./app",
		Notes: []string{"o programa deve ler a porta da variável PORT"}}
}

func hasRootMain(dir string) bool {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		if goMainRe.MatchString(readSmall(dir, filepath.Base(f))) {
			return true
		}
	}
	return false
}

// ---- Rust ----

var cargoNameRe = regexp.MustCompile(`(?m)^name\s*=\s*"([A-Za-z0-9_-]+)"`)

func detectRust(dir string) *Detection {
	d := &Detection{Kind: "Rust", Port: 8080, Requires: []string{"cargo"}, Build: "cargo build --release",
		Notes: []string{"o programa deve ler a porta da variável PORT"}}
	if m := cargoNameRe.FindStringSubmatch(readSmall(dir, "Cargo.toml")); m != nil {
		d.Start = "./target/release/" + m[1]
	}
	return d
}

// ---- Python ----

var (
	djangoSettingsRe = regexp.MustCompile(`DJANGO_SETTINGS_MODULE["']\s*,\s*["']([A-Za-z0-9_]+)\.`)
	pyModuleRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
)

func detectPython(dir string) *Detection {
	reqs := strings.ToLower(readSmall(dir, "requirements.txt") + readSmall(dir, "pyproject.toml") + readSmall(dir, "Pipfile"))
	d := &Detection{Kind: "Python", Port: 8000, Requires: []string{"python3"}}
	install := ".venv/bin/pip install -r requirements.txt"
	if !exists(dir, "requirements.txt") {
		install = ".venv/bin/pip install ."
	}
	build := "python3 -m venv .venv && " + install
	need := func(pkg string) {
		if !strings.Contains(reqs, pkg) {
			build += " && .venv/bin/pip install " + pkg
		}
	}

	pyHas := func(file, needle string) bool { return strings.Contains(readSmall(dir, file), needle) }
	switch {
	case exists(dir, "manage.py") && strings.Contains(reqs, "django"):
		d.Kind = "Python (Django)"
		pkg := ""
		if m := djangoSettingsRe.FindStringSubmatch(readSmall(dir, "manage.py")); m != nil {
			pkg = m[1]
		}
		if pkg != "" && pyModuleRe.MatchString(pkg) {
			need("gunicorn")
			d.Start = ".venv/bin/gunicorn " + pkg + ".wsgi:application --bind $HOST:$PORT"
		}
		d.Notes = append(d.Notes, "rode as migrações manualmente, se necessário (python manage.py migrate)")
	case strings.Contains(reqs, "fastapi") || strings.Contains(reqs, "uvicorn"):
		d.Kind = "Python (FastAPI)"
		for _, c := range [][2]string{{"main.py", "main:app"}, {"app/main.py", "app.main:app"}, {"app.py", "app:app"}} {
			if exists(dir, c[0]) {
				need("uvicorn")
				d.Start = ".venv/bin/uvicorn " + c[1] + " --host $HOST --port $PORT"
				break
			}
		}
	case strings.Contains(reqs, "flask"):
		d.Kind = "Python (Flask)"
		for _, c := range [][2]string{{"app.py", "app:app"}, {"wsgi.py", "wsgi:app"}, {"main.py", "main:app"}} {
			if exists(dir, c[0]) && (pyHas(c[0], "Flask(") || c[0] == "wsgi.py") {
				need("gunicorn")
				d.Start = ".venv/bin/gunicorn " + c[1] + " --bind $HOST:$PORT"
				break
			}
		}
	}
	if d.Start == "" {
		for _, f := range []string{"main.py", "app.py", "server.py", "run.py"} {
			if exists(dir, f) {
				d.Start = ".venv/bin/python " + f
				break
			}
		}
	}
	d.Build = build
	return d
}

// ---- Ruby ----

func detectRuby(dir string) *Detection {
	d := &Detection{Kind: "Ruby", Port: 3000, Requires: []string{"bundle"},
		Build: "bundle config set --local deployment true && bundle install"}
	switch {
	case exists(dir, "config/application.rb"):
		d.Kind = "Ruby on Rails"
		d.Start = "RAILS_ENV=production bundle exec rails server -b $HOST -p $PORT"
		d.Notes = append(d.Notes, "defina SECRET_KEY_BASE nas variáveis de ambiente")
	case exists(dir, "config.ru"):
		d.Kind = "Ruby (Rack)"
		d.Start = "bundle exec rackup -o $HOST -p $PORT"
	}
	return d
}

// ---- PHP ----

func detectPHP(dir string) *Detection {
	root := "."
	if isDir(dir, "public") {
		root = "public"
	}
	return &Detection{Kind: "PHP", Port: 8000, Requires: []string{"php", "composer"},
		Build: "composer install --no-dev --optimize-autoloader",
		Start: "php -S $HOST:$PORT -t " + root,
		Notes: []string{"usa o servidor embutido do PHP (adequado para apps simples)"}}
}

// ---- static sites ----

func detectStatic(dir string) *Detection {
	start := "python3 -m http.server $PORT --bind $HOST"
	if !exists(dir, "index.html") && exists(dir, "public/index.html") {
		start += " --directory public"
	}
	return &Detection{Kind: "Site estático", Port: 8000, Requires: []string{"python3"}, Start: start}
}

// MissingRequirements lists executables from d.Requires that are not in PATH, with install hints.
func MissingRequirements(d *Detection, lookPath func(string) (string, error)) []string {
	var out []string
	for _, r := range d.Requires {
		if _, err := lookPath(r); err != nil {
			out = append(out, fmt.Sprintf("%s (%s)", r, requireHints[r]))
		}
	}
	return out
}
