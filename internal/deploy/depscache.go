package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// depStep is an install command that can be skipped when nothing it reads has changed: the manifest
// and the lockfile are byte for byte the ones of the last successful build, and the folder it
// fills (node_modules, vendor) is still there.
type depStep struct {
	cmd      string   // the exact command, as the detection writes it
	manifest string   // file with the dependency list
	locks    []string // at least one must exist: without a lockfile "unchanged" proves nothing
	dir      string   // what the command installs, relative to the project
}

var depSteps = []depStep{
	{"npm ci", "package.json", []string{"package-lock.json", "npm-shrinkwrap.json"}, "node_modules"},
	{"npm install", "package.json", []string{"package-lock.json", "npm-shrinkwrap.json"}, "node_modules"},
	{"pnpm install --frozen-lockfile", "package.json", []string{"pnpm-lock.yaml"}, "node_modules"},
	{"yarn install --frozen-lockfile", "package.json", []string{"yarn.lock"}, "node_modules"},
	{"bun install", "package.json", []string{"bun.lockb", "bun.lock"}, "node_modules"},
	{"composer install --no-dev --optimize-autoloader", "composer.json", []string{"composer.lock"}, "vendor"},
}

// depsPlan is what to do with the install step of a build command.
type depsPlan struct {
	step *depStep // nil: the build does not start with a known install command
	rest string   // the build command without the install step
	hash string   // fingerprint of this install ("" when it cannot be computed)
	skip bool     // the last successful build had the same fingerprint and the folder exists
}

// splitInstall finds the known install command at the start of a build command. Only the literal
// command followed by "&&" (or alone) matches, so nothing the admin wrote is reinterpreted.
func splitInstall(build string) (*depStep, string) {
	build = strings.TrimSpace(build)
	for i := range depSteps {
		s := &depSteps[i]
		switch {
		case build == s.cmd:
			return s, ""
		case strings.HasPrefix(build, s.cmd+" && "):
			return s, strings.TrimSpace(strings.TrimPrefix(build, s.cmd+" && "))
		}
	}
	return nil, build
}

// depsHash fingerprints an install: the command, the manifest, the lockfiles and extra (what else
// changes the result: the Node.js version and the variables the package managers read). It is
// empty when no lockfile exists.
func depsHash(src string, s *depStep, extra string) string {
	h := sha256.New()
	fmt.Fprintf(h, "v1\x00%s\x00%s\x00", s.cmd, extra)
	locked := false
	for _, f := range append([]string{s.manifest}, s.locks...) {
		b, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			fmt.Fprintf(h, "%s\x00absent\x00", f)
			continue
		}
		if f != s.manifest {
			locked = true
		}
		fmt.Fprintf(h, "%s\x00%d\x00", f, len(b))
		h.Write(b)
	}
	if !locked {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// planDeps decides whether the install step of build can be skipped. saved is the fingerprint
// stored by the last successful build ("" if none).
func planDeps(build, src, extra, saved string) depsPlan {
	s, rest := splitInstall(build)
	p := depsPlan{step: s, rest: rest}
	if s == nil {
		return p
	}
	p.hash = depsHash(src, s, extra)
	if fi, err := os.Stat(filepath.Join(src, s.dir)); err == nil && fi.IsDir() && p.hash != "" && p.hash == saved {
		p.skip = true
	}
	return p
}

// depsExtra gathers what besides the files changes an install: the Node.js version and the
// variables that package managers read (NODE_ENV decides whether devDependencies are installed).
func depsExtra(nodeVersion string, env []string) string {
	var b strings.Builder
	b.WriteString("node=" + nodeVersion)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		u := strings.ToUpper(k)
		if u == "NODE_ENV" || strings.HasPrefix(u, "NPM_CONFIG_") || strings.HasPrefix(u, "YARN_") ||
			strings.HasPrefix(u, "PNPM_") || strings.HasPrefix(u, "COMPOSER_") || strings.HasPrefix(u, "BUN_") {
			b.WriteString("\x00" + kv)
		}
	}
	return b.String()
}

func (d *Deployer) depsFile(app string) string {
	return filepath.Join(d.Paths.AppDir(app), "deps.sha256")
}

func (d *Deployer) savedDeps(app string) string {
	b, _ := os.ReadFile(d.depsFile(app))
	return strings.TrimSpace(string(b))
}
