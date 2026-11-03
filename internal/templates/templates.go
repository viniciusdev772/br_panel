// Package templates is the catalog of one-click apps (n8n, Uptime Kuma, Gitea…). A template is a
// preset for an ordinary app: repository or generated files, build and start commands, variables
// (some with generated secrets) and persistent storage. Nothing here runs Docker; the deployer
// builds and runs a template exactly like any other app, as a systemd service.
package templates

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RefPrefix starts the Repo of an app whose files come from a template instead of Git.
const RefPrefix = "template:"

// VersionVar is the variable that carries the chosen version into the build commands.
const VersionVar = "TPL_VERSION"

// EnvVar is one variable of the new app.
type EnvVar struct {
	Key    string
	Value  string // may use {{domain}}
	Secret bool   // Value is ignored; a random 256-bit hex string is generated per install
	When   string // "", "domain" (only with a domain) or "nodomain" (only without)
}

// Mount is a persistent storage folder of the new app (a panel "volume"); the app reads its real
// path from VOLUME_<NAME>.
type Mount struct{ Name, Target string }

// Template describes one installable app.
type Template struct {
	ID          string
	Name        string
	Category    string
	Icon        string // Material icon
	Summary     string
	Description string
	Docs        string // upstream documentation
	After       string // what to do once it is running

	// Source: Repo+version-as-branch when Repo is set, otherwise Files written on every deploy.
	Repo        string
	Files       map[string]string
	Version     string // default; installs may override it
	VersionHint string

	Requires []string // programs the build needs (node, npm, curl…)
	Build    string
	Start    string
	Env      []EnvVar
	Mounts   []Mount
	MemoryMB int // advice shown on the page; the app is created without a limit
}

// Ref is the Repo value of a files-based app made from t.
func (t Template) Ref() string { return RefPrefix + t.ID }

// FilesBased reports whether the sources are generated files rather than a Git checkout.
func (t Template) FilesBased() bool { return t.Repo == "" }

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// versionRe is deliberately narrow: the version ends up in a branch name and, quoted, in a shell
// command of the build.
var versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

// ValidVersion reports whether v can be used as an install version.
func ValidVersion(v string) bool { return versionRe.MatchString(v) && !strings.Contains(v, "..") }

// All returns the catalog ordered by category and name.
func All() []Template {
	out := append([]Template(nil), catalog...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Get finds a template by id.
func Get(id string) (Template, bool) {
	for _, t := range catalog {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// ForRepo finds the template an app was made from: by its "template:<id>" reference, or, for
// Git-based templates, by the repository URL.
func ForRepo(repo string) (Template, bool) {
	if id, ok := strings.CutPrefix(repo, RefPrefix); ok {
		return Get(id)
	}
	for _, t := range catalog {
		if t.Repo != "" && t.Repo == repo {
			return t, true
		}
	}
	return Template{}, false
}

// Input is what the person fills in.
type Input struct {
	Name    string // app name (validated by the caller)
	Domain  string // optional, validated by the caller
	Version string // empty = the template default
}

// Plan is the app and storage to create.
type Plan struct {
	Repo, Branch, Build, Start string
	Env                        string // KEY=value lines
	Mounts                     []Mount
}

// Instantiate turns the template and the input into what the panel stores. Secrets are random
// here, once per install, and live only in the app's variables.
func (t Template) Instantiate(in Input) (Plan, error) {
	version := in.Version
	if version == "" {
		version = t.Version
	}
	if version != "" && !ValidVersion(version) {
		return Plan{}, fmt.Errorf("versão inválida: use letras, números, ponto, hífen e sublinhado (até 40 caracteres)")
	}
	p := Plan{Repo: t.Ref(), Branch: "main", Build: t.Build, Start: t.Start, Mounts: append([]Mount(nil), t.Mounts...)}
	if t.Repo != "" {
		p.Repo, p.Branch = t.Repo, version
	}
	var env []string
	if version != "" {
		env = append(env, VersionVar+"="+version)
	}
	for _, e := range t.Env {
		if (e.When == "domain" && in.Domain == "") || (e.When == "nodomain" && in.Domain != "") {
			continue
		}
		v := strings.ReplaceAll(e.Value, "{{domain}}", in.Domain)
		if e.Secret {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return Plan{}, err
			}
			v = hex.EncodeToString(b)
		}
		env = append(env, e.Key+"="+v)
	}
	p.Env = strings.Join(env, "\n")
	return p, nil
}

// Validate checks the catalog itself; a test runs it so a bad entry never ships.
func Validate(t Template) error {
	switch {
	case !idRe.MatchString(t.ID):
		return fmt.Errorf("id inválido: %q", t.ID)
	case t.Name == "" || t.Summary == "" || t.Category == "" || t.Start == "":
		return fmt.Errorf("%s: faltam nome, resumo, categoria ou comando de início", t.ID)
	case t.Repo == "" && len(t.Files) == 0:
		return fmt.Errorf("%s: precisa de repositório ou arquivos", t.ID)
	case t.Repo != "" && !strings.HasPrefix(t.Repo, "https://"):
		return fmt.Errorf("%s: repositório precisa ser https", t.ID)
	case t.Version != "" && !ValidVersion(t.Version):
		return fmt.Errorf("%s: versão padrão inválida", t.ID)
	}
	for name := range t.Files {
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return fmt.Errorf("%s: nome de arquivo inválido: %q", t.ID, name)
		}
	}
	return nil
}
