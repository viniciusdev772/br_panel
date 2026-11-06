package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/sysinfo"
	"github.com/viniciusdev772/br_panel/internal/systemd"
)

// The panel gives every instance a PORT, but plenty of code ignores it and listens on a number
// written in the source. Two copies of such an app (or its replicas) then fight for the port, and
// systemd restarts the loser forever while the deploy still looks fine. After a deploy this file
// looks at what the instances really do, says so in the deploy log (never failing the deploy: a
// worker without a port is legitimate) and remembers when an app ignores PORT, so the autoscaler
// does not add instances that can only crash.

// portProbe is what the check observes; tests replace it with a scripted one.
type portProbe interface {
	info(unit string) systemd.ServiceInfo
	ports(unit string) []int // TCP ports the unit's processes listen on
	journal(unit string) string
	holders(port int) []string // units (or commands) listening on a port
}

type liveProbe struct{}

func (liveProbe) info(unit string) systemd.ServiceInfo { return systemd.InfoUnit(unit) }
func (liveProbe) journal(unit string) string           { return systemd.JournalUnit(unit, 40) }
func (liveProbe) holders(port int) []string            { return sysinfo.PortHolders(port) }
func (liveProbe) ports(unit string) []int {
	cg, _ := systemd.Ctl("show", unit, "-p", "ControlGroup", "--value")
	return sysinfo.ListeningPorts(strings.TrimSpace(cg))
}

// portWatch holds the timings of the check.
type portWatch struct {
	site time.Duration // an app with a domain: Nginx needs its port, so wait longer for it
	idle time.Duration // an app without one: only catch a crash or a wrong port
	poll time.Duration
}

var defaultWatch = portWatch{site: 25 * time.Second, idle: 6 * time.Second, poll: 500 * time.Millisecond}

type verdictKind int

const (
	vOK        verdictKind = iota // listens on the expected port
	vCrash                        // restarted by itself or failed: it does not stay up
	vElsewhere                    // alive, but listens only on other ports
	vSilent                       // alive, listens on nothing (yet)
	vGone                         // exited cleanly: a job, not a service
	vInterrupted
)

type verdict struct {
	kind     verdictKind
	got      []int // ports it listens on
	restarts int
}

func contains(ports []int, p int) bool { return slices.Contains(ports, p) }

// watch observes one instance until it listens on port, proves to be crashing, or the time is up.
func watch(ctx context.Context, p portProbe, w portWatch, unit string, port int, hasSite bool) verdict {
	limit := w.idle
	if hasSite {
		limit = w.site
	}
	start := time.Now()
	var v verdict
	for {
		info, ports := p.info(unit), p.ports(unit)
		v = verdict{got: ports, restarts: info.Restarts}
		// The first automatic restart shows as SubState "auto-restart" while NRestarts is still 0
		// (a manual restart, which the deploy does, resets the counter).
		falling := info.Restarts > 0 || info.SubState == "auto-restart"
		switch {
		case contains(ports, port):
			v.kind = vOK
			return v
		case info.State == "failed" || info.Restarts >= 2:
			v.kind = vCrash
			return v
		case falling:
			limit = max(limit, w.site) // one restart: it may recover, give it the longer time
		case info.State == "inactive":
			v.kind = vGone
			return v
		}
		if time.Since(start) >= limit {
			switch {
			case falling:
				v.kind = vCrash
			case len(ports) > 0:
				v.kind = vElsewhere
			default:
				v.kind = vSilent
			}
			return v
		}
		select {
		case <-ctx.Done():
			v.kind = vInterrupted
			return v
		case <-time.After(w.poll):
		}
	}
}

// ---- finding the cause ----

var addrInUseRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)address already in use\s*:+(\d{2,5})\b`), // Node: listen EADDRINUSE: address already in use :::3090
	regexp.MustCompile(`(?i)listen tcp [^\s:]*:(\d{2,5}): bind`),     // Go
	regexp.MustCompile(`(?i)EADDRINUSE[^\n]*?:(\d{2,5})\b`),
	regexp.MustCompile(`\bport: (\d{2,5})\b`), // Node prints the error object: { ... port: 3090 }
}

// addrInUse reports whether the journal shows a port-in-use failure and, when the message says
// which port, that port (0 otherwise: Python only prints "Address already in use").
func addrInUse(journal string) (port int, found bool) {
	lines := strings.Split(journal, "\n")
	for i := len(lines) - 1; i >= 0; i-- { // the latest failure matters
		l := lines[i]
		low := strings.ToLower(l)
		if !strings.Contains(low, "eaddrinuse") && !strings.Contains(low, "address already in use") && !strings.Contains(low, "errno 98") {
			continue
		}
		for _, re := range addrInUseRes {
			if m := re.FindStringSubmatch(l); m != nil {
				n, _ := strconv.Atoi(m[1])
				return n, true
			}
		}
		for _, near := range lines[i:min(i+12, len(lines))] { // Node puts "port: N" lines below the error
			if m := addrInUseRes[3].FindStringSubmatch(near); m != nil {
				n, _ := strconv.Atoi(m[1])
				return n, true
			}
		}
		return 0, true
	}
	return 0, false
}

var portLiteralRe = regexp.MustCompile(`\b\d{4,5}\b`)

// startPorts are the port-like numbers written in a start command.
func startPorts(cmd string) []int {
	var out []int
	for _, m := range portLiteralRe.FindAllString(cmd, -1) {
		if n, _ := strconv.Atoi(m); n >= 1024 && n <= 65535 && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// Files worth reading for a port number, and folders that never hold the app's own code.
var (
	sourceExts = map[string]bool{".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".py": true, ".rb": true, ".go": true,
		".php": true, ".java": true, ".kt": true, ".cs": true, ".rs": true, ".ex": true, ".exs": true}
	skipDirs = map[string]bool{"node_modules": true, ".git": true, "vendor": true, "dist": true, "build": true, ".next": true, "target": true,
		"venv": true, ".venv": true, "__pycache__": true, "site-packages": true}
	secretish = regexp.MustCompile(`(?i)://|pass|secret|token|key|auth`)
)

const (
	maxSourceFiles = 3000
	maxSourceBytes = 400 << 10
)

// findPortInSource returns up to three lines of the project that declare the port, as
// "file:line: text". The repository is not trusted: links are never followed, and a line that
// looks like it carries a credential or a URL is left out (the log is shown to the assistant too).
func findPortInSource(dir string, port int) []string {
	// os.Root keeps every read inside the project even if the app swaps a file for a link while we
	// scan: the panel is root and the code is not its own.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil
	}
	defer root.Close()
	fsys := root.FS()
	re := regexp.MustCompile(`(^|[^0-9.])` + strconv.Itoa(port) + `([^0-9]|$)`)
	var out []string
	seen := 0
	_ = fs.WalkDir(fsys, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && rel != "." {
				return fs.SkipDir
			}
			return nil
		}
		if len(out) >= 3 || !d.Type().IsRegular() || !sourceExts[path.Ext(rel)] {
			return nil
		}
		if seen++; seen > maxSourceFiles {
			return fs.SkipAll
		}
		if fi, err := d.Info(); err != nil || fi.Size() > maxSourceBytes {
			return nil
		}
		b, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if len(out) >= 3 {
				break
			}
			if !re.MatchString(line) || secretish.MatchString(line) {
				continue
			}
			text := strings.TrimSpace(line)
			if len(text) > 100 {
				text = text[:100] + "…"
			}
			out = append(out, fmt.Sprintf("%s:%d: %s", rel, i+1, text))
		}
		return nil
	})
	return out
}

// holderLabel turns a holder (a unit name or a command) into words for the log.
func holderLabel(h string) string {
	switch {
	case strings.HasPrefix(h, "brpanel-") && strings.HasSuffix(h, ".service"):
		return "a aplicação " + strings.TrimSuffix(strings.TrimPrefix(h, "brpanel-"), ".service")
	case strings.HasPrefix(h, "brpanelr") && strings.HasSuffix(h, ".service"):
		if _, app, ok := strings.Cut(strings.TrimSuffix(h, ".service"), "-"); ok {
			return "uma instância extra de " + app
		}
	case strings.HasPrefix(h, "brpaneldb-") && strings.HasSuffix(h, ".service"):
		return "o banco " + strings.TrimSuffix(strings.TrimPrefix(h, "brpaneldb-"), ".service")
	}
	return h
}

const fixHint = "O código precisa ler a porta da variável PORT, que o painel define para cada instância, em vez de fixar o número " +
	"(Node: process.env.PORT; Python: os.environ[\"PORT\"]; Go: os.Getenv(\"PORT\"); PHP/Ruby/Java: a variável PORT do ambiente)."

// ---- the report ----

type portReport struct {
	At       time.Time `json:"at"`
	Ignored  bool      `json:"ignores_port"` // the app listens where it likes: it cannot have more than one instance
	Note     string    `json:"note"`         // one line for the panel's screens
	Problems []problem `json:"problems"`     // the instances that were not right, to look at again later
}

// problem is an instance that did not listen on its port when the deploy checked.
type problem struct {
	Unit string `json:"unit"`
	Port int    `json:"port"`
}

func (d *Deployer) reportFile(app string) string {
	return filepath.Join(d.Paths.AppDir(app), "portcheck.json")
}

func (d *Deployer) loadReport(app string) portReport {
	var r portReport
	if b, err := os.ReadFile(d.reportFile(app)); err == nil {
		_ = json.Unmarshal(b, &r)
	}
	return r
}

// current is the saved report, unless every instance it complained about now listens on its port:
// a slow app that opened the port after the deploy stopped waiting, or one the person fixed and
// restarted, must not keep a warning (or a locked autoscaler) that is no longer true.
func (d *Deployer) current(app string) portReport {
	r := d.loadReport(app)
	if len(r.Problems) == 0 || d.probe == nil {
		return r
	}
	for _, p := range r.Problems {
		if !contains(d.probe.ports(p.Unit), p.Port) {
			return r
		}
	}
	_ = os.Remove(d.reportFile(app))
	return portReport{}
}

// PortNote is the one-line warning left by the last deploy's port check (empty when all is well).
func (d *Deployer) PortNote(app string) string { return d.current(app).Note }

// IgnoresPort is true when the last check found that the app does not listen on the PORT it was given.
func (d *Deployer) IgnoresPort(app string) bool { return d.current(app).Ignored }

func (d *Deployer) saveReport(app string, r portReport) {
	if r.Note == "" && !r.Ignored {
		_ = os.Remove(d.reportFile(app))
		return
	}
	r.At = time.Now()
	if b, err := json.Marshal(r); err == nil {
		_ = os.WriteFile(d.reportFile(app), b, 0o600)
	}
}

// verifyPorts watches each instance after a deploy and writes what it finds to the log.
func (d *Deployer) verifyPorts(ctx context.Context, a store.App, log io.Writer) {
	if d.probe == nil {
		return
	}
	defer func() { // an extra, advisory check must never take the panel (a root service) down
		if r := recover(); r != nil {
			fmt.Fprintf(log, "AVISO: a verificação de porta falhou e foi ignorada: %v\n", r)
		}
	}()
	a.Replicas = max(a.Replicas, 1)
	var rep portReport
	record := func(f finding, unit string, port int) {
		rep.Problems = append(rep.Problems, problem{Unit: unit, Port: port})
		fmt.Fprintln(log, strings.Join(f.lines, "\n"))
		if rep.Note == "" {
			rep.Note = f.note
		}
		rep.Ignored = rep.Ignored || f.ignored
	}
	for k := 0; k < a.Replicas; k++ {
		unit, port := systemd.ReplicaUnitName(a.Name, k), a.Port+k
		v := watch(ctx, d.probe, d.watchTimes, unit, port, a.Domain != "")
		switch v.kind {
		case vInterrupted:
			return
		case vCrash:
			record(d.explainCrash(a, k, unit, port, v), unit, port)
		case vElsewhere:
			if a.Domain == "" && a.Slots() < 2 { // nothing depends on that port: no warning, and no verdict on weak evidence
				continue
			}
			record(d.explainElsewhere(a, k, port, v.got), unit, port)
		case vSilent:
			if a.Domain != "" {
				note := fmt.Sprintf("a instância %d está ativa, mas ainda não abriu a porta %d", k, port)
				record(finding{note: note, lines: []string{"AVISO: " + note + "; o Nginx responde 502 até ela abrir (aplicações lentas podem demorar; um worker sem porta não deveria ter domínio)."}}, unit, port)
			}
		}
	}
	d.saveReport(a.Name, rep)
}

// finding is one problem: the lines for the deploy log, a one-sentence note for the screens, and
// whether it proves the app does not read PORT.
type finding struct {
	lines   []string
	note    string
	ignored bool
}

func (d *Deployer) explainElsewhere(a store.App, k, port int, got []int) finding {
	ports := make([]string, len(got))
	for i, p := range got {
		ports[i] = strconv.Itoa(p)
	}
	note := fmt.Sprintf("a instância %d escuta na porta %s, mas o painel espera a %d (variável PORT): o código fixa a porta", k, strings.Join(ports, ", "), port)
	consequence := fmt.Sprintf("O painel espera a %d; com mais de uma instância elas brigariam pela mesma porta.", port)
	if a.Domain != "" {
		consequence = fmt.Sprintf("O Nginx aponta para a %d e o site dará 502; com mais de uma instância elas brigariam pela mesma porta.", port)
	}
	lines := []string{"AVISO: " + note + ". " + consequence}
	lines = append(lines, d.whereIsPort(a, got[0])...)
	lines = append(lines, fixHint)
	if k == 0 && a.Replicas == 1 && !a.AutoScale.Enabled {
		adopt := a
		adopt.Port = got[0]
		if msg := d.PortConflict(adopt); msg == "" {
			lines = append(lines, fmt.Sprintf("Sem mexer no código: brpanel apps set %s --port %d e faça um novo deploy (vale só para uma instância).", a.Name, got[0]))
		}
	}
	return finding{lines: lines, note: note, ignored: k == 0}
}

// whereIsPort says where the source or the start command writes the number.
func (d *Deployer) whereIsPort(a store.App, port int) []string {
	var lines []string
	if slices.Contains(startPorts(a.StartCmd), port) {
		lines = append(lines, fmt.Sprintf("O comando de início da aplicação já contém o número %d.", port))
	}
	if hits := findPortInSource(d.ProjectDir(a), port); len(hits) > 0 {
		lines = append(lines, "Onde o número aparece no código: "+strings.Join(hits, " | "))
	}
	return lines
}

// explainCrash tells why an instance keeps restarting. ignored is true when the cause is two
// instances of the same app fighting for one port, which proves the app does not read PORT.
func (d *Deployer) explainCrash(a store.App, k int, unit string, port int, v verdict) finding {
	journal := d.probe.journal(unit)
	p, inUse := addrInUse(journal)
	if inUse && p == 0 { // the message names no port: take the one the start command writes and somebody holds
		for _, c := range startPorts(a.StartCmd) {
			if len(d.probe.holders(c)) > 0 {
				p = c
				break
			}
		}
	}
	if !inUse {
		how := "caiu e não fica de pé"
		if v.restarts > 0 {
			how = fmt.Sprintf("reiniciou %d vez(es) sozinha e não fica de pé", v.restarts)
		}
		note := fmt.Sprintf("a instância %d %s: o erro está no próprio app (não é de porta); veja o log do serviço", k, how)
		lines := []string{"AVISO: " + note + ". Últimas linhas do log:"}
		return finding{lines: append(lines, lastLines(journal, 3)...), note: note}
	}
	own := systemd.ReplicaUnits(a.Name, a.Slots())
	var who []string
	ignored := false
	for _, h := range d.probe.holders(p) {
		if h == unit {
			continue
		}
		label := holderLabel(h)
		if slices.Contains(own, h) {
			label, ignored = "outra instância deste mesmo app ("+h+")", true
		}
		who = append(who, label)
	}
	where := "outro processo do servidor"
	if len(who) > 0 {
		where = strings.Join(who, ", ")
	}
	if p == 0 {
		note := fmt.Sprintf("a instância %d não consegue subir: uma porta já está em uso (o log não diz qual) e o systemd reinicia o app em loop", k)
		return finding{lines: []string{"AVISO: " + note + ".", fixHint}, note: note}
	}
	note := fmt.Sprintf("a instância %d não consegue subir: a porta %d já está em uso (quem a usa: %s) e o systemd reinicia o app em loop", k, p, where)
	lines := []string{"AVISO: " + note + ". O painel deu a esta instância a porta " + strconv.Itoa(port) + " (PORT), então o código está fixando a " + strconv.Itoa(p) + "."}
	lines = append(lines, d.whereIsPort(a, p)...)
	return finding{lines: append(lines, fixHint), note: note, ignored: ignored}
}

func lastLines(journal string, n int) []string {
	var out []string
	for _, l := range strings.Split(journal, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "-- ") {
			if len(l) > 160 {
				l = l[:160] + "…"
			}
			out = append(out, "  "+l)
		}
	}
	return out[max(0, len(out)-n):]
}
