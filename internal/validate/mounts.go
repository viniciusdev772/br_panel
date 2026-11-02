package validate

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Limits of the storage tab of an app.
const (
	MaxMounts    = 20
	MaxFileBytes = 256 << 10 // content of a "file" mount, kept in the database
)

var (
	mountNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	// mountPathRe is deliberately narrow: no spaces, ":" or "%" (they split or expand inside
	// BindPaths=), no "$", quotes or shell characters.
	mountPathRe = regexp.MustCompile(`^[A-Za-z0-9._/@+-]+$`)
)

// deniedRoots can be neither the source nor the target of a mount.
var deniedRoots = []string{"/etc", "/proc", "/sys", "/dev", "/boot", "/root", "/run"}

// deniedTargetRoots are also refused as targets: the service sees them read-only anyway.
var deniedTargetRoots = []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64"}

// MountName checks the name of a mount (also used in the VOLUME_<NAME> variable).
func MountName(n string) error {
	if !mountNameRe.MatchString(n) {
		return errors.New("nome do armazenamento inválido: use a-z, 0-9 e -, começando com letra (até 32 caracteres)")
	}
	return nil
}

// MountEnvName is the environment variable that holds the real path of mount name.
func MountEnvName(name string) string {
	return "VOLUME_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

func under(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }

// DeniedPath reports whether the absolute, cleaned path p is the panel's data directory, or inside
// it, or inside one of the system directories a mount may never touch ("/" included).
func DeniedPath(p, dataDir string) bool {
	if p == "/" {
		return true
	}
	for _, d := range deniedRoots {
		if under(p, d) {
			return true
		}
	}
	dataDir = path.Clean(strings.ReplaceAll(dataDir, `\`, "/"))
	return dataDir != "." && dataDir != "/" && under(p, dataDir)
}

// cleanPath checks the characters and the shape of a path: already clean, no ".." segment.
func cleanPath(label, p string) error {
	if p == "" || len(p) > 200 {
		return fmt.Errorf("%s: informe um caminho de até 200 caracteres", label)
	}
	if !mountPathRe.MatchString(p) {
		return fmt.Errorf("%s: use só letras, números e . _ / @ + - (sem espaços)", label)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("%s: não pode ter \"..\"", label)
		}
	}
	if path.Clean(p) != p || p == "." {
		return fmt.Errorf("%s: escreva o caminho sem \"./\", \"//\" ou barra no fim", label)
	}
	// systemd reads a leading "-" as "ignore this path when it is missing", and the panel must
	// never silently skip a mount the admin asked for.
	if strings.HasPrefix(p, "-") {
		return fmt.Errorf("%s: não pode começar com \"-\"", label)
	}
	return nil
}

// MountTarget checks where a mount appears inside the service: relative to the project directory,
// or absolute and outside the protected directories.
func MountTarget(target, dataDir string) error {
	if err := cleanPath("destino", target); err != nil {
		return err
	}
	if !strings.HasPrefix(target, "/") {
		return nil
	}
	if DeniedPath(target, dataDir) {
		return errors.New("destino: esse diretório do servidor é protegido")
	}
	for _, d := range deniedTargetRoots {
		if under(target, d) {
			return errors.New("destino: esse diretório do servidor é protegido")
		}
	}
	return nil
}

// Mount validates one storage entry. kind is "volume", "bind" or "file"; source is used by bind
// only and content by file only.
func Mount(kind, name, source, target, content, dataDir string) error {
	if err := MountName(name); err != nil {
		return err
	}
	if err := MountTarget(target, dataDir); err != nil {
		return err
	}
	switch kind {
	case "volume":
		if source != "" || content != "" {
			return errors.New("um volume não tem origem nem conteúdo")
		}
	case "bind":
		if content != "" {
			return errors.New("uma pasta do servidor não tem conteúdo")
		}
		if !strings.HasPrefix(source, "/") {
			return errors.New("origem: use um caminho absoluto do servidor, como /srv/dados")
		}
		if err := cleanPath("origem", source); err != nil {
			return err
		}
		if DeniedPath(source, dataDir) {
			return errors.New("origem: esse diretório do servidor é protegido")
		}
	case "file":
		if source != "" {
			return errors.New("um arquivo não tem origem")
		}
		if len(content) > MaxFileBytes {
			return fmt.Errorf("o arquivo passa de %d KB", MaxFileBytes>>10)
		}
		if strings.ContainsRune(content, 0) {
			return errors.New("o arquivo só pode ter texto")
		}
	default:
		return errors.New("tipo de armazenamento inválido")
	}
	return nil
}
