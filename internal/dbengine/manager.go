package dbengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/viniciusdev772/br_panel/internal/execx"
	"github.com/viniciusdev772/br_panel/internal/settings"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

const (
	provisionTimeout = 15 * time.Minute
	backupTimeout    = 30 * time.Minute
	keepBackups      = 14
)

// Manager creates and operates database instances.
type Manager struct {
	Store    *store.Store
	DataDir  string
	Settings func() settings.Settings

	mu       sync.Mutex
	installs map[Kind]*installState
	busy     map[string]string // database -> running long operation ("backup", "restore")
}

type installState struct {
	running bool
	err     string
}

// New returns a Manager.
func New(st *store.Store, dataDir string, set func() settings.Settings) *Manager {
	return &Manager{Store: st, DataDir: dataDir, Settings: set, installs: map[Kind]*installState{}, busy: map[string]string{}}
}

// DBRoot is the directory that holds every instance.
func (m *Manager) DBRoot() string { return filepath.Join(m.DataDir, "databases") }

// Instance resolves a database record to its on-disk instance.
func (m *Manager) Instance(d store.Database) Instance {
	return Instance{Database: d, Dir: filepath.Join(m.DBRoot(), d.Name), RunUser: m.Settings().DBUser}
}

func (m *Manager) logPath(name string) string {
	return filepath.Join(m.DataDir, "logs", "db-"+name+".log")
}

// LogPath is the provisioning/operations log of a database.
func (m *Manager) LogPath(name string) string { return m.logPath(name) }

func (m *Manager) unitFile(name string) string {
	return filepath.Join(m.Settings().UnitDir, systemd.DBUnitName(name))
}

func (m *Manager) record(kind, name, msg string) { m.Store.AddEvent(kind, name, msg) }

// ---- engines available on this server ----

// KindStatus tells whether an engine can be provisioned right now.
type KindStatus struct {
	Spec
	Installed  bool   `json:"installed"`
	Missing    string `json:"missing,omitempty"`
	CanInstall bool   `json:"can_install"` // apt-get present and the distro ships the packages
	Installing bool   `json:"installing"`
}

// Kinds lists every engine with its availability.
func (m *Manager) Kinds() []KindStatus {
	_, haveApt := exec.LookPath("apt-get")
	var out []KindStatus
	for _, sp := range specs {
		e, _ := engineOf(sp.Kind)
		_, err := e.locate()
		ks := KindStatus{Spec: sp, Installed: err == nil, CanInstall: haveApt == nil && len(sp.Packages) > 0}
		if err != nil {
			ks.Missing = strings.TrimPrefix(err.Error(), "faltam programas: ")
		}
		m.mu.Lock()
		if st := m.installs[sp.Kind]; st != nil {
			ks.Installing = st.running
		}
		m.mu.Unlock()
		out = append(out, ks)
	}
	return out
}

// Version returns the engine version, or "" when it is not installed.
func (m *Manager) Version(k Kind) string {
	e, err := engineOf(k)
	if err != nil {
		return ""
	}
	b, err := e.locate()
	if err != nil {
		return ""
	}
	return e.version(b)
}

// ---- installing engines (apt) ----

var defaultUnits = map[Kind][]string{
	Postgres: {"postgresql"},
	MySQL:    {"mariadb", "mysql"},
	Redis:    {"redis-server", "redis"},
}

// InstallLogPath is the log of the install job of a kind.
func (m *Manager) InstallLogPath(k Kind) string {
	return filepath.Join(m.DataDir, "logs", "install-"+string(k)+".log")
}

// InstallState reports a running or finished install job.
func (m *Manager) InstallState(k Kind) (running bool, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.installs[k]; st != nil {
		return st.running, st.err
	}
	return false, ""
}

// Install installs the engine's packages with apt-get in the background. The distribution's own
// auto-started instance is disabled: the panel runs dedicated instances instead.
func (m *Manager) Install(k Kind) error {
	sp, ok := SpecOf(k)
	if !ok || len(sp.Packages) == 0 {
		return errors.New("este banco não pode ser instalado automaticamente")
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		return errors.New("instalação automática só existe para Debian/Ubuntu (apt-get); instale os pacotes manualmente")
	}
	m.mu.Lock()
	if st := m.installs[k]; st != nil && st.running {
		m.mu.Unlock()
		return errors.New("a instalação já está em andamento")
	}
	m.installs[k] = &installState{running: true}
	m.mu.Unlock()
	go func() {
		err := m.runInstall(k, sp)
		m.mu.Lock()
		m.installs[k].running = false
		if err != nil {
			m.installs[k].err = err.Error()
		}
		m.mu.Unlock()
		if err != nil {
			m.record("database", string(k), "Falha ao instalar "+sp.Label)
		} else {
			m.record("database", string(k), sp.Label+" instalado")
		}
	}()
	return nil
}

func (m *Manager) runInstall(k Kind, sp Spec) error {
	lf, err := os.OpenFile(m.InstallLogPath(k), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer lf.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	fmt.Fprintf(lf, "== instalando %s ==\n", sp.Label)
	if err := execx.Run(ctx, lf, "apt-get", "update"); err != nil {
		return err
	}
	args := append([]string{"DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y"}, sp.Packages...)
	if err := execx.Run(ctx, lf, "env", args...); err != nil {
		return err
	}
	for _, u := range defaultUnits[k] { // best effort: the unit may not exist
		_ = execx.Run(ctx, io.Discard, "systemctl", "disable", "--now", u)
	}
	fmt.Fprintln(lf, "\n== instalação concluída ==")
	return nil
}

// ---- creating ----

func defaultUser(k Kind) string {
	switch k {
	case Postgres:
		return "postgres"
	case MySQL:
		return "root"
	case MongoDB:
		return "admin"
	}
	return "default"
}

func defaultDB(k Kind) string {
	if k == Redis {
		return ""
	}
	return "app"
}

// pickPort returns the first free port from base, skipping ports of other apps/databases and busy ones.
func (m *Manager) pickPort(base int) int {
	used := m.Store.UsedPorts()
	for p := base; p < base+500; p++ {
		if used[p] {
			continue
		}
		if l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", p)); err == nil {
			l.Close()
			return p
		}
	}
	return 0
}

// Create registers a database and provisions it in the background.
func (m *Manager) Create(name string, k Kind) (store.Database, error) {
	if err := validate.Name(name); err != nil {
		return store.Database{}, err
	}
	e, err := engineOf(k)
	if err != nil {
		return store.Database{}, err
	}
	if _, err := e.locate(); err != nil {
		return store.Database{}, fmt.Errorf("%s não está instalado (%w)", k, err)
	}
	set := m.Settings()
	if err := settings.EnsureAppUser(set.DBUser); err != nil {
		return store.Database{}, err
	}
	for _, d := range []string{m.DBRoot()} { // the service user must be able to traverse down to its own directory
		if err := os.MkdirAll(d, 0o711); err != nil {
			return store.Database{}, err
		}
		_ = os.Chmod(d, 0o711)
	}
	sp, _ := SpecOf(k)
	d := store.Database{Name: name, Kind: string(k), Port: m.pickPort(sp.BasePort), Username: defaultUser(k), Password: GeneratePassword(), DBName: defaultDB(k)}
	if d.Port == 0 {
		return d, errors.New("não há porta livre para o banco")
	}
	if err := m.Store.CreateDatabase(d); err != nil {
		if errors.Is(err, store.ErrExists) {
			return d, errors.New("já existe uma aplicação ou banco com esse nome")
		}
		return d, err
	}
	m.record("database", name, fmt.Sprintf("Criando %s", sp.Label))
	go m.provision(m.Instance(d))
	return d, nil
}

func (m *Manager) runnerFor(inst Instance) runner { return runner{runUser: inst.RunUser} }

func (m *Manager) provision(inst Instance) {
	lf, err := os.OpenFile(m.logPath(inst.Name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		m.Store.SetDatabaseStatus(inst.Name, store.DBFailed)
		return
	}
	defer lf.Close()
	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()
	fmt.Fprintf(lf, "== provisionando %s (%s) em %s ==\n", inst.Name, inst.Kind, time.Now().Format(time.RFC3339))
	if err := m.doProvision(ctx, inst, lf); err != nil {
		fmt.Fprintf(lf, "\n== FALHOU: %v ==\n", err)
		m.Store.SetDatabaseStatus(inst.Name, store.DBFailed)
		m.record("database", inst.Name, "Falha ao provisionar: "+firstLine(err.Error()))
		return
	}
	fmt.Fprintln(lf, "\n== banco pronto ==")
	m.Store.SetDatabaseStatus(inst.Name, store.DBReady)
	m.record("database", inst.Name, "Banco pronto")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (m *Manager) doProvision(ctx context.Context, inst Instance, log io.Writer) error {
	e, err := engineOf(Kind(inst.Kind))
	if err != nil {
		return err
	}
	bins, err := e.locate()
	if err != nil {
		return err
	}
	r := m.runnerFor(inst)
	for _, d := range []string{inst.Dir, inst.DataDir(), inst.RunDir()} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return err
		}
		if err := chownTo(d, inst.RunUser); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(inst.BackupDir(), 0o700); err != nil {
		return err
	}
	// postgres insists on 0700/0750 data directories
	_ = os.Chmod(inst.DataDir(), 0o700)

	if empty, _ := dirEmpty(inst.DataDir()); empty {
		if err := e.init(ctx, r, inst, bins, log); err != nil {
			return err
		}
	}
	if err := e.configure(inst, bins); err != nil {
		return err
	}
	if err := m.writeUnit(inst, e, bins); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", systemd.DBUnitName(inst.Name)}, {"restart", systemd.DBUnitName(inst.Name)}} {
		if err := execx.Run(ctx, log, "systemctl", args...); err != nil {
			return fmt.Errorf("systemctl %s: %w", args[0], err)
		}
	}
	fmt.Fprintf(log, "aguardando o banco na porta %d…\n", inst.Port)
	if err := waitTCP(ctx, fmt.Sprintf("127.0.0.1:%d", inst.Port), 120*time.Second); err != nil {
		return err
	}
	return e.postStart(ctx, r, inst, bins, log)
}

func dirEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	return len(entries) == 0, err
}

func (m *Manager) writeUnit(inst Instance, e engine, bins any) error {
	u := systemd.Unit{App: inst.Name, Desc: "BR Panel database " + inst.Name, WorkDir: inst.Dir,
		StartCmd: e.execStart(inst, bins), User: inst.RunUser, Extra: e.extraUnit()}
	if err := os.MkdirAll(filepath.Dir(m.unitFile(inst.Name)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.unitFile(inst.Name), []byte(u.Render()), 0o644)
}

// ---- lifecycle ----

func (m *Manager) get(name string) (Instance, engine, any, error) {
	d, ok := m.Store.GetDatabase(name)
	if !ok {
		return Instance{}, nil, nil, errors.New("banco não encontrado")
	}
	e, err := engineOf(Kind(d.Kind))
	if err != nil {
		return Instance{}, nil, nil, err
	}
	bins, err := e.locate()
	if err != nil {
		return Instance{}, nil, nil, err
	}
	return m.Instance(d), e, bins, nil
}

// Control runs systemctl start|stop|restart on the database's unit.
func (m *Manager) Control(name, action string) error {
	if _, ok := m.Store.GetDatabase(name); !ok {
		return errors.New("banco não encontrado")
	}
	if action != "start" && action != "stop" && action != "restart" {
		return errors.New("ação inválida")
	}
	if out, err := systemd.Ctl(action, systemd.DBUnitName(name)); err != nil {
		return fmt.Errorf("%s", out)
	}
	m.record("database", name, map[string]string{"start": "Banco iniciado", "stop": "Banco parado", "restart": "Banco reiniciado"}[action])
	return nil
}

// Delete stops the service and removes the unit, the data and the record.
func (m *Manager) Delete(name string) error {
	if _, ok := m.Store.GetDatabase(name); !ok {
		return errors.New("banco não encontrado")
	}
	_, _ = systemd.Ctl("disable", "--now", systemd.DBUnitName(name))
	if err := os.Remove(m.unitFile(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, _ = systemd.Ctl("daemon-reload")
	if err := os.RemoveAll(filepath.Join(m.DBRoot(), name)); err != nil {
		return err
	}
	_ = os.Remove(m.logPath(name))
	if err := m.Store.DeleteDatabase(name); err != nil {
		return err
	}
	m.record("database", name, "Banco excluído")
	return nil
}

// RegeneratePassword sets a new random password on the server and in the panel.
func (m *Manager) RegeneratePassword(name string) error {
	inst, e, bins, err := m.get(name)
	if err != nil {
		return err
	}
	newPw := GeneratePassword()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := e.setPassword(ctx, m.runnerFor(inst), inst, bins, newPw); err != nil {
		return fmt.Errorf("não foi possível trocar a senha: %w", err)
	}
	if err := m.Store.SetDatabasePassword(name, newPw); err != nil {
		return err
	}
	inst.Password = newPw
	if err := e.configure(inst, bins); err != nil {
		return err
	}
	m.record("database", name, "Senha regenerada")
	return nil
}

// SetPublic toggles listening on all interfaces and restarts the service.
func (m *Manager) SetPublic(name string, public bool) error {
	inst, e, bins, err := m.get(name)
	if err != nil {
		return err
	}
	if inst.Public == public {
		return nil
	}
	if err := m.Store.SetDatabasePublic(name, public); err != nil {
		return err
	}
	inst.Public = public
	if err := e.configure(inst, bins); err != nil {
		return err
	}
	if err := m.writeUnit(inst, e, bins); err != nil {
		return err
	}
	_, _ = systemd.Ctl("daemon-reload")
	if out, err := systemd.Ctl("restart", systemd.DBUnitName(name)); err != nil {
		return fmt.Errorf("%s", out)
	}
	if public {
		m.record("database", name, "Acesso externo ativado")
	} else {
		m.record("database", name, "Acesso externo desativado")
	}
	return nil
}

// ---- connection info ----

// Connection is one line of the "Conectar" tab.
type Connection struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

// PublicHost is the address external clients should use.
func (m *Manager) PublicHost() string {
	if d := m.Settings().PanelDomain; d != "" {
		return d
	}
	if c, err := net.Dial("udp", "198.51.100.1:80"); err == nil { // no packet is sent; picks the outbound interface
		defer c.Close()
		if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
			return a.IP.String()
		}
	}
	return "SEU-SERVIDOR"
}

// Connections lists the connection strings and parts of a database.
func (m *Manager) Connections(d store.Database) []Connection {
	e, err := engineOf(Kind(d.Kind))
	if err != nil {
		return nil
	}
	inst := Instance{Database: d}
	out := []Connection{{Label: "URL privada (apps neste servidor)", Value: e.url(inst, PrivateHost), Secret: true}}
	if d.Public {
		out = append(out, Connection{Label: "URL pública", Value: e.url(inst, m.PublicHost()), Secret: true})
	}
	out = append(out,
		Connection{Label: "Host", Value: PrivateHost},
		Connection{Label: "Porta", Value: fmt.Sprint(d.Port)},
		Connection{Label: "Usuário", Value: d.Username})
	if d.DBName != "" {
		out = append(out, Connection{Label: "Banco", Value: d.DBName})
	}
	out = append(out, Connection{Label: "Senha", Value: d.Password, Secret: true})
	return out
}

// ConsoleCommand is a ready-to-paste command line client invocation.
func (m *Manager) ConsoleCommand(d store.Database) string {
	e, err := engineOf(Kind(d.Kind))
	if err != nil {
		return ""
	}
	u := e.url(Instance{Database: d}, PrivateHost)
	switch Kind(d.Kind) {
	case Postgres:
		return fmt.Sprintf("psql %q", u)
	case MongoDB:
		return fmt.Sprintf("mongosh %q", u)
	case Redis:
		return fmt.Sprintf("redis-cli -u %q", u)
	}
	return fmt.Sprintf("mariadb -h %s -P %d -u %s -p %s", PrivateHost, d.Port, d.Username, d.DBName)
}

// ---- data browsing ----

// Tables lists tables / collections / keys.
func (m *Manager) Tables(ctx context.Context, name string) ([]string, error) {
	inst, e, bins, err := m.get(name)
	if err != nil {
		return nil, err
	}
	return e.tables(ctx, m.runnerFor(inst), inst, bins)
}

// Preview returns the first rows of a table / collection / key.
func (m *Manager) Preview(ctx context.Context, name, item string) (Preview, error) {
	inst, e, bins, err := m.get(name)
	if err != nil {
		return Preview{}, err
	}
	return e.preview(ctx, m.runnerFor(inst), inst, bins, item)
}

// ---- backups ----

// BackupInfo describes one backup file.
type BackupInfo struct {
	File string    `json:"file"`
	Size int64     `json:"size"`
	Time time.Time `json:"time"`
}

var backupFileRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}\.[a-z.]{2,16}$`)

// ValidBackupFile reports whether name looks like a file produced by Backup.
func ValidBackupFile(name string) bool { return backupFileRe.MatchString(name) }

// Backups lists a database's backups, newest first.
func (m *Manager) Backups(name string) []BackupInfo {
	entries, _ := os.ReadDir(filepath.Join(m.DBRoot(), name, "backups"))
	var out []BackupInfo
	for _, en := range entries {
		if !ValidBackupFile(en.Name()) {
			continue
		}
		if info, err := en.Info(); err == nil {
			out = append(out, BackupInfo{File: en.Name(), Size: info.Size(), Time: info.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File > out[j].File })
	return out
}

// BackupPath returns the path of a backup file after validating its name.
func (m *Manager) BackupPath(name, file string) (string, bool) {
	if !ValidBackupFile(file) {
		return "", false
	}
	p := filepath.Join(m.DBRoot(), name, "backups", file)
	_, err := os.Stat(p)
	return p, err == nil
}

// DeleteBackup removes one backup file.
func (m *Manager) DeleteBackup(name, file string) error {
	p, ok := m.BackupPath(name, file)
	if !ok {
		return errors.New("backup não encontrado")
	}
	return os.Remove(p)
}

// Busy returns the long operation (backup/restore) running on a database, if any.
func (m *Manager) Busy(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busy[name]
}

func (m *Manager) begin(name, op string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy[name] != "" {
		return false
	}
	m.busy[name] = op
	return true
}

func (m *Manager) end(name string) {
	m.mu.Lock()
	delete(m.busy, name)
	m.mu.Unlock()
}

// Backup creates a backup in the background (reason is "manual" or "agendado").
func (m *Manager) Backup(name, reason string) error {
	inst, e, bins, err := m.get(name)
	if err != nil {
		return err
	}
	if inst.Status != store.DBReady {
		return errors.New("o banco ainda não está pronto")
	}
	if !m.begin(name, "backup") {
		return errors.New("já há uma operação em andamento neste banco")
	}
	go func() {
		defer m.end(name)
		ctx, cancel := context.WithTimeout(context.Background(), backupTimeout)
		defer cancel()
		lf, _ := os.OpenFile(m.logPath(name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
		if lf != nil {
			defer lf.Close()
		} else {
			lf = os.Stderr
		}
		file := time.Now().Format("20060102-150405") + "." + e.backupExt()
		dst := filepath.Join(inst.BackupDir(), file)
		fmt.Fprintf(lf, "\n== backup (%s) → %s ==\n", reason, file)
		if err := e.backup(ctx, m.runnerFor(inst), inst, bins, dst, lf); err != nil {
			_ = os.Remove(dst)
			fmt.Fprintf(lf, "FALHOU: %v\n", err)
			m.record("backup", name, "Backup falhou ("+reason+")")
			return
		}
		m.prune(name)
		m.record("backup", name, "Backup concluído ("+reason+")")
	}()
	return nil
}

func (m *Manager) prune(name string) {
	b := m.Backups(name)
	for i := keepBackups; i < len(b); i++ {
		_ = os.Remove(filepath.Join(m.DBRoot(), name, "backups", b[i].File))
	}
}

// Restore replaces the database contents with a backup, in the background.
func (m *Manager) Restore(name, file string) error {
	inst, e, bins, err := m.get(name)
	if err != nil {
		return err
	}
	path, ok := m.BackupPath(name, file)
	if !ok {
		return errors.New("backup não encontrado")
	}
	if !m.begin(name, "restore") {
		return errors.New("já há uma operação em andamento neste banco")
	}
	go func() {
		defer m.end(name)
		ctx, cancel := context.WithTimeout(context.Background(), backupTimeout)
		defer cancel()
		lf, _ := os.OpenFile(m.logPath(name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
		if lf != nil {
			defer lf.Close()
		} else {
			lf = os.Stderr
		}
		fmt.Fprintf(lf, "\n== restaurando %s ==\n", file)
		var rerr error
		if e.restoreOffline() {
			_, _ = systemd.Ctl("stop", systemd.DBUnitName(name))
			rerr = e.restore(ctx, m.runnerFor(inst), inst, bins, path, lf)
			_, _ = systemd.Ctl("start", systemd.DBUnitName(name))
		} else {
			rerr = e.restore(ctx, m.runnerFor(inst), inst, bins, path, lf)
		}
		if rerr != nil {
			fmt.Fprintf(lf, "FALHOU: %v\n", rerr)
			m.record("backup", name, "Restauração falhou")
			return
		}
		m.record("backup", name, "Restaurado de "+file)
	}()
	return nil
}

// RunScheduler takes scheduled backups (daily/weekly) while ctx is alive.
func (m *Manager) RunScheduler(ctx context.Context) {
	tick := time.NewTicker(15 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.scheduledBackups(time.Now())
		}
	}
}

func (m *Manager) scheduledBackups(now time.Time) {
	for _, d := range m.Store.ListDatabases() {
		interval := map[string]time.Duration{"daily": 24 * time.Hour, "weekly": 7 * 24 * time.Hour}[d.BackupSchedule]
		if interval == 0 || d.Status != store.DBReady || systemd.StateUnit(systemd.DBUnitName(d.Name)) != "active" {
			continue
		}
		b := m.Backups(d.Name)
		if len(b) > 0 && now.Sub(b[0].Time) < interval-time.Hour {
			continue
		}
		_ = m.Backup(d.Name, "agendado")
	}
}
