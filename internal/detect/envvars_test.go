package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envByName(vars []EnvVar) map[string]EnvVar {
	m := map[string]EnvVar{}
	for _, v := range vars {
		m[v.Name] = v
	}
	return m
}

func TestEnvVars(t *testing.T) {
	dir := project(t, map[string]string{
		"package.json":        `{"scripts":{"start":"node src/a.js"}}`,
		".env.example":        "# comentário\nMONGODB_URI=mongodb://localhost\nEMPTY_ONE=\nexport QUOTED=\"\"\nlower=1\n",
		"src/a.js":            "const k = process.env.JWT_PRIVATE_KEY;\nconst p = process.env.WITH_DEFAULT || 'x';\nconst q = process.env['BRACKET'] ?? 1;\nconst e1 = process.env.EMPTY_NULLISH ?? \"\";\nconst e2 = process.env.EMPTY_OR || '';\nconst e3 = process.env.TERNARY ? 1 : 2;\nconst r = process.env.CHAINED?.trim();\nconst s = process.env.PORT;\n",
		"src/b.py":            "import os\nos.environ['PY_REQUIRED']\nos.getenv('PY_OPTIONAL', 'x')\n",
		"src/c.go":            "package main\nvar _ = os.Getenv(\"GO_VAR\")\n",
		"node_modules/x/i.js": "process.env.IN_NODE_MODULES",
		"tests/t.js":          "process.env.IN_TESTS",
		"web/package.json":    `{}`,
		"web/w.js":            "process.env.IN_SUBPROJECT",
		"src/big.txt":         "process.env.NOT_CODE",
	})
	got := envByName(EnvVars(dir, dir))
	want := map[string]bool{ // name -> required
		"JWT_PRIVATE_KEY": true, "WITH_DEFAULT": false, "BRACKET": false, "CHAINED": true, "PORT": true, "EMPTY_NULLISH": true, "EMPTY_OR": true, "TERNARY": false,
		"MONGODB_URI": false, "EMPTY_ONE": true, "QUOTED": true, "PY_REQUIRED": true, "PY_OPTIONAL": false, "GO_VAR": false,
	}
	for n, req := range want {
		if v, ok := got[n]; !ok || v.Required != req {
			t.Errorf("%s: %+v (esperado required=%v)", n, v, req)
		}
	}
	for _, n := range []string{"IN_NODE_MODULES", "IN_TESTS", "IN_SUBPROJECT", "NOT_CODE", "lower"} {
		if _, ok := got[n]; ok {
			t.Errorf("%s não deveria ser listada", n)
		}
	}
	if got["MONGODB_URI"].Source != ".env.example" {
		t.Errorf("origem: %+v", got["MONGODB_URI"])
	}
}

func TestEnvVarsIgnoresSymlinkedExampleAndHugeFiles(t *testing.T) {
	dir := project(t, map[string]string{"package.json": `{}`, "big.js": "process.env.AA" + strings.Repeat(" ", maxEnvFileSize) + "\nprocess.env.PAST_LIMIT"})
	outside := project(t, map[string]string{"secret.env": "LEAKED=1\n"})
	if err := os.Symlink(filepath.Join(outside, "secret.env"), filepath.Join(dir, ".env.example")); err != nil {
		t.Skip("symlink indisponível:", err)
	}
	got := envByName(EnvVars(dir, dir))
	if _, ok := got["LEAKED"]; ok {
		t.Error(".env.example que é link para fora do repositório não pode ser lido")
	}
	if _, ok := got["PAST_LIMIT"]; ok {
		t.Error("o que passa do limite de tamanho não deve ser lido")
	}
}

func TestUndefined(t *testing.T) {
	vars := []EnvVar{{Name: "PORT"}, {Name: "VOLUME_DATA"}, {Name: "A", Required: true}, {Name: "B"}, {Name: "CC"}}
	defined := CmdAssigns("NODE_ENV=production CC=1 pnpm start && echo $X")
	defined["B"] = true
	got := Undefined(vars, defined)
	if len(got) != 1 || got[0].Name != "A" {
		t.Errorf("%+v", got)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		mono  bool
		paths []string
	}{
		{"projeto simples", map[string]string{"package.json": `{}`, "src/a.js": ""}, false, nil},
		{"raiz com subprojetos", map[string]string{"package.json": `{"scripts":{"start":"x"}}`, "web/package.json": `{"dependencies":{"vite":"1"}}`,
			"video/package.json": `{}`, "android/build.gradle.kts": ""}, true, []string{"web", "video", "android"}},
		{"docs e exemplos não contam", map[string]string{"package.json": `{}`, "docs/package.json": `{}`, "examples/a/package.json": `{}`, "public/index.html": ""}, false, nil},
		{"workspaces declarados", map[string]string{"package.json": `{"workspaces":["apps/*"]}`, "apps/a/package.json": `{}`}, true, []string{"apps/a"}},
	}
	for _, c := range cases {
		l := Classify(project(t, c.files))
		if l.Monorepo != c.mono || len(l.Apps) != len(c.paths) {
			t.Errorf("%s: %+v", c.name, l)
			continue
		}
		got := map[string]bool{}
		for _, a := range l.Apps {
			got[a.Path] = true
		}
		for _, p := range c.paths {
			if !got[p] {
				t.Errorf("%s: falta %s em %+v", c.name, p, l.Apps)
			}
		}
	}
	for _, a := range Classify(project(t, map[string]string{"package.json": `{}`, "android/build.gradle.kts": ""})).Apps {
		if a.Deployable {
			t.Errorf("projeto Android não é publicável: %+v", a)
		}
	}
}
