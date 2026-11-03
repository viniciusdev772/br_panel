package detect

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Candidate is a folder of a repository that looks like an app a person would deploy: the unit a
// monorepo is made of.
type Candidate struct {
	Path       string `json:"path"`       // relative to the repository root, slash separated
	Name       string `json:"name"`       // package or module name when there is one
	Kind       string `json:"kind"`       // what Detect makes of it, e.g. "Node.js (pnpm)"
	Deployable bool   `json:"deployable"` // false for libraries and shared packages
}

const (
	maxCandidates = 60
	maxGlobHits   = 300
	maxGlobDepth  = 4
)

// manifests whose presence makes a folder worth offering when no workspace file lists the apps.
var manifests = []string{"package.json", "go.mod", "Cargo.toml", "requirements.txt", "pyproject.toml", "Pipfile", "Gemfile", "composer.json", "Procfile", "brpanel.json", "index.html"}

// skipDirs are never entered or offered.
var skipDirs = map[string]bool{"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true, "coverage": true, "__pycache__": true}

// Workspaces lists the apps of a monorepo found in repo (the checkout root), without running
// anything from it: it reads the workspace files of npm/yarn/bun (package.json), pnpm
// (pnpm-workspace.yaml), Go (go.work) and Cargo, and otherwise falls back to the folders that
// hold a project manifest. A repository that is a single project returns nil.
func Workspaces(repo string) []Candidate {
	patterns, listed := workspacePatterns(repo)
	var dirs []string
	if listed {
		dirs = expand(repo, patterns)
	} else if rootIsProject(repo) {
		return nil
	} else {
		dirs = scanManifests(repo)
	}
	seen := map[string]bool{}
	var out []Candidate
	for _, d := range dirs {
		if seen[d] || d == "" || !plainDir(repo, d) {
			continue
		}
		seen[d] = true
		c := describe(repo, d)
		if hasManifest(repo, d) {
			out = append(out, c)
		}
		if len(out) >= maxCandidates {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Deployable != out[j].Deployable {
			return out[i].Deployable
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// plainDir reports whether rel is a real folder inside repo, with no symbolic link on the way: the
// repository is not trusted and the panel (root) reads what it lists.
func plainDir(repo, rel string) bool {
	p := repo
	for _, seg := range strings.Split(rel, "/") {
		p = filepath.Join(p, seg)
		fi, err := os.Lstat(p)
		if err != nil || !fi.IsDir() {
			return false
		}
	}
	return true
}

func hasManifest(repo, rel string) bool {
	dir := filepath.Join(repo, filepath.FromSlash(rel))
	for _, m := range manifests {
		if exists(dir, m) {
			return true
		}
	}
	return false
}

// IsMonorepo reports whether the repository declares workspaces (or holds several projects).
func IsMonorepo(repo string) bool { return len(Workspaces(repo)) > 0 }

func rootIsProject(repo string) bool {
	for _, m := range manifests {
		if m != "index.html" && exists(repo, m) {
			return true
		}
	}
	return false
}

// workspacePatterns reads the folder patterns every supported tool uses to declare its apps and
// packages. listed is true when some workspace file exists.
func workspacePatterns(repo string) (patterns []string, listed bool) {
	var pkg struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if json.Unmarshal([]byte(readSmall(repo, "package.json")), &pkg) == nil && len(pkg.Workspaces) > 0 {
		var arr []string
		var obj struct {
			Packages []string `json:"packages"`
		}
		if json.Unmarshal(pkg.Workspaces, &arr) == nil {
			patterns, listed = append(patterns, arr...), true
		} else if json.Unmarshal(pkg.Workspaces, &obj) == nil {
			patterns, listed = append(patterns, obj.Packages...), true
		}
	}
	if y := readSmall(repo, "pnpm-workspace.yaml"); y != "" {
		patterns, listed = append(patterns, yamlPackages(y)...), true
	}
	if g := readSmall(repo, "go.work"); g != "" {
		patterns, listed = append(patterns, goWorkUses(g)...), true
	}
	if c := readSmall(repo, "Cargo.toml"); strings.Contains(c, "[workspace]") {
		patterns, listed = append(patterns, cargoMembers(c)...), true
	}
	return patterns, listed
}

// yamlPackages reads the `packages:` list of pnpm-workspace.yaml (a plain list of globs).
func yamlPackages(y string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(y, "\n") {
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "#") || trim == "":
			continue
		case strings.HasPrefix(line, "packages:"):
			in = true
		case in && strings.HasPrefix(trim, "-"):
			v := strings.TrimSpace(strings.TrimPrefix(trim, "-"))
			if i := strings.Index(v, " #"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			out = append(out, strings.Trim(v, `"'`))
		case !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t"):
			in = false // another top-level key
		}
	}
	return out
}

var goUseRe = regexp.MustCompile(`(?m)^\s*use\s+(\(([^)]*)\)|(\S+))`)

// goWorkUses reads the `use` directives of go.work (single or block form).
func goWorkUses(g string) []string {
	var out []string
	for _, m := range goUseRe.FindAllStringSubmatch(g, -1) {
		list := m[3]
		if m[2] != "" {
			list = m[2]
		}
		for _, f := range strings.Fields(list) {
			if !strings.HasPrefix(f, "//") {
				out = append(out, strings.Trim(f, `"`))
			}
		}
	}
	return out
}

var cargoMembersRe = regexp.MustCompile(`(?s)members\s*=\s*\[(.*?)\]`)

// cargoMembers reads `members = [...]` of the [workspace] table.
func cargoMembers(c string) []string {
	i := strings.Index(c, "[workspace]")
	m := cargoMembersRe.FindStringSubmatch(c[i:])
	if m == nil {
		return nil
	}
	var out []string
	for _, f := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' || r == '\r' }) {
		f = strings.Trim(f, `"'`)
		if f != "" && !strings.HasPrefix(f, "#") {
			out = append(out, f)
		}
	}
	return out
}

// expand turns workspace patterns into existing folders. Patterns starting with "!" exclude.
func expand(repo string, patterns []string) []string {
	var include, exclude []string
	for _, p := range patterns {
		p = strings.TrimPrefix(path.Clean(strings.TrimSpace(strings.TrimPrefix(p, "./"))), "./")
		switch {
		case p == "" || p == "." || strings.HasPrefix(p, "/") || strings.Contains(p, ".."):
			continue
		case strings.HasPrefix(p, "!"):
			exclude = append(exclude, strings.TrimPrefix(p, "!"))
		default:
			include = append(include, p)
		}
	}
	var out []string
	for _, p := range include {
		for _, d := range globDirs(repo, strings.Split(p, "/"), "", 0) {
			skip := false
			for _, e := range exclude {
				if globMatch(e, d) {
					skip = true
					break
				}
			}
			if !skip {
				out = append(out, d)
			}
		}
		if len(out) >= maxGlobHits {
			break
		}
	}
	return out
}

// globDirs lists the folders under repo that match the segments ("*" one level, "**" any number).
func globDirs(repo string, segs []string, prefix string, depth int) []string {
	if len(segs) == 0 {
		return []string{prefix}
	}
	if depth > maxGlobDepth {
		return nil
	}
	seg, rest := segs[0], segs[1:]
	if seg != "*" && seg != "**" && !strings.ContainsAny(seg, "*?[") {
		next := path.Join(prefix, seg)
		if !isDir(repo, next) {
			return nil
		}
		return globDirs(repo, rest, next, depth+1)
	}
	entries, err := os.ReadDir(filepath.Join(repo, filepath.FromSlash(prefix)))
	if err != nil {
		return nil
	}
	var out []string
	if seg == "**" { // zero folders: keep matching the rest here
		out = append(out, globDirs(repo, rest, prefix, depth+1)...)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || skipDirs[name] || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		next := path.Join(prefix, name)
		switch {
		case seg == "**":
			out = append(out, globDirs(repo, segs, next, depth+1)...)
		default:
			if ok, _ := path.Match(seg, name); ok {
				out = append(out, globDirs(repo, rest, next, depth+1)...)
			}
		}
		if len(out) >= maxGlobHits {
			break
		}
	}
	return out
}

// globMatch reports whether folder d matches an exclusion pattern.
func globMatch(pattern, d string) bool {
	if ok, _ := path.Match(pattern, d); ok {
		return true
	}
	return strings.HasSuffix(pattern, "/**") && (d == strings.TrimSuffix(pattern, "/**") || strings.HasPrefix(d, strings.TrimSuffix(pattern, "/**")+"/"))
}

// scanManifests looks for project folders one and two levels down (apps/web, services/api,
// backend) for repositories that declare no workspaces.
func scanManifests(repo string) []string {
	var out []string
	has := func(rel string) bool {
		for _, m := range manifests {
			if exists(filepath.Join(repo, filepath.FromSlash(rel)), m) {
				return true
			}
		}
		return false
	}
	top, _ := os.ReadDir(repo)
	for _, e := range top {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || skipDirs[name] || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		if has(name) {
			out = append(out, name)
			continue
		}
		sub, _ := os.ReadDir(filepath.Join(repo, name))
		for _, s := range sub {
			if s.IsDir() && !strings.HasPrefix(s.Name(), ".") && !skipDirs[s.Name()] && s.Type()&os.ModeSymlink == 0 && has(name+"/"+s.Name()) {
				out = append(out, name+"/"+s.Name())
			}
		}
	}
	return out
}

// webFrameworks are dependencies that make a Node.js package an app rather than a library.
var webFrameworks = []string{"next", "nuxt", "vite", "express", "fastify", "koa", "hono", "@nestjs/core", "astro", "@sveltejs/kit", "react-scripts", "@hapi/hapi", "@remix-run/node", "@remix-run/serve", "nitropack", "gatsby", "@tanstack/start", "vinxi"}

// describe fills in a Candidate for folder d.
func describe(repo, d string) Candidate {
	dir := filepath.Join(repo, filepath.FromSlash(d))
	c := Candidate{Path: d, Deployable: true}
	if det := detectIn(dir, repo); det != nil {
		c.Kind = det.Kind
	}
	switch {
	case exists(dir, "package.json"):
		var pkg struct {
			Name            string            `json:"name"`
			Scripts         map[string]string `json:"scripts"`
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		_ = json.Unmarshal([]byte(readSmall(dir, "package.json")), &pkg)
		c.Name = pkg.Name
		app := pkg.Scripts["start"] != ""
		for _, f := range webFrameworks {
			if _, ok := pkg.Dependencies[f]; ok {
				app = true
			}
			if _, ok := pkg.DevDependencies[f]; ok {
				app = true
			}
		}
		c.Deployable = app
	case exists(dir, "go.mod"):
		if m := goModuleRe.FindStringSubmatch(readSmall(dir, "go.mod")); m != nil {
			c.Name = m[1]
		}
		c.Deployable = hasRootMain(dir) || isDir(dir, "cmd")
	case exists(dir, "Cargo.toml"):
		if m := cargoNameRe.FindStringSubmatch(readSmall(dir, "Cargo.toml")); m != nil {
			c.Name = m[1]
		}
		c.Deployable = exists(dir, "src/main.rs") || strings.Contains(readSmall(dir, "Cargo.toml"), "[[bin]]")
	}
	return c
}

var goModuleRe = regexp.MustCompile(`(?m)^module\s+(\S+)`)
