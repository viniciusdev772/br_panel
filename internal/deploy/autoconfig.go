package deploy

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/viniciusdev772/br_panel/internal/detect"
	"github.com/viniciusdev772/br_panel/internal/githubapp"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

// ListenHost is where the app should bind: loopback behind Nginx, all interfaces otherwise.
func ListenHost(a store.App) string {
	if a.Domain != "" {
		return "127.0.0.1"
	}
	return "0.0.0.0"
}

const (
	firstAutoPort = 3001
	lastAutoPort  = 3999
)

// PickPort returns a free port not used by another app and not currently listening.
func (d *Deployer) PickPort(except string) int { return d.PickPortRange(except, 1) }

// PickPortRange returns the first port of n consecutive free ports (instance k of an app listens
// on first+k), or 0 when there is no such range. Ports of the other apps' instances and of
// databases count as taken, and so does anything already listening.
func (d *Deployer) PickPortRange(except string, n int) int {
	used := d.Store.UsedPorts()
	if self, ok := d.Store.Get(except); ok { // its own ports are free to reuse
		for k := 0; k < self.Slots(); k++ {
			delete(used, self.Port+k)
		}
	}
	n = max(n, 1)
	for p := firstAutoPort; p+n-1 <= lastAutoPort; p++ {
		ok := true
		for k := 0; k < n && ok; k++ {
			if used[p+k] {
				ok = false
				continue
			}
			l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p+k))
			if err != nil {
				ok = false
				continue
			}
			l.Close()
		}
		if ok {
			return p
		}
	}
	return 0
}

// PortConflict explains why the instance ports of a (Port .. Port+Replicas-1) cannot be used, or
// returns "" when they are free of other apps and databases. Ports already listening are not
// checked: they may be this app's own running instances.
func (d *Deployer) PortConflict(a store.App) string {
	if a.Port == 0 {
		return ""
	}
	last := a.Port + a.Slots() - 1
	if last > 65535 {
		return fmt.Sprintf("as %d instâncias não cabem a partir da porta %d (o limite é 65535)", a.Slots(), a.Port)
	}
	for _, o := range d.Store.List() {
		if o.Name != a.Name && o.Port <= last && a.Port <= o.Port+o.Slots()-1 {
			return fmt.Sprintf("as portas %d a %d colidem com as da aplicação %q (%d a %d)", a.Port, last, o.Name, o.Port, o.Port+o.Slots()-1)
		}
	}
	for _, db := range d.Store.ListDatabases() {
		if db.Port >= a.Port && db.Port <= last {
			return fmt.Sprintf("a porta %d é do banco %q e cai entre as portas das instâncias (%d a %d)", db.Port, db.Name, a.Port, last)
		}
	}
	return ""
}

// autoConfigure fills in missing commands by inspecting the checkout. An empty start
// command means "detect"; the result is saved so it shows up (and can be edited) in the UI.
func (d *Deployer) autoConfigure(ctx context.Context, a store.App, src string, log io.Writer) (store.App, error) {
	if a.StartCmd != "" {
		return a, nil
	}
	det := detect.DetectApp(src, a.RootDir)
	if det == nil {
		return a, fmt.Errorf("não consegui detectar o tipo do projeto (procurei package.json, go.mod, requirements.txt, Cargo.toml, Gemfile, composer.json, index.html…). Informe o comando de início ou adicione um Procfile")
	}
	fmt.Fprintf(log, "detectado: %s\n", det.Kind)
	for _, n := range det.Notes {
		fmt.Fprintf(log, "  nota: %s\n", n)
	}
	if det.Start == "" {
		return a, fmt.Errorf("projeto %s detectado, mas sem comando de início conhecido; informe-o ou adicione um Procfile", det.Kind)
	}
	missing := detect.MissingRequirements(det, exec.LookPath)
	if len(missing) > 0 && d.Node != nil && needsNode(det.Requires) {
		fmt.Fprintln(log, "Node.js ausente no servidor: instalando com o nvm…")
		if err := d.Node.Ensure(ctx, log); err != nil {
			fmt.Fprintf(log, "AVISO: %v\n", err)
		}
		missing = detect.MissingRequirements(det, exec.LookPath)
	}
	if len(missing) > 0 {
		return a, fmt.Errorf("faltam programas no servidor: %s", strings.Join(missing, ", "))
	}
	a.StartCmd = det.Start
	a.Kind = det.Kind
	if a.BuildCmd == "" {
		a.BuildCmd = det.Build
	}
	if a.Port == 0 {
		a.Port = d.PickPortRange(a.Name, a.Slots())
	}
	fmt.Fprintf(log, "  build:  %s\n  início: %s\n  porta:  %d\n", orDash(a.BuildCmd), a.StartCmd, a.Port)
	if err := d.Store.UpdateConfig(a); err != nil {
		return a, err
	}
	d.Store.SetKind(a.Name, a.Kind)
	return a, nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

var nameSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// needsNode reports whether any required program comes with Node.js (nvm).
func needsNode(requires []string) bool {
	for _, r := range requires {
		switch r {
		case "node", "npm", "npx", "pnpm", "yarn":
			return true
		}
	}
	return false
}

// SuggestNameIn names an app of a monorepo after the repository and its folder (mono + apps/web
// = "mono-web"); without a folder it is SuggestName.
func SuggestNameIn(repo, root string) string {
	base := SuggestName(repo)
	if root == "" {
		return base
	}
	last := root[strings.LastIndex(root, "/")+1:]
	n := strings.Trim(nameSlugRe.ReplaceAllString(strings.ToLower(base+"-"+last), "-"), "-")
	if len(n) > 32 {
		n = strings.Trim(n[:32], "-")
	}
	if validate.Name(n) != nil {
		return base
	}
	return n
}

// SuggestName derives an app name from the repository URL.
func SuggestName(repo string) string {
	base := repo[strings.LastIndexAny(repo, "/:")+1:]
	base = strings.TrimSuffix(base, ".git")
	n := strings.Trim(nameSlugRe.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if len(n) > 32 {
		n = strings.Trim(n[:32], "-")
	}
	if validate.Name(n) != nil {
		return ""
	}
	return n
}

// ErrCloneFailed is returned by DetectRepo when the repository could not be cloned.
type ErrCloneFailed struct{ Detail string }

func (e ErrCloneFailed) Error() string {
	return "não consegui clonar (confira a URL, a branch e o acesso): " + e.Detail
}

var detectMu sync.Mutex // one throw-away clone at a time

// RepoDetection is what DetectRepo finds in a repository.
type RepoDetection struct {
	Det     *detect.Detection  // nil for projects it does not recognise
	Missing []string           // programs the suggested commands need that this server lacks
	Apps    []detect.Candidate // the apps of a monorepo (empty for a single project)
}

// DetectRepo makes a shallow, temporary clone of repo@branch and reports what detect finds in
// its folder root ("" = the whole repository; an app of a monorepo otherwise), the programs the
// suggested commands need that are missing on this server, and the apps a monorepo holds.
func (d *Deployer) DetectRepo(ctx context.Context, repo, branch, root string) (RepoDetection, error) {
	var res RepoDetection
	detectMu.Lock()
	defer detectMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	tmp, err := os.MkdirTemp(filepath.Join(d.DataDir, "tmp"), "detect-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(tmp)
	env := []string{"PATH=" + execPath(), "HOME=" + tmp, "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=" + cmp.Or(d.gitProtocols, "https:ssh"),
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new"}
	if u := d.Settings().AppUser; u != "" {
		if err := chownTo(ctx, u, tmp); err != nil {
			return res, err
		}
	}
	gitEnv := githubapp.AuthEnv(ctx, d.GitHub(), repo, env, io.Discard)
	var out strings.Builder
	src := filepath.Join(tmp, "src")
	if err := d.run(ctx, &out, tmp, gitEnv, "git", "clone", "--depth", "1", "--branch", branch, "--", repo, src); err != nil {
		return res, ErrCloneFailed{Detail: lastLine(out.String())}
	}
	// The clone is not trusted and the panel reads it as root: the folder must be a real one.
	if root != "" {
		if err := noSymlinks(src, filepath.Join(src, filepath.FromSlash(root))); err != nil {
			return res, fmt.Errorf("diretório raiz %q: %w", root, err)
		}
		if fi, err := os.Stat(filepath.Join(src, filepath.FromSlash(root))); err != nil || !fi.IsDir() {
			return res, fmt.Errorf("o diretório raiz %q não existe neste repositório (branch %s)", root, branch)
		}
	}
	res.Apps = detect.Workspaces(src)
	if res.Det = detect.DetectApp(src, root); res.Det != nil {
		res.Missing = detect.MissingRequirements(res.Det, exec.LookPath)
	}
	return res, nil
}
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
