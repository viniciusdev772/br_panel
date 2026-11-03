package validate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	maxRootDirLen    = 200
	maxRootDirDepth  = 10
	maxWatchPatterns = 20
	maxWatchLen      = 200
)

var (
	rootSegRe  = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._-]*$`)
	watchPatRe = regexp.MustCompile(`^!?[A-Za-z0-9._*?/-]+$`)
)

// RootDir validates the folder of an app inside its repository (a monorepo) and returns it
// normalized: slash separated, no "./" in front and no trailing slash. "" and "." mean the whole
// repository. The value goes into shell commands (cd) and unit files, so only plain relative
// paths are accepted: no "..", no absolute path, no backslash, no ".git".
func RootDir(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	for strings.HasPrefix(s, "./") {
		s = strings.TrimPrefix(s, "./")
	}
	s = strings.TrimRight(s, "/")
	if s == "" || s == "." {
		return "", nil
	}
	if len(s) > maxRootDirLen {
		return "", fmt.Errorf("o diretório raiz é longo demais (máximo %d caracteres)", maxRootDirLen)
	}
	if strings.HasPrefix(s, "/") || strings.Contains(s, `\`) {
		return "", errors.New("o diretório raiz deve ser um caminho relativo à raiz do repositório, como apps/web")
	}
	segs := strings.Split(s, "/")
	if len(segs) > maxRootDirDepth {
		return "", fmt.Errorf("o diretório raiz tem pastas demais (máximo %d)", maxRootDirDepth)
	}
	for _, seg := range segs {
		switch {
		case seg == "" || seg == "." || seg == "..":
			return "", errors.New(`o diretório raiz não pode ter "..", "." nem barras duplas`)
		case !rootSegRe.MatchString(seg):
			return "", errors.New("o diretório raiz só aceita letras, números, ponto, hífen, sublinhado e barras")
		case strings.EqualFold(seg, ".git"):
			return "", errors.New("o diretório raiz não pode ser a pasta .git")
		case strings.HasPrefix(seg, "-"):
			return "", errors.New("os nomes de pasta do diretório raiz não podem começar com hífen")
		}
	}
	return s, nil
}

// WatchPaths validates the extra paths that trigger a deploy of a monorepo app: one pattern per
// line (commas also separate), relative to the repository root, with * (inside a folder), ** (any
// folders) and ?; a leading ! excludes what the pattern matches. They are evaluated in order and
// the last one that matches wins. It returns them normalized, one per line.
func WatchPaths(raw string) (string, error) {
	var out []string
	for _, p := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' }) {
		p = strings.TrimSpace(p)
		for strings.HasPrefix(p, "./") {
			p = strings.TrimPrefix(p, "./")
		}
		if p == "" {
			continue
		}
		body := strings.TrimPrefix(p, "!")
		switch {
		case len(p) > maxWatchLen:
			return "", fmt.Errorf("o caminho %.30q... é longo demais", p)
		case strings.HasPrefix(body, "/"):
			return "", fmt.Errorf("o caminho %q deve ser relativo à raiz do repositório (sem / no começo)", p)
		case body == "" || !watchPatRe.MatchString(p) || strings.HasPrefix(body, "!"):
			return "", fmt.Errorf("o caminho %q não é válido (use letras, números, . - _ / * ? ** e, no começo, ! para excluir)", p)
		}
		for _, seg := range strings.Split(body, "/") {
			if seg == ".." {
				return "", fmt.Errorf("o caminho %q não pode ter ..", p)
			}
		}
		out = append(out, p)
	}
	if len(out) > maxWatchPatterns {
		return "", fmt.Errorf("no máximo %d caminhos para acompanhar", maxWatchPatterns)
	}
	return strings.Join(out, "\n"), nil
}
