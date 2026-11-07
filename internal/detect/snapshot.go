package detect

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	snapshotMaxEntries = 220     // lines of the file tree
	snapshotMaxDepth   = 3       // folders below the project root that the tree shows
	snapshotFileCap    = 6 << 10 // bytes read from one key file
	snapshotTotalCap   = 48 << 10
)

// snapshotSkipDirs are folders that hold dependencies or build output: they only add noise to the tree.
var snapshotSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, ".venv": true, "venv": true,
	"__pycache__": true, "target": true, ".next": true, ".nuxt": true, ".cache": true, ".idea": true, ".vscode": true,
	"coverage": true, "bower_components": true, ".gradle": true, ".terraform": true,
}

// keyFiles are the files that say how a project builds and starts. Only these are ever read for a
// snapshot, so a secret (a .env, a private key) never reaches the model; .env.example is a
// template, not a secret.
var keyFiles = map[string]bool{
	"package.json": true, "requirements.txt": true, "pyproject.toml": true, "pipfile": true, "procfile": true,
	"runtime.txt": true, "go.mod": true, "cargo.toml": true, "composer.json": true, "gemfile": true, "pom.xml": true,
	"build.gradle": true, "build.gradle.kts": true, "makefile": true, "dockerfile": true, "docker-compose.yml": true,
	"docker-compose.yaml": true, "readme.md": true, "readme": true, ".nvmrc": true, ".python-version": true,
	".tool-versions": true, "deno.json": true, "bunfig.toml": true, "manage.py": true, "wsgi.py": true, "asgi.py": true,
	"main.py": true, "app.py": true, "server.js": true, "index.js": true, "main.go": true, "artisan": true, "config.ru": true,
	"next.config.js": true, "next.config.mjs": true, "next.config.ts": true, "vite.config.js": true, "vite.config.ts": true,
	"nuxt.config.ts": true, "nuxt.config.js": true, "astro.config.mjs": true, "svelte.config.js": true, "angular.json": true,
	".env.example": true, ".env.sample": true, ".env.template": true,
}

// Tree lists the files of dir (sorted, folders with a trailing slash), skipping dependency and
// build folders and every symbolic link: a repository is not trusted, and the listing must not
// leave dir. It stops after maxEntries lines and descends maxDepth levels. keys are the paths of the
// key files (see keyFiles) found within the first two levels.
func Tree(dir string, maxEntries, maxDepth int) (lines, keys []string, truncated bool) {
	var walk func(rel string, depth int)
	walk = func(rel string, depth int) {
		entries, err := os.ReadDir(filepath.Join(dir, rel))
		if err != nil {
			return
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if len(lines) >= maxEntries {
				truncated = true
				return
			}
			if e.Type()&fs.ModeSymlink != 0 {
				continue
			}
			p := filepath.ToSlash(filepath.Join(rel, e.Name()))
			if e.IsDir() {
				if snapshotSkipDirs[e.Name()] {
					continue
				}
				lines = append(lines, p+"/")
				if depth < maxDepth {
					walk(filepath.Join(rel, e.Name()), depth+1)
				}
				continue
			}
			lines = append(lines, p)
			if depth <= 1 && keyFiles[strings.ToLower(e.Name())] {
				keys = append(keys, p)
			}
		}
	}
	walk("", 0)
	return lines, keys, truncated
}

// Snapshot is a text picture of a project for a language model: the file tree and the content of
// the key files that decide how it runs, within fixed size limits.
func Snapshot(dir string) string {
	lines, keys, cut := Tree(dir, snapshotMaxEntries, snapshotMaxDepth)
	var b strings.Builder
	b.WriteString("## Árvore de arquivos\n" + strings.Join(lines, "\n") + "\n")
	if cut {
		b.WriteString("… (lista cortada)\n")
	}
	total := 0
	for _, p := range keys {
		if total >= snapshotTotalCap {
			b.WriteString("\n(demais arquivos omitidos por tamanho)\n")
			break
		}
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		buf := make([]byte, snapshotFileCap+1)
		m, _ := f.Read(buf)
		_ = f.Close()
		text := strings.ToValidUTF8(string(buf[:m]), "")
		cutNote := ""
		if m > snapshotFileCap {
			text, cutNote = text[:snapshotFileCap], "\n… (arquivo cortado)"
		}
		total += len(text)
		b.WriteString("\n## " + p + "\n" + text + cutNote + "\n")
	}
	return b.String()
}
