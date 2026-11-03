package detect

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// EnvVar is an environment variable a project reads.
type EnvVar struct {
	Name     string `json:"name"`
	Required bool   `json:"required"` // no default seen: the app most likely fails without it
	Source   string `json:"source"`   // where it was found: an example file or a source file
}

const (
	maxEnvFiles    = 4000
	maxEnvFileSize = 400 << 10
	maxEnvVars     = 200
)

// exampleFiles are the conventional "copy me to .env" files.
var exampleFiles = []string{".env.example", ".env.sample", ".env.template", ".env.dist", "example.env"}

// auxDirs hold tests, docs and samples: their code is not the app and they do not make a
// repository a monorepo.
var auxDirs = map[string]bool{"test": true, "tests": true, "__tests__": true, "e2e": true, "spec": true, "example": true, "examples": true,
	"docs": true, "doc": true, "fixtures": true, "__fixtures__": true, "sample": true, "samples": true, "demo": true, "demos": true,
	"bench": true, "benchmarks": true, ".git": true, ".next": true, ".cache": true, ".venv": true, "venv": true}

var (
	envName = `[A-Z][A-Z0-9_]{1,63}`
	// process.env.X, process.env["X"]; group 3 is a fallback operator right after it (||, ??, ?).
	// "?." is optional chaining, not a default.
	jsEnvRe = regexp.MustCompile(`process\.env(?:\.(` + envName + `)|\[\s*['"](` + envName + `)['"]\s*\])(\s*(?:\|\||\?\?|\?[^.]))?`)
	// what follows `||`/`??` when the "default" is nothing at all
	emptyDefault = regexp.MustCompile("^\\s*(?:\"\"|''|``|null\\b|undefined\\b)")
	envKeyRe     = regexp.MustCompile(`^` + envName + `$`)
	// os.environ["X"] (required) · os.environ.get("X"[, default]) · os.getenv("X"[, default])
	pyEnvRe = regexp.MustCompile(`os\.environ\[\s*['"](` + envName + `)['"]\s*\]|os\.(?:environ\.get|getenv)\(\s*['"](` + envName + `)['"]`)
	goEnvRe = regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\(\s*"(` + envName + `)"\s*\)`)
	cmdSet  = regexp.MustCompile(`(?:^|[\s;&|(])(` + envName + `)=`)
)

// EnvVars lists the variables the project in dir reads: the keys of its .env.example (also the
// one at the repository root) and the names used in its source. An example entry with no value,
// or a name read with no default, is Required. Nothing is executed; sub-projects with a manifest
// of their own are not entered, they are other apps of a monorepo.
func EnvVars(dir, root string) []EnvVar {
	found := map[string]*EnvVar{}
	add := func(name string, required bool, src string) {
		if v := found[name]; v != nil {
			v.Required = v.Required || required
			return
		}
		if len(found) < maxEnvVars {
			found[name] = &EnvVar{Name: name, Required: required, Source: src}
		}
	}
	dirs := []string{dir}
	if root != "" && root != dir {
		dirs = append(dirs, root)
	}
	for _, d := range dirs {
		for _, f := range exampleFiles {
			if plainFile(d, f) {
				for _, line := range strings.Split(readSmall(d, f), "\n") {
					line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
					k, v, ok := strings.Cut(line, "=")
					k = strings.TrimSpace(k)
					if !ok || !envKeyRe.MatchString(k) {
						continue
					}
					v, _, _ = strings.Cut(v, " #")
					add(k, strings.Trim(strings.TrimSpace(v), `"'`) == "", f)
				}
			}
		}
	}
	files := 0
	_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || files >= maxEnvFiles {
			return filepath.SkipDir
		}
		name := e.Name()
		if e.IsDir() {
			if p != dir && (skipDirs[name] || auxDirs[name] || strings.HasPrefix(name, ".") || hasProject(p)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() {
			return nil
		}
		ext := filepath.Ext(name)
		if ext != ".js" && ext != ".mjs" && ext != ".cjs" && ext != ".ts" && ext != ".tsx" && ext != ".jsx" && ext != ".py" && ext != ".go" {
			return nil
		}
		files++
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		b, _ := io.ReadAll(io.LimitReader(f, maxEnvFileSize))
		f.Close()
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		src := string(b)
		switch ext {
		case ".py":
			for _, m := range pyEnvRe.FindAllStringSubmatchIndex(src, -1) {
				if m[2] >= 0 { // os.environ["X"] raises when missing
					add(src[m[2]:m[3]], true, rel)
				} else {
					add(src[m[4]:m[5]], false, rel)
				}
			}
		case ".go":
			for _, m := range goEnvRe.FindAllStringSubmatch(src, -1) {
				add(m[1], false, rel) // Go code checks for "" by hand: a default cannot be told apart
			}
		default:
			for _, m := range jsEnvRe.FindAllStringSubmatchIndex(src, -1) {
				var name, op string
				if m[2] >= 0 {
					name = src[m[2]:m[3]]
				} else {
					name = src[m[4]:m[5]]
				}
				if m[6] >= 0 {
					op = strings.TrimSpace(src[m[6]:m[7]])
				}
				ternary := strings.HasPrefix(op, "?") && op != "??" // `X ? a : b` tests it, no default involved
				// `?? ""` only turns "unset" into "empty": the code still needs a value.
				add(name, op == "" || (!ternary && emptyDefault.MatchString(src[m[7]:])), rel)
			}
		}
		return nil
	})
	out := make([]EnvVar, 0, len(found))
	for _, v := range found {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// plainFile reports whether dir/name is a regular file (not a link out of the repository).
func plainFile(dir, name string) bool {
	fi, err := os.Lstat(filepath.Join(dir, name))
	return err == nil && fi.Mode().IsRegular()
}

// hasProject reports whether folder p holds a project manifest of its own.
func hasProject(p string) bool {
	for _, m := range manifests {
		if m != "index.html" && m != "Procfile" && m != "brpanel.json" && exists(p, m) {
			return true
		}
	}
	return false
}

// providedByPanel are names the panel or the system already set for every app.
func providedByPanel(n string) bool {
	switch n {
	case "PORT", "HOST", "HOME", "PATH", "LANG", "NODE_ENV", "TZ", "CI", "PWD", "USER", "SHELL", "HOSTNAME", "TERM":
		return true
	}
	return strings.HasPrefix(n, "VOLUME_")
}

// CmdAssigns returns the variables set inline in shell commands ("NODE_ENV=production pnpm start").
func CmdAssigns(cmds ...string) map[string]bool {
	out := map[string]bool{}
	for _, c := range cmds {
		for _, m := range cmdSet.FindAllStringSubmatch(c, -1) {
			out[m[1]] = true
		}
	}
	return out
}

// Undefined keeps the variables that neither the panel nor the app (defined) provides.
func Undefined(vars []EnvVar, defined map[string]bool) []EnvVar {
	var out []EnvVar
	for _, v := range vars {
		if !providedByPanel(v.Name) && !defined[v.Name] {
			out = append(out, v)
		}
	}
	return out
}
