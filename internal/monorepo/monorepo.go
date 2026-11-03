// Package monorepo decides which apps of a monorepo a push affects. Several panel apps can point
// at one repository, each with its own folder (RootDir); a push only redeploys the ones whose
// files changed. It only knows paths, not Git or the panel.
package monorepo

import "strings"

// sharedRootFiles are the repository-level files every app of a monorepo builds on: a change to
// one of them redeploys all apps. Other root files (README, LICENSE, CI configuration) do not.
var sharedRootFiles = map[string]bool{
	"package.json": true, "package-lock.json": true, "npm-shrinkwrap.json": true, "pnpm-lock.yaml": true,
	"pnpm-workspace.yaml": true, "yarn.lock": true, "bun.lock": true, "bun.lockb": true, ".yarnrc.yml": true,
	".yarnrc": true, ".npmrc": true, ".pnpmfile.cjs": true, ".nvmrc": true, ".node-version": true, ".tool-versions": true,
	"turbo.json": true, "nx.json": true, "lerna.json": true, "go.work": true, "go.work.sum": true, "go.mod": true, "go.sum": true,
	"cargo.toml": true, "cargo.lock": true, "rust-toolchain": true, "rust-toolchain.toml": true,
	"pyproject.toml": true, "uv.lock": true, "poetry.lock": true, "pipfile": true, "pipfile.lock": true,
	"composer.json": true, "composer.lock": true, "gemfile": true, "gemfile.lock": true, "brpanel.json": true,
	"bunfig.toml": true, "deno.json": true, "deno.lock": true, ".pnp.cjs": true,
}

func isSharedRootFile(p string) bool {
	// Yarn releases and plugins, and pnpm patches, are applied to every app of the workspace.
	if strings.HasPrefix(p, ".yarn/") || strings.HasPrefix(p, "patches/") {
		return true
	}
	if strings.Contains(p, "/") {
		return false
	}
	l := strings.ToLower(p)
	return sharedRootFiles[l] || (strings.HasPrefix(l, "tsconfig") && strings.HasSuffix(l, ".json")) ||
		(strings.HasPrefix(l, "requirements") && strings.HasSuffix(l, ".txt"))
}

// Affected reports whether a push that changed the given repository paths (slash separated,
// relative to the root) must redeploy an app with the given root folder and extra watch patterns.
//
//   - No root folder and no patterns: the app is the whole repository; every push counts.
//   - A root folder: files inside it count by default, and so do the shared root files (lockfile,
//     workspace and toolchain configuration).
//   - Watch patterns are evaluated in order on top of that and the last one that matches a path
//     wins: "packages/ui/**" adds a sibling package the app uses, "!**/*.md" removes documentation
//     (the same model Coolify uses for its watch paths).
//   - Only patterns (no root folder): nothing counts by default; the patterns decide.
func Affected(rootDir string, watch []string, changed []string) bool {
	if rootDir == "" && len(watch) == 0 {
		return true
	}
	// With no folder, patterns that only exclude ("!docs/**") mean "everything except": starting
	// from nothing they could never match, and the app would silently never deploy.
	everything := rootDir == "" && !hasPositive(watch)
	for _, p := range changed {
		p = strings.TrimPrefix(p, "./")
		hit := everything || (rootDir != "" && (p == rootDir || strings.HasPrefix(p, rootDir+"/") || isSharedRootFile(p)))
		for _, w := range watch {
			if neg := strings.HasPrefix(w, "!"); neg {
				if Match(strings.TrimPrefix(w, "!"), p) {
					hit = false
				}
			} else if Match(w, p) {
				hit = true
			}
		}
		if hit {
			return true
		}
	}
	return false
}

func hasPositive(watch []string) bool {
	for _, w := range watch {
		if !strings.HasPrefix(w, "!") {
			return true
		}
	}
	return false
}

// Match reports whether path matches pattern: "*" and "?" stay inside one folder level, "**" spans
// any number of folders (including none), and a pattern naming a folder matches everything under
// it ("packages/ui" is "packages/ui/**").
func Match(pattern, path string) bool {
	pat := strings.Split(strings.Trim(pattern, "/"), "/")
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if pat[len(pat)-1] != "**" {
		// "packages/ui" must also cover "packages/ui/src/a.ts"
		if matchSegs(pat, segs) {
			return true
		}
		pat = append(pat[:len(pat):len(pat)], "**")
	}
	return matchSegs(pat, segs)
}

// matchSegs matches the pattern segments against the path segments. It is a dynamic program over
// (pattern position, path position): paths come from a hostile repository, and a naive recursion
// with several "**" is exponential in the path depth.
func matchSegs(pat, segs []string) bool {
	// ok[i][j]: pat[i:] matches segs[j:]
	ok := make([][]bool, len(pat)+1)
	for i := range ok {
		ok[i] = make([]bool, len(segs)+1)
	}
	ok[len(pat)][len(segs)] = true
	for i := len(pat) - 1; i >= 0; i-- {
		for j := len(segs); j >= 0; j-- {
			switch {
			case pat[i] == "**":
				ok[i][j] = ok[i+1][j] || (j < len(segs) && ok[i][j+1])
			case j < len(segs):
				ok[i][j] = matchSeg(pat[i], segs[j]) && ok[i+1][j+1]
			}
		}
	}
	return ok[0][0]
}

// matchSeg matches one path segment against a pattern with * and ?.
func matchSeg(p, s string) bool {
	pr, sr := []rune(p), []rune(s)
	var star, mark = -1, 0
	i, j := 0, 0
	for j < len(sr) {
		switch {
		case i < len(pr) && (pr[i] == '?' || pr[i] == sr[j]):
			i++
			j++
		case i < len(pr) && pr[i] == '*':
			star, mark = i, j
			i++
		case star >= 0:
			i = star + 1
			mark++
			j = mark
		default:
			return false
		}
	}
	for i < len(pr) && pr[i] == '*' {
		i++
	}
	return i == len(pr)
}

// Commit is the file lists of one commit of a push event.
type Commit struct{ Added, Modified, Removed []string }

// maxTrustedCommits: GitHub has documented the number of commits listed in a push payload as 20
// and, more recently, as 2048. Not knowing which one a delivery follows, the smaller number is the
// safe one: a push that reaches it may have been cut, so its file lists cannot prove that an app
// is unaffected, and every app deploys. A push with that many commits is rare.
const maxTrustedCommits = 20

// ChangedPaths flattens the commits of a push into the changed paths. complete is false when the
// list cannot prove which files changed: no commits (a branch created or moved without new
// commits), commits without any file (empty commits only), or a payload as large as GitHub's
// limit. The caller must then deploy every app: skipping a deploy by mistake is worse than an
// extra one. Merge commits list no files of their own, but the commits they bring are in the
// payload too, so the union stays right.
func ChangedPaths(commits []Commit) (paths []string, complete bool) {
	if len(commits) == 0 || len(commits) >= maxTrustedCommits {
		return nil, false
	}
	seen := map[string]bool{}
	for _, c := range commits {
		for _, list := range [][]string{c.Added, c.Modified, c.Removed} {
			for _, p := range list {
				if !seen[p] {
					seen[p] = true
					paths = append(paths, p)
				}
			}
		}
	}
	return paths, len(paths) > 0
}
