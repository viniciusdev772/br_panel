package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectNode(t *testing.T) {
	cases := []struct {
		name        string
		files       map[string]string
		kind, build string
		start       string
	}{
		{"npm com lock, build e start", map[string]string{
			"package.json":      `{"scripts":{"build":"tsc","start":"node dist/index.js"}}`,
			"package-lock.json": "{}"},
			"Node.js (npm)", "npm ci && npm run build", "NODE_ENV=production npm start"},
		{"pnpm", map[string]string{"package.json": `{"scripts":{"start":"x"}}`, "pnpm-lock.yaml": ""},
			"Node.js (pnpm)", "pnpm install --frozen-lockfile", "NODE_ENV=production pnpm start"},
		{"yarn", map[string]string{"package.json": `{"scripts":{"build":"b","start":"x"}}`, "yarn.lock": ""},
			"Node.js (yarn)", "yarn install --frozen-lockfile && yarn build", "NODE_ENV=production yarn start"},
		{"sem lock", map[string]string{"package.json": `{"scripts":{"start":"x"}}`},
			"Node.js (npm)", "npm install", "NODE_ENV=production npm start"},
		{"next sem script start", map[string]string{"package.json": `{"dependencies":{"next":"14"},"scripts":{"build":"next build"}}`},
			"Node.js (npm)", "npm install && npm run build", "NODE_ENV=production npx next start -p $PORT -H $HOST"},
		{"vite", map[string]string{"package.json": `{"devDependencies":{"vite":"5"},"scripts":{"build":"vite build"}}`},
			"Node.js (npm)", "npm install && npm run build", "npx vite preview --host $HOST --port $PORT"},
		{"arquivo principal", map[string]string{"package.json": `{"main":"src/main.js"}`, "src/main.js": ""},
			"Node.js (npm)", "npm install", "NODE_ENV=production node src/main.js"},
	}
	for _, c := range cases {
		d := detectProject(project(t, c.files))
		if d == nil || d.Kind != c.kind || d.Build != c.build || d.Start != c.start {
			t.Errorf("%s: %+v", c.name, d)
		}
	}
}

func TestDetectNodeRejectsUnsafeMain(t *testing.T) {
	d := detectProject(project(t, map[string]string{"package.json": `{"main":"x.js; rm -rf /"}`}))
	if d == nil || d.Start != "" || !strings.Contains(strings.Join(d.Notes, " "), "informe") {
		t.Errorf("main inseguro não deveria virar comando: %+v", d)
	}
}

func TestDetectOtherLanguages(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		kind  string
		build string
		start string
	}{
		{"go raiz", map[string]string{"go.mod": "module x", "main.go": "package main\n"},
			"Go", "go build -o app .", "./app"},
		{"go cmd", map[string]string{"go.mod": "module x", "cmd/api/main.go": "package main\n"},
			"Go", "go build -o app ./cmd/api", "./app"},
		{"rust", map[string]string{"Cargo.toml": "[package]\nname = \"meu-app\"\n"},
			"Rust", "cargo build --release", "./target/release/meu-app"},
		{"python fastapi", map[string]string{"requirements.txt": "fastapi\n", "main.py": "app = FastAPI()"},
			"Python (FastAPI)", "python3 -m venv .venv && .venv/bin/pip install -r requirements.txt && .venv/bin/pip install uvicorn",
			".venv/bin/uvicorn main:app --host $HOST --port $PORT"},
		{"python flask com gunicorn", map[string]string{"requirements.txt": "flask\ngunicorn\n", "app.py": "app = Flask(__name__)"},
			"Python (Flask)", "python3 -m venv .venv && .venv/bin/pip install -r requirements.txt",
			".venv/bin/gunicorn app:app --bind $HOST:$PORT"},
		{"django", map[string]string{"requirements.txt": "Django\n", "manage.py": `os.environ.setdefault("DJANGO_SETTINGS_MODULE", "meusite.settings")`},
			"Python (Django)", "python3 -m venv .venv && .venv/bin/pip install -r requirements.txt && .venv/bin/pip install gunicorn",
			".venv/bin/gunicorn meusite.wsgi:application --bind $HOST:$PORT"},
		{"python simples", map[string]string{"requirements.txt": "requests\n", "main.py": "print(1)"},
			"Python", "python3 -m venv .venv && .venv/bin/pip install -r requirements.txt", ".venv/bin/python main.py"},
		{"rails", map[string]string{"Gemfile": "", "config/application.rb": ""},
			"Ruby on Rails", "bundle config set --local deployment true && bundle install",
			"RAILS_ENV=production bundle exec rails server -b $HOST -p $PORT"},
		{"php", map[string]string{"composer.json": "{}", "public/index.php": ""},
			"PHP", "composer install --no-dev --optimize-autoloader", "php -S $HOST:$PORT -t public"},
		{"estático", map[string]string{"index.html": "<h1>oi</h1>"},
			"Site estático", "", "python3 -m http.server $PORT --bind $HOST"},
		{"estático em public", map[string]string{"public/index.html": ""},
			"Site estático", "", "python3 -m http.server $PORT --bind $HOST --directory public"},
	}
	for _, c := range cases {
		d := detectProject(project(t, c.files))
		if d == nil || d.Kind != c.kind || d.Build != c.build || d.Start != c.start {
			t.Errorf("%s: %+v", c.name, d)
		}
	}
	if detectProject(project(t, map[string]string{"README.md": "x"})) != nil {
		t.Error("projeto desconhecido deve retornar nil")
	}
}

func TestProcfileAndOverride(t *testing.T) {
	d := detectProject(project(t, map[string]string{
		"package.json": `{"scripts":{"start":"x"}}`,
		"Procfile":     "release: echo x\nweb: node custom.js --port $PORT\n",
	}))
	if d.Start != "node custom.js --port $PORT" {
		t.Errorf("Procfile: %q", d.Start)
	}
	d = detectProject(project(t, map[string]string{
		"package.json": `{"scripts":{"start":"x"}}`,
		"Procfile":     "web: node a.js",
		"brpanel.json": `{"build":"make","start":"./bin/run","port":9000}`,
	}))
	if d.Build != "make" || d.Start != "./bin/run" || d.Port != 9000 {
		t.Errorf("brpanel.json: %+v", d)
	}
	// invalid override (multi-line) is ignored
	d = detectProject(project(t, map[string]string{
		"package.json": `{"scripts":{"start":"x"}}`,
		"brpanel.json": `{"start":"a\nb"}`,
	}))
	if d.Start != "NODE_ENV=production npm start" {
		t.Errorf("override inválido deveria ser ignorado: %q", d.Start)
	}
	// Procfile alone is enough
	if d := detectProject(project(t, map[string]string{"Procfile": "web: ./run"})); d == nil || d.Start != "./run" {
		t.Errorf("%+v", d)
	}
}

func TestMissingRequirements(t *testing.T) {
	d := &Detection{Requires: []string{"node", "npm"}}
	look := func(s string) (string, error) {
		if s == "node" {
			return "/usr/bin/node", nil
		}
		return "", errors.New("não encontrado")
	}
	m := missingRequirements(d, look)
	if len(m) != 1 || !strings.HasPrefix(m[0], "npm (") {
		t.Errorf("%v", m)
	}
}
