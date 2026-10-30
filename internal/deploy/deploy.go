// Package deploy turns an app definition into a running service: clone, detect, build,
// systemd unit, Nginx/SSL. It also owns the on-disk layout of an app.
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/viniciusdev772/br_panel/internal/cloudflare"
	"github.com/viniciusdev772/br_panel/internal/dbengine"
	"github.com/viniciusdev772/br_panel/internal/execx"
	"github.com/viniciusdev772/br_panel/internal/githubapp"
	"github.com/viniciusdev772/br_panel/internal/nginx"
	"github.com/viniciusdev772/br_panel/internal/settings"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

const deployTimeout = 20 * time.Minute

// Paths is the on-disk layout of the data directory.
type Paths struct{ Data string }

func (p Paths) AppDir(n string) string  { return filepath.Join(p.Data, "apps", n) }
func (p Paths) SrcDir(n string) string  { return filepath.Join(p.AppDir(n), "src") }
func (p Paths) LogFile(n string) string { return filepath.Join(p.Data, "logs", n+".log") }
func (p Paths) EnvFile(n string) string { return filepath.Join(p.Data, "env", n+".env") }

// Deployer deploys apps. Settings and GitHub are functions so that changes made in the UI apply immediately.
type Deployer struct {
	Store    *store.Store
	Paths    Paths
	DataDir  string
	Settings func() settings.Settings
	GitHub   func() *githubapp.Client

	pendMu  sync.Mutex
	pending map[string]bool
}

// New returns a Deployer rooted at dataDir.
func New(st *store.Store, dataDir string, set func() settings.Settings, gh func() *githubapp.Client) *Deployer {
	return &Deployer{Store: st, Paths: Paths{Data: dataDir}, DataDir: dataDir, Settings: set, GitHub: gh, pending: map[string]bool{}}
}

func (d *Deployer) unitFile(app string) string {
	return filepath.Join(d.Settings().UnitDir, systemd.UnitName(app))
}

func (d *Deployer) nginx() *nginx.Manager { return nginx.FromSettings(d.Settings()) }

// MarkPending asks for one more deploy once the current one finishes.
func (d *Deployer) MarkPending(name string) {
	d.pendMu.Lock()
	d.pending[name] = true
	d.pendMu.Unlock()
}

func (d *Deployer) takePending(name string) bool {
	d.pendMu.Lock()
	defer d.pendMu.Unlock()
	p := d.pending[name]
	delete(d.pending, name)
	return p
}

// LastLines returns the last n lines of s.
func LastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}

// command builds an exec.Cmd, optionally dropping privileges to the app user.
// Every argument is passed separately; nothing is interpreted by a shell
// unless the caller explicitly asks for `sh -c`.
func (d *Deployer) command(ctx context.Context, dir string, env []string, name string, args ...string) *exec.Cmd {
	if d.Settings().AppUser != "" {
		args = append([]string{"-u", d.Settings().AppUser, "--", name}, args...)
		name = "runuser"
	}
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Env = env
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 5 * time.Second
	return c
}

func (d *Deployer) run(ctx context.Context, log io.Writer, dir string, env []string, name string, args ...string) error {
	fmt.Fprintf(log, "$ %s %s\n", name, strings.Join(args, " "))
	c := d.command(ctx, dir, env, name, args...)
	c.Stdout, c.Stderr = log, log
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func (d *Deployer) capture(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	var out bytes.Buffer
	c := d.command(ctx, dir, env, name, args...)
	c.Stdout = &out
	err := c.Run()
	return strings.TrimSpace(out.String()), err
}

// envPairs parses the app's variables and resolves ${{database.VAR}} references.
func (d *Deployer) envPairs(a store.App) ([][2]string, error) {
	kv, err := validate.ParseEnv(a.Env)
	if err != nil {
		return nil, err
	}
	dbs := d.Store.ListDatabases()
	for i := range kv {
		kv[i][1], _ = dbengine.Resolve(kv[i][1], dbs)
	}
	return kv, nil
}

func (d *Deployer) buildEnv(a store.App) ([]string, error) {
	kv, err := d.envPairs(a)
	if err != nil {
		return nil, err
	}
	env := []string{
		"PATH=" + execPath(),
		"HOME=" + d.Paths.AppDir(a.Name),
		"LANG=C.UTF-8",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=https:ssh",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new",
	}
	if a.Port > 0 {
		env = append(env, fmt.Sprintf("PORT=%d", a.Port))
	}
	env = append(env, "HOST="+ListenHost(a))
	for _, p := range kv {
		env = append(env, p[0]+"="+p[1])
	}
	return env, nil
}

// Start launches a deploy in the background; false if one is already running.
func (d *Deployer) Start(name, reason string) bool {
	a, ok := d.Store.BeginDeploy(name)
	if !ok {
		return false
	}
	go func() {
		// A push that arrived during this deploy triggers one more run afterwards.
		defer func() {
			if d.takePending(name) {
				d.Start(name, "push recebido durante o deploy anterior")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), deployTimeout)
		defer cancel()
		lf, err := os.OpenFile(d.Paths.LogFile(name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			d.Store.FinishDeploy(name, false)
			return
		}
		defer lf.Close()
		fmt.Fprintf(lf, "== deploy de %s iniciado em %s (%s) ==\n", name, time.Now().Format(time.RFC3339), reason)
		d.Store.AddEvent("deploy", name, "Deploy iniciado ("+reason+")")
		if err := d.deploy(ctx, a, lf); err != nil {
			fmt.Fprintf(lf, "\n== FALHOU: %v ==\n", err)
			d.Store.FinishDeploy(name, false)
			d.Store.AddEvent("deploy", name, "Deploy falhou")
			return
		}
		fmt.Fprintf(lf, "\n== deploy concluído ==\n")
		d.Store.FinishDeploy(name, true)
		d.Store.AddEvent("deploy", name, "Deploy concluído")
	}()
	return true
}

func (d *Deployer) deploy(ctx context.Context, a store.App, log io.Writer) error {
	env, err := d.buildEnv(a)
	if err != nil {
		return err
	}
	appDir, src := d.Paths.AppDir(a.Name), d.Paths.SrcDir(a.Name)
	gitEnv := githubapp.AuthEnv(ctx, d.GitHub(), a.Repo, env, log)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	if d.Settings().AppUser != "" {
		if err := d.run(ctx, log, "/", env, "chown", d.Settings().AppUser+":", appDir); err != nil {
			return err
		}
	}

	// Reuse the existing checkout only if it points at the same repository.
	if _, err := os.Stat(filepath.Join(src, ".git")); err == nil {
		if url, _ := d.capture(ctx, src, gitEnv, "git", "config", "--get", "remote.origin.url"); url != a.Repo {
			fmt.Fprintln(log, "repositório mudou; clonando novamente")
			if err := os.RemoveAll(src); err != nil {
				return err
			}
		}
	} else if err := os.RemoveAll(src); err != nil {
		return err
	}

	if _, err := os.Stat(src); os.IsNotExist(err) {
		if err := d.run(ctx, log, appDir, gitEnv, "git", "clone", "--depth", "1", "--branch", a.Branch, "--", a.Repo, src); err != nil {
			return err
		}
	} else {
		if err := d.run(ctx, log, src, gitEnv, "git", "fetch", "--depth", "1", "origin", a.Branch); err != nil {
			return err
		}
		if err := d.run(ctx, log, src, gitEnv, "git", "reset", "--hard", "FETCH_HEAD"); err != nil {
			return err
		}
	}

	var err2 error
	if a, err2 = d.autoConfigure(ctx, a, src, log); err2 != nil {
		return err2
	}
	if env, err = d.buildEnv(a); err != nil {
		return err
	}

	if a.BuildCmd != "" {
		if err := d.run(ctx, log, src, env, "/bin/sh", "-c", a.BuildCmd); err != nil {
			return err
		}
	}

	// Service definition.
	if err := writeFile(d.Paths.EnvFile(a.Name), d.envFileContent(a), 0o600); err != nil {
		return err
	}
	unit := systemd.Unit{App: a.Name, WorkDir: d.Paths.SrcDir(a.Name), EnvFile: d.Paths.EnvFile(a.Name), StartCmd: a.StartCmd, User: d.Settings().AppUser}.Render()
	if err := writeFile(d.unitFile(a.Name), unit, 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", systemd.UnitName(a.Name)}, {"restart", systemd.UnitName(a.Name)}} {
		if err := execx.Run(ctx, log, "systemctl", args...); err != nil {
			return fmt.Errorf("systemctl %s: %w", args[0], err)
		}
	}

	if a.Domain == "" {
		return d.nginx().Remove(ctx, log, a.Name)
	}
	d.ensureDNS(ctx, log, a.Domain)
	return d.nginx().Ensure(ctx, log, nginx.Site{Key: a.Name, Domain: a.Domain, Upstream: fmt.Sprintf("127.0.0.1:%d", a.Port)})
}

// ensureDNS points the domain at this server through Cloudflare, before certbot runs
// (the HTTP-01 challenge needs the record). Failures only warn: DNS may be managed elsewhere.
func (d *Deployer) ensureDNS(ctx context.Context, log io.Writer, domain string) {
	cf := cloudflare.New(d.Settings().CloudflareEmail, d.Settings().CloudflareToken)
	if cf == nil {
		return
	}
	ip, res, err := cf.EnsureServerA(ctx, domain)
	if err == nil {
		fmt.Fprintf(log, "DNS na Cloudflare: %s -> %s (%s)\n", domain, ip, res)
		return
	}
	fmt.Fprintf(log, "AVISO: não foi possível ajustar o DNS na Cloudflare (%v). Aponte %s para este servidor manualmente.\n", err, domain)
}

func (d *Deployer) envFileContent(a store.App) string {
	var b strings.Builder
	if a.Port > 0 {
		b.WriteString(systemd.EnvFileLine("PORT", fmt.Sprint(a.Port)))
	}
	b.WriteString(systemd.EnvFileLine("HOST", ListenHost(a)))
	kv, _ := d.envPairs(a) // validated on save
	for _, p := range kv {
		b.WriteString(systemd.EnvFileLine(p[0], p[1]))
	}
	return b.String()
}

func writeFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

// Remove stops the service and deletes the app, its files, unit and vhost.
func (d *Deployer) Remove(name string) error {
	unit := systemd.UnitName(name)
	_, _ = systemd.Ctl("disable", "--now", unit)
	for _, p := range []string{d.unitFile(name), d.Paths.EnvFile(name), d.Paths.LogFile(name)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_, _ = systemd.Ctl("daemon-reload")
	_ = d.nginx().Remove(context.Background(), io.Discard, name)
	if err := os.RemoveAll(d.Paths.AppDir(name)); err != nil {
		return err
	}
	return d.Store.Delete(name)
}

// TailFile returns up to max trailing bytes of a file ("" if missing).
func TailFile(path string, max int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > max {
		_, _ = f.Seek(-max, io.SeekEnd)
	}
	b, _ := io.ReadAll(f)
	return string(b)
}

// execPath is the PATH given to builds and git: the standard directories plus whatever the panel
// itself was started with, so that "program found by the readiness check" means "program runnable".
func execPath() string {
	seen := map[string]bool{}
	var parts []string
	for _, p := range append(strings.Split("/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", ":"), filepath.SplitList(os.Getenv("PATH"))...) {
		if p != "" && filepath.IsAbs(p) && !seen[p] {
			seen[p] = true
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ":")
}
