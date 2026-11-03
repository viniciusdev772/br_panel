package detect

import (
	"path/filepath"
	"sort"
	"strings"
)

// DetectApp inspects the app that lives in rootDir (slash separated, relative to repo; "" = the
// whole repository) and suggests its commands. The build runs with rootDir as the working
// directory and so does the service, so a monorepo app only needs commands that work from there.
func DetectApp(repo, rootDir string) *Detection {
	if rootDir == "" {
		return Detect(repo)
	}
	return detectIn(filepath.Join(repo, filepath.FromSlash(rootDir)), repo)
}

// Layout says whether a repository holds more than one project, and which.
type Layout struct {
	Monorepo bool        `json:"monorepo"`
	Apps     []Candidate `json:"apps,omitempty"`
}

// Classify tells a single project from a monorepo. Besides the declared workspaces (package.json,
// pnpm, go.work, Cargo) it recognises a project at the root that also holds projects of their own
// in sub-folders (api at the root plus web/, video/, android/): the root is an app, and each of
// those folders can be published as another app with its own "pasta do projeto".
func Classify(repo string) Layout {
	if apps := Workspaces(repo); len(apps) > 0 {
		return Layout{Monorepo: true, Apps: apps}
	}
	if !rootIsProject(repo) {
		return Layout{}
	}
	var subs []Candidate
	for _, d := range scanManifests(repo) {
		if !plainDir(repo, d) || !hasProject(filepath.Join(repo, filepath.FromSlash(d))) {
			continue
		}
		aux := false
		for _, seg := range strings.Split(d, "/") {
			aux = aux || auxDirs[seg]
		}
		if !aux {
			subs = append(subs, describe(repo, d))
		}
	}
	sort.SliceStable(subs, func(i, j int) bool {
		if subs[i].Deployable != subs[j].Deployable {
			return subs[i].Deployable
		}
		return subs[i].Path < subs[j].Path
	})
	if len(subs) > maxCandidates {
		subs = subs[:maxCandidates]
	}
	return Layout{Monorepo: len(subs) > 0, Apps: subs}
}
