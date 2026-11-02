// Package dbengine provisions and operates database instances (PostgreSQL, MySQL/MariaDB,
// MongoDB, Redis) as dedicated systemd services, each with its own data directory, port and
// credentials — the native-Linux counterpart of "add a database" in container platforms.
package dbengine

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
)

// Kind identifies a database engine.
type Kind string

// Supported engines.
const (
	Postgres Kind = "postgres"
	MySQL    Kind = "mysql"
	MongoDB  Kind = "mongodb"
	Redis    Kind = "redis"
)

// Spec describes an engine for the UI and the installer.
type Spec struct {
	Kind        Kind     `json:"kind"`
	Label       string   `json:"label"`
	Badge       string   `json:"badge"` // two-letter badge shown on cards
	Color       string   `json:"color"` // brand-like badge color
	Description string   `json:"description"`
	BasePort    int      `json:"base_port"`
	Packages    []string `json:"packages"` // apt packages; empty when the distro has none
	InstallNote string   `json:"install_note"`
	URLVar      string   `json:"url_var"` // name of the connection-string variable
}

var specs = []Spec{
	{Kind: Postgres, Label: "PostgreSQL", Badge: "Pg", Color: "#336791", BasePort: 5433, Packages: []string{"postgresql"}, URLVar: "DATABASE_URL",
		Description: "Banco relacional robusto, ótimo padrão para a maioria dos apps."},
	{Kind: MySQL, Label: "MySQL / MariaDB", Badge: "My", Color: "#00758f", BasePort: 3307, Packages: []string{"mariadb-server"}, URLVar: "DATABASE_URL",
		Description: "Banco relacional popular em PHP, WordPress e muitos frameworks."},
	{Kind: MongoDB, Label: "MongoDB", Badge: "Mg", Color: "#47a248", BasePort: 27018, URLVar: "MONGO_URL",
		InstallNote: "O MongoDB não está nos repositórios padrão do Ubuntu/Debian. Instale o pacote mongodb-org e o mongosh seguindo a documentação oficial; o painel detecta automaticamente.",
		Description: "Banco de documentos JSON, esquema flexível."},
	{Kind: Redis, Label: "Redis", Badge: "Rd", Color: "#dc382d", BasePort: 6380, Packages: []string{"redis-server"}, URLVar: "REDIS_URL",
		Description: "Cache e fila em memória com persistência opcional."},
}

// Specs lists every supported engine.
func Specs() []Spec { return append([]Spec(nil), specs...) }

// SpecOf returns the spec of a kind.
func SpecOf(k Kind) (Spec, bool) {
	for _, s := range specs {
		if s.Kind == k {
			return s, true
		}
	}
	return Spec{}, false
}

// Instance is a database record plus where it lives on disk.
type Instance struct {
	store.Database
	Dir     string // <data>/databases/<name>
	RunUser string // unprivileged system user that owns the files and runs the service
}

func (i Instance) DataDir() string   { return filepath.Join(i.Dir, "data") }
func (i Instance) RunDir() string    { return filepath.Join(i.Dir, "run") }
func (i Instance) BackupDir() string { return filepath.Join(i.Dir, "backups") }

// BindAddr is the interface the server listens on.
func (i Instance) BindAddr() string {
	if i.Public {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

// Preview is a read-only slice of a table, collection or key.
type Preview struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Total   int        `json:"total"` // -1 when unknown
}

// engine is implemented once per kind.
type engine interface {
	// locate finds the server and client programs; the error lists what is missing.
	locate() (any, error)
	version(b any) string
	// configure writes config files derived from the instance (idempotent): called on
	// provisioning, when the password changes and when public access is toggled.
	configure(inst Instance, b any) error
	// init prepares an empty data directory (before the first start).
	init(ctx context.Context, r runner, inst Instance, b any, log io.Writer) error
	// execStart is the service command line (run through /bin/sh -c "exec …").
	execStart(inst Instance, b any) string
	extraUnit() []string
	// postStart runs once the server accepts connections (create database/user).
	postStart(ctx context.Context, r runner, inst Instance, b any, log io.Writer) error
	url(inst Instance, host string) string
	backup(ctx context.Context, r runner, inst Instance, b any, dst string, log io.Writer) error
	restore(ctx context.Context, r runner, inst Instance, b any, src string, log io.Writer) error
	// restoreOffline engines need the service stopped while the backup is put in place.
	restoreOffline() bool
	setPassword(ctx context.Context, r runner, inst Instance, b any, newPassword string) error
	tables(ctx context.Context, r runner, inst Instance, b any) ([]string, error)
	preview(ctx context.Context, r runner, inst Instance, b any, name string) (Preview, error)
	// create adds a table / collection / key from the "Dados" tab (see Manager.Create).
	create(ctx context.Context, r runner, inst Instance, b any, item, value string) error
	backupExt() string
}

func engineOf(k Kind) (engine, error) {
	switch k {
	case Postgres:
		return pgEngine{}, nil
	case MySQL:
		return mysqlEngine{}, nil
	case MongoDB:
		return mongoEngine{}, nil
	case Redis:
		return redisEngine{}, nil
	}
	return nil, fmt.Errorf("tipo de banco desconhecido: %q", k)
}

// ---- command runner ----

// runner executes the engines' tools. Server-side programs (initdb, mariadb-install-db…) run as
// the unprivileged database user; clients run as the panel user and receive secrets only through
// environment variables or 0600 temp files, never argv.
type runner struct{ runUser string }

func (r runner) command(ctx context.Context, asUser bool, env []string, name string, args ...string) *exec.Cmd {
	if asUser && r.runUser != "" && os.Geteuid() == 0 {
		args = append([]string{"-u", r.runUser, "--", name}, args...)
		name = "runuser"
	}
	c := exec.CommandContext(ctx, name, args...)
	c.Env = append(os.Environ(), env...)
	c.WaitDelay = 5 * time.Second
	return c
}

func (r runner) run(ctx context.Context, log io.Writer, asUser bool, env []string, name string, args ...string) error {
	fmt.Fprintf(log, "$ %s %s\n", filepath.Base(name), strings.Join(args, " "))
	c := r.command(ctx, asUser, env, name, args...)
	c.Stdout, c.Stderr = log, log
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	return nil
}

// out runs a client program and returns trimmed stdout; stderr is part of the error.
func (r runner) out(ctx context.Context, env []string, name string, args ...string) (string, error) {
	c := r.command(ctx, false, env, name, args...)
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ---- helpers ----

const alnum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GeneratePassword returns a random 24-character alphanumeric password (safe in URLs, SQL and shell).
func GeneratePassword() string {
	b := make([]byte, 24)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alnum))))
		if err != nil {
			panic(err)
		}
		b[i] = alnum[n.Int64()]
	}
	return string(b)
}

// waitTCP blocks until host:port accepts connections.
func waitTCP(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			c.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s não respondeu em %s", addr, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// which finds the first existing program among names, looking in extra directories too.
func which(extraDirs []string, names ...string) string {
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
		for _, d := range extraDirs {
			p := filepath.Join(d, n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
				return p
			}
		}
	}
	return ""
}

func missingList(found map[string]string) error {
	var miss []string
	for k, v := range found {
		if v == "" {
			miss = append(miss, k)
		}
	}
	if len(miss) == 0 {
		return nil
	}
	sort.Strings(miss)
	return fmt.Errorf("faltam programas: %s", strings.Join(miss, ", "))
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]{0,62}$`)

// writeSecretFile writes a 0600 file owned by owner (when running as root and owner exists).
func writeOwned(path string, data []byte, mode os.FileMode, owner string) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	return chownTo(path, owner)
}

func gzipTo(src io.Reader, dstPath string) error {
	return gzipWrite(src, dstPath)
}
