// Package nodeenv installs Node.js with nvm into the panel's data directory and links node,
// npm and friends into a directory on the default PATH, so builds (run as the app user)
// and systemd services find them without touching the distribution's packages.
package nodeenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// nvmVersion is a pinned release: the panel clones that tag instead of piping a script
// from the internet into a shell. Bump it deliberately.
const nvmVersion = "v0.40.8"

const installTimeout = 15 * time.Minute

// linked are the executables of the default Node.js version exposed on the PATH.
// pnpm and yarn are corepack shims, present once corepack is enabled.
var linked = []string{"node", "npm", "npx", "corepack", "pnpm", "pnpx", "yarn", "yarnpkg"}

// Installer installs Node.js with nvm. One install runs at a time; the zero value is not usable.
type Installer struct {
	Dir     string        // nvm home, e.g. <data>/nvm
	LinkDir string        // where node/npm/... are linked; /usr/local/bin in production
	User    func() string // unprivileged app user, to check it can run node; may be nil

	runMu sync.Mutex // serializes installs (button and deploy)

	mu      sync.Mutex // guards the fields below
	running bool
	err     string
	log     []byte
}

// New returns an Installer that keeps nvm in dir and links binaries into /usr/local/bin.
func New(dir string, user func() string) *Installer {
	return &Installer{Dir: dir, LinkDir: "/usr/local/bin", User: user}
}

// Available reports whether node and npm are both on the PATH.
func Available() bool {
	for _, b := range []string{"node", "npm"} {
		if _, err := exec.LookPath(b); err != nil {
			return false
		}
	}
	return true
}

// Start installs in the background; false if an install is already running.
func (i *Installer) Start() bool {
	i.mu.Lock()
	if i.running {
		i.mu.Unlock()
		return false
	}
	i.running, i.err, i.log = true, "", nil
	i.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
		defer cancel()
		i.runMu.Lock()
		err := i.install(ctx, (*stateLog)(i))
		i.runMu.Unlock()
		i.mu.Lock()
		i.running = false
		if err != nil {
			i.err = err.Error()
		}
		i.mu.Unlock()
	}()
	return true
}

// State reports the background install: whether it runs, why it failed and the log tail.
func (i *Installer) State() (running bool, errMsg, log string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.running, i.err, string(i.log)
}

// Ensure makes node and npm available, installing them when missing. It blocks, so a deploy
// can continue right after; output goes to log.
func (i *Installer) Ensure(ctx context.Context, log io.Writer) error {
	i.runMu.Lock()
	defer i.runMu.Unlock()
	if Available() {
		return nil
	}
	return i.install(ctx, log)
}

// stateLog appends to the installer's visible log, keeping the last 16 KiB.
type stateLog Installer

func (s *stateLog) Write(p []byte) (int, error) {
	i := (*Installer)(s)
	i.mu.Lock()
	defer i.mu.Unlock()
	i.log = append(i.log, p...)
	if len(i.log) > 16<<10 {
		i.log = i.log[len(i.log)-16<<10:]
	}
	return len(p), nil
}

func (i *Installer) install(ctx context.Context, log io.Writer) error {
	for bin, pkg := range map[string]string{"git": "git", "bash": "bash"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("falta o programa %s no servidor (apt install %s)", bin, pkg)
		}
	}
	_, errCurl := exec.LookPath("curl")
	_, errWget := exec.LookPath("wget")
	if errCurl != nil && errWget != nil {
		return errors.New("o nvm precisa de curl ou wget para baixar o Node.js (apt install curl)")
	}

	if _, err := os.Stat(filepath.Join(i.Dir, "nvm.sh")); err != nil {
		_ = os.RemoveAll(i.Dir) // leftovers of an interrupted clone
		fmt.Fprintf(log, "Baixando o nvm %s…\n", nvmVersion)
		if err := run(ctx, log, "", nil, "git", "clone", "--depth", "1", "--branch", nvmVersion, "--", "https://github.com/nvm-sh/nvm.git", i.Dir); err != nil {
			return fmt.Errorf("não consegui baixar o nvm: %w", err)
		}
	}

	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + i.Dir, "NVM_DIR=" + i.Dir, "LANG=C.UTF-8"}
	// The script is fixed text: nothing user-controlled reaches the shell.
	// corepack provides pnpm and yarn; recent Node.js versions no longer bundle it, so fall back to npm.
	const script = `. "$NVM_DIR/nvm.sh" && nvm install --lts && nvm alias default 'lts/*' && { corepack enable >/dev/null 2>&1 || { npm install -g corepack >/dev/null 2>&1 && corepack enable >/dev/null 2>&1; } || true; }`
	fmt.Fprintln(log, "Instalando o Node.js LTS com o nvm (pode levar alguns minutos)…")
	if err := run(ctx, log, i.Dir, env, "bash", "-c", script); err != nil {
		return fmt.Errorf("o nvm não conseguiu instalar o Node.js: %w", err)
	}
	var out bytes.Buffer
	if err := run(ctx, &out, i.Dir, env, "bash", "-c", `. "$NVM_DIR/nvm.sh" >/dev/null && dirname "$(nvm which default)"`); err != nil {
		return fmt.Errorf("não achei o Node.js instalado: %w", err)
	}
	binDir := strings.TrimSpace(lastLine(out.String()))
	if !strings.HasPrefix(binDir, i.Dir) {
		return fmt.Errorf("caminho inesperado para o Node.js: %q", binDir)
	}

	// Apps run as an unprivileged user and must be able to read the whole tree.
	if err := run(ctx, log, "", nil, "chmod", "-R", "go+rX", i.Dir); err != nil {
		return err
	}
	if err := i.link(log, binDir); err != nil {
		return err
	}
	return i.verify(ctx, log)
}

// link exposes the binaries of binDir in LinkDir. It replaces its own earlier links
// (after a Node.js upgrade) and never overwrites a real file such as a distro package.
func (i *Installer) link(log io.Writer, binDir string) error {
	if err := os.MkdirAll(i.LinkDir, 0o755); err != nil {
		return err
	}
	for _, name := range linked {
		src := filepath.Join(binDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(i.LinkDir, name)
		if fi, err := os.Lstat(dst); err == nil {
			target, _ := os.Readlink(dst)
			if fi.Mode()&os.ModeSymlink == 0 || !strings.HasPrefix(target, i.Dir) {
				fmt.Fprintf(log, "%s já existe e não é do nvm; mantido como está.\n", dst)
				continue
			}
			if err := os.Remove(dst); err != nil {
				return err
			}
		}
		if err := os.Symlink(src, dst); err != nil {
			return err
		}
		fmt.Fprintf(log, "%s -> %s\n", dst, src)
	}
	return nil
}

// verify runs node and npm the way the apps will: by absolute link and as the app user.
func (i *Installer) verify(ctx context.Context, log io.Writer) error {
	user := ""
	if i.User != nil {
		user = i.User()
	}
	for _, name := range []string{"node", "npm"} {
		bin := filepath.Join(i.LinkDir, name)
		prog, args := bin, []string{"--version"}
		if user != "" {
			prog, args = "runuser", []string{"-u", user, "--", bin, "--version"}
		}
		var out bytes.Buffer
		env := []string{"PATH=" + i.LinkDir + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + os.TempDir()}
		if err := run(ctx, &out, "", env, prog, args...); err != nil {
			return fmt.Errorf("%s instalado, mas não executa%s: %w (%s)", name, forUser(user), err, lastLine(out.String()))
		}
		fmt.Fprintf(log, "%s %s\n", name, strings.TrimSpace(lastLine(out.String())))
	}
	fmt.Fprintln(log, "Node.js pronto.")
	return nil
}

func forUser(u string) string {
	if u == "" {
		return ""
	}
	return " como " + u
}

// run executes name with fixed arguments (never through a shell string built from input).
func run(ctx context.Context, log io.Writer, dir string, env []string, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir, c.Stdout, c.Stderr = dir, log, log
	if env != nil {
		c.Env = env
	}
	return c.Run()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
