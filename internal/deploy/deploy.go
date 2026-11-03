// Package deploy turns an app definition into a running service: clone, detect, build,
// systemd unit, Nginx/SSL. It also owns the on-disk layout of an app.
package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/viniciusdev772/br_panel/internal/cloudflare"
	"github.com/viniciusdev772/br_panel/internal/dbengine"
	"github.com/viniciusdev772/br_panel/internal/execx"
	"github.com/viniciusdev772/br_panel/internal/githubapp"
	"github.com/viniciusdev772/br_panel/internal/nginx"
	"github.com/viniciusdev772/br_panel/internal/nodeenv"
	"github.com/viniciusdev772/br_panel/internal/settings"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
	"github.com/viniciusdev772/br_panel/internal/templates"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

const deployTimeout = 20 * time.Minute

// Paths is the on-disk layout of the data directory.
type Paths struct{ Data string }

func (p Paths) AppDir(n string) string  { return filepath.Join(p.Data, "apps", n) }
func (p Paths) SrcDir(n string) string  { return filepath.Join(p.AppDir(n), "src") }
func (p Paths) LogFile(n string) string { return filepath.Join(p.Data, "logs", n+".log") }
func (p Paths) EnvFile(n string) string { return filepath.Join(p.Data, "env", n+".env") }

// VolumesDir holds the volume mounts of an app; each subfolder is one volume, owned by the app
// user and shared by every instance.
func (p Paths) VolumesDir(n string) string { return filepath.Join(p.Data, "volumes", n) }

// VolumeDir is the folder of one volume mount of an app.
func (p Paths) VolumeDir(n, name string) string { return filepath.Join(p.VolumesDir(n), name) }

// FilesDir holds the contents of the "file" mounts of an app, one file per mount id.
func (p Paths) FilesDir(n string) string { return filepath.Join(p.Data, "files", n) }

// FileMount is the file written for a "file" mount, mounted read-only into the service.
func (p Paths) FileMount(n string, id int64) string {
	return filepath.Join(p.FilesDir(n), strconv.FormatInt(id, 10))
}

// VolumeBackupDir keeps the .tar.gz backups of an app's volumes, outside them (a backup inside a
// volume would copy the previous backups too).
func (p Paths) VolumeBackupDir(n string) string {
	return filepath.Join(p.Data, "backups", "volumes", n)
}

// VolumeLogFile is the log of the volume backups and restores of an app.
func (p Paths) VolumeLogFile(n string) string {
	return filepath.Join(p.Data, "logs", "volume-"+n+".log")
}

// EnvFileN is the environment file of instance k (instance 0 uses EnvFile). Each instance has its
// own file because PORT differs, and systemd lets EnvironmentFile override Environment=.
func (p Paths) EnvFileN(n string, k int) string {
	if k <= 0 {
		return p.EnvFile(n)
	}
	return filepath.Join(p.Data, "env", fmt.Sprintf("%s.r%d.env", n, k))
}

// Deployer deploys apps. Settings and GitHub are functions so that changes made in the UI apply immediately.
type Deployer struct {
	Store    *store.Store
	Paths    Paths
	DataDir  string
	Settings func() settings.Settings
	GitHub   func() *githubapp.Client
	Node     *nodeenv.Installer // installs Node.js (nvm) when a project needs it; may be nil

	pendMu  sync.Mutex
	pending map[string]bool

	volMu   sync.Mutex // guards the volume backup/restore running on each app
	volBusy map[string]string
}

// New returns a Deployer rooted at dataDir.
func New(st *store.Store, dataDir string, set func() settings.Settings, gh func() *githubapp.Client) *Deployer {
	return &Deployer{Store: st, Paths: Paths{Data: dataDir}, DataDir: dataDir, Settings: set, GitHub: gh,
		pending: map[string]bool{}, volBusy: map[string]string{}}
}

func (d *Deployer) unitFile(app string, k int) string {
	return filepath.Join(d.Settings().UnitDir, systemd.ReplicaUnitName(app, k))
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
		// -m keeps the given environment. Without it runuser replaces HOME with the account's
		// home (/home/<user>, which does not exist for a --no-create-home system user), and
		// npm, pip, cargo... fail with EACCES creating their cache there.
		args = append([]string{"-u", d.Settings().AppUser, "-m", "--", name}, args...)
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
	a.Replicas = max(a.Replicas, 1)
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
		if err := chownTo(ctx, d.Settings().AppUser, appDir); err != nil {
			return err
		}
	}

	tpl, fromTemplate := templates.ForRepo(a.Repo)
	switch {
	case strings.HasPrefix(a.Repo, templates.RefPrefix) && !fromTemplate:
		return fmt.Errorf("o template %q não existe mais nesta versão do painel", strings.TrimPrefix(a.Repo, templates.RefPrefix))
	case fromTemplate && tpl.FilesBased():
		if err := d.writeTemplateFiles(ctx, tpl, src, log); err != nil {
			return err
		}
	default:
		if err := d.fetchGit(ctx, a, appDir, src, gitEnv, log); err != nil {
			return err
		}
	}
	if fromTemplate {
		if err := d.ensureRequires(ctx, tpl.Requires, log); err != nil {
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

	// Service definition: one unit and one environment file per instance.
	if msg := d.PortConflict(a); msg != "" {
		return errors.New(msg)
	}
	mounts, err := d.prepareMounts(ctx, a)
	if err != nil {
		return err
	}
	for k := 0; k < a.Replicas; k++ {
		if err := d.writeInstance(a, k, mounts); err != nil {
			return err
		}
	}
	d.removeSurplusInstances(ctx, log, a)
	units := systemd.ReplicaUnits(a.Name, a.Replicas)
	for _, args := range [][]string{{"daemon-reload"}, append([]string{"enable"}, units...)} {
		if err := execx.Run(ctx, log, "systemctl", args...); err != nil {
			return fmt.Errorf("systemctl %s: %w", args[0], err)
		}
	}
	if err := d.restartInstances(ctx, a, log); err != nil {
		return err
	}

	if a.Domain == "" {
		if a.Replicas > 1 || a.RateLimit > 0 {
			fmt.Fprintln(log, "AVISO: sem domínio não há Nginx na frente: o balanceamento entre instâncias, o limite de requisições e a compressão só valem com um domínio.")
		}
		return d.nginx().Remove(ctx, log, a.Name)
	}
	return d.publishSite(ctx, log, a)
}

// fetchGit clones the repository, or updates the checkout when it already points at it.
func (d *Deployer) fetchGit(ctx context.Context, a store.App, appDir, src string, gitEnv []string, log io.Writer) error {
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
		return d.run(ctx, log, appDir, gitEnv, "git", "clone", "--depth", "1", "--branch", a.Branch, "--", a.Repo, src)
	}
	if err := d.run(ctx, log, src, gitEnv, "git", "fetch", "--depth", "1", "origin", a.Branch); err != nil {
		return err
	}
	return d.run(ctx, log, src, gitEnv, "git", "reset", "--hard", "FETCH_HEAD")
}

// writeTemplateFiles lays the template's own files over the project folder. They are rewritten on
// every deploy; what the build installed next to them (node_modules, a downloaded binary) stays.
func (d *Deployer) writeTemplateFiles(ctx context.Context, t templates.Template, src string, log io.Writer) error {
	fmt.Fprintf(log, "template %s: gravando %d arquivo(s)\n", t.Name, len(t.Files))
	if err := os.MkdirAll(src, 0o755); err != nil {
		return err
	}
	paths := []string{src}
	for name, content := range t.Files {
		p := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := writeFile(p, content, 0o644); err != nil {
			return err
		}
		paths = append(paths, p)
	}
	if user := d.Settings().AppUser; user != "" {
		for _, p := range paths {
			if err := chownTo(ctx, user, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureRequires checks that the programs a template needs exist, installing Node.js with nvm
// when that is what is missing (the same help detected projects get).
func (d *Deployer) ensureRequires(ctx context.Context, requires []string, log io.Writer) error {
	missing := func() (m []string) {
		for _, r := range requires {
			if _, err := exec.LookPath(r); err != nil {
				m = append(m, r)
			}
		}
		return m
	}
	m := missing()
	if len(m) > 0 && d.Node != nil && needsNode(m) {
		fmt.Fprintln(log, "Node.js ausente no servidor: instalando com o nvm…")
		if err := d.Node.Ensure(ctx, log); err != nil {
			fmt.Fprintf(log, "AVISO: %v\n", err)
		}
		m = missing()
	}
	if len(m) > 0 {
		return fmt.Errorf("faltam programas no servidor: %s", strings.Join(m, ", "))
	}
	return nil
}

// writeInstance writes the environment file and the unit of instance k. Every instance mounts the
// same storage: the volume is shared, not one copy per instance.
func (d *Deployer) writeInstance(a store.App, k int, m mountSetup) error {
	if err := writeFile(d.Paths.EnvFileN(a.Name, k), d.envFileContent(a, a.Port+k, m), 0o600); err != nil {
		return err
	}
	unit := systemd.Unit{App: a.Name, WorkDir: d.Paths.SrcDir(a.Name), EnvFile: d.Paths.EnvFileN(a.Name, k), StartCmd: a.StartCmd, User: d.Settings().AppUser,
		MemoryMB: a.MemoryMB, CPUPercent: a.CPUPercent, NoFile: 65536,
		BindPaths: m.Bind, BindReadOnlyPaths: m.ReadOnly}
	if k > 0 {
		unit.Desc = fmt.Sprintf("BR Panel app %s (instância %d)", a.Name, k)
	}
	return writeFile(d.unitFile(a.Name, k), unit.Render(), 0o644)
}

// siteFor is the nginx vhost of an app with its current instances.
func siteFor(a store.App) nginx.Site {
	site := nginx.Site{Key: a.Name, Domain: a.Domain, Upstream: fmt.Sprintf("127.0.0.1:%d", a.Port), RateLimit: a.RateLimit, Gzip: true}
	for k := 0; k < max(a.Replicas, 1); k++ {
		site.Upstreams = append(site.Upstreams, fmt.Sprintf("127.0.0.1:%d", a.Port+k))
	}
	applyWeb(&site, a.Web)
	return site
}

// drainDelay is how long a removed instance keeps running after nginx stopped sending it new
// requests, so the requests already in flight can finish.
const drainDelay = 5 * time.Second

// Scale changes the number of running instances of an already deployed app to n, without a deploy.
// New instances are started and must accept connections before nginx learns about them; removed
// ones leave nginx first and are stopped after drainDelay. On success the new count and the time
// are saved (the time starts the autoscaler's cooldown).
func (d *Deployer) Scale(ctx context.Context, a store.App, n int, log io.Writer) error {
	old, n := max(a.Replicas, 1), max(n, 1)
	if n == old {
		return nil
	}
	if _, err := os.Stat(d.unitFile(a.Name, 0)); err != nil {
		return errors.New("a aplicação ainda não foi publicada")
	}
	grown := a
	grown.Replicas = n
	if msg := d.PortConflict(grown); msg != "" {
		return errors.New(msg)
	}
	if n > old {
		mounts, err := d.prepareMounts(ctx, grown)
		if err != nil {
			return err
		}
		var fresh []string
		for k := old; k < n; k++ {
			if err := d.writeInstance(grown, k, mounts); err != nil {
				return err
			}
			fresh = append(fresh, systemd.ReplicaUnitName(a.Name, k))
		}
		for _, args := range [][]string{{"daemon-reload"}, append([]string{"enable", "--now"}, fresh...)} {
			if err := execx.Run(ctx, log, "systemctl", args...); err != nil {
				d.removeSurplusInstances(ctx, log, a) // put things back as they were
				return fmt.Errorf("systemctl %s: %w", args[0], err)
			}
		}
		for k := old; k < n; k++ {
			if !waitListening(ctx, a.Port+k, instanceReadyTimeout) {
				d.removeSurplusInstances(ctx, log, a)
				return fmt.Errorf("a instância %d não respondeu na porta %d em %s", k, a.Port+k, instanceReadyTimeout)
			}
		}
	}
	if a.Domain != "" {
		if err := d.nginx().Reload(ctx, log, siteFor(grown)); err != nil {
			if n > old {
				d.removeSurplusInstances(ctx, log, a)
			}
			return err
		}
	}
	if n < old {
		select {
		case <-ctx.Done():
		case <-time.After(drainDelay):
		}
		d.removeSurplusInstances(ctx, log, grown)
	}
	return d.Store.SetScaled(a.Name, n, time.Now())
}

// instanceReadyTimeout is how long a rolling restart waits for an instance to accept connections.
const instanceReadyTimeout = 30 * time.Second

// restartInstances restarts the instances one at a time and, with more than one, waits for each to
// accept connections before touching the next. Nginx therefore always has a live instance, and
// the site stays up through a deploy. An instance that never listens (a worker without a port)
// only produces a warning.
func (d *Deployer) restartInstances(ctx context.Context, a store.App, log io.Writer) error {
	units := systemd.ReplicaUnits(a.Name, a.Replicas)
	for k, u := range units {
		if err := execx.Run(ctx, log, "systemctl", "restart", u); err != nil {
			return fmt.Errorf("systemctl restart: %w", err)
		}
		if len(units) > 1 && !waitListening(ctx, a.Port+k, instanceReadyTimeout) {
			fmt.Fprintf(log, "AVISO: a instância %d não respondeu na porta %d em %s; seguindo para a próxima.\n", k, a.Port+k, instanceReadyTimeout)
		}
	}
	return nil
}

// Restart restarts every instance of an app the same way a deploy does (one at a time).
func (d *Deployer) Restart(a store.App) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(a.Replicas)*(instanceReadyTimeout+10*time.Second))
	defer cancel()
	return d.restartInstances(ctx, a, io.Discard)
}

// waitListening polls 127.0.0.1:port until it accepts a connection.
func waitListening(ctx context.Context, port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
			c.Close()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return false
}

// removeSurplusInstances stops and deletes the units and env files of instances beyond a.Replicas
// (left over after lowering the count).
func (d *Deployer) removeSurplusInstances(ctx context.Context, log io.Writer, a store.App) {
	for k := max(a.Replicas, 1); k < validate.MaxReplicas; k++ {
		unitPath := d.unitFile(a.Name, k)
		if _, err := os.Stat(unitPath); err != nil {
			continue
		}
		fmt.Fprintf(log, "removendo a instância %d (o número de instâncias diminuiu)\n", k)
		_ = execx.Run(ctx, log, "systemctl", "disable", "--now", systemd.ReplicaUnitName(a.Name, k))
		_ = os.Remove(unitPath)
		_ = os.Remove(d.Paths.EnvFileN(a.Name, k))
	}
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

// envFileContent renders the environment of one instance; port is that instance's own PORT.
func (d *Deployer) envFileContent(a store.App, port int, m mountSetup) string {
	var b strings.Builder
	if port > 0 {
		b.WriteString(systemd.EnvFileLine("PORT", fmt.Sprint(port)))
	}
	b.WriteString(systemd.EnvFileLine("HOST", ListenHost(a)))
	kv, _ := d.envPairs(a) // validated on save
	for _, p := range kv {
		b.WriteString(systemd.EnvFileLine(p[0], p[1]))
	}
	// After the app's own variables: a VOLUME_ name the admin typed by hand must not hide the
	// real path.
	for _, p := range m.Env {
		b.WriteString(systemd.EnvFileLine(p[0], p[1]))
	}
	return b.String()
}

// chownTo hands path to the app user. It runs as the panel (root) on purpose: Deployer.run
// drops privileges to the app user, and an unprivileged chown always fails with "Operation
// not permitted". The command's own message is kept in the error.
func chownTo(ctx context.Context, user, path string) error {
	out, err := exec.CommandContext(ctx, "chown", user+":", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("chown %s para %s: %w: %s", path, user, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

// Remove stops the service and deletes the app, its files, unit and vhost. The volumes and the
// stored files go too; the screen asks for confirmation before calling this.
func (d *Deployer) Remove(name string) error {
	paths := []string{d.Paths.LogFile(name), d.Paths.VolumeLogFile(name)}
	for k := 0; k < validate.MaxReplicas; k++ { // every instance that may exist, whatever the saved count says
		_, _ = systemd.Ctl("disable", "--now", systemd.ReplicaUnitName(name, k))
		paths = append(paths, d.unitFile(name, k), d.Paths.EnvFileN(name, k))
	}
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_, _ = systemd.Ctl("daemon-reload")
	_ = d.nginx().Remove(context.Background(), io.Discard, name)
	for _, dir := range []string{d.Paths.AppDir(name), d.Paths.VolumesDir(name), d.Paths.FilesDir(name), d.Paths.VolumeBackupDir(name)} {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
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
