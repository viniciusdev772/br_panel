package detect

import "path/filepath"

// DetectApp inspects the app that lives in rootDir (slash separated, relative to repo; "" = the
// whole repository) and suggests its commands. The build runs with rootDir as the working
// directory and so does the service, so a monorepo app only needs commands that work from there.
func DetectApp(repo, rootDir string) *Detection {
	if rootDir == "" {
		return Detect(repo)
	}
	return detectIn(filepath.Join(repo, filepath.FromSlash(rootDir)), repo)
}
