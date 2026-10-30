package dbengine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type pgEngine struct{}

type pgBins struct{ dir string }

func (b pgBins) tool(n string) string { return filepath.Join(b.dir, n) }

func (pgEngine) locate() (any, error) {
	var dirs []string
	if m, _ := filepath.Glob("/usr/lib/postgresql/*/bin"); len(m) > 0 {
		sort.Slice(m, func(i, j int) bool { return versionKey(m[i]) > versionKey(m[j]) })
		dirs = m
	}
	for _, d := range dirs {
		b := pgBins{dir: d}
		if _, err := os.Stat(b.tool("initdb")); err == nil {
			return b, missingList(map[string]string{
				"postgres": which(nil, b.tool("postgres")), "initdb": which(nil, b.tool("initdb")),
				"psql": which(nil, b.tool("psql")), "pg_dump": which(nil, b.tool("pg_dump")), "pg_restore": which(nil, b.tool("pg_restore")),
			})
		}
	}
	// distributions that put the binaries in PATH (Fedora/Arch/Alpine)
	if p := which(nil, "initdb"); p != "" {
		b := pgBins{dir: filepath.Dir(p)}
		return b, missingList(map[string]string{"postgres": which([]string{b.dir}, "postgres"), "psql": which([]string{b.dir}, "psql"),
			"pg_dump": which([]string{b.dir}, "pg_dump"), "pg_restore": which([]string{b.dir}, "pg_restore")})
	}
	return pgBins{}, fmt.Errorf("faltam programas: initdb, postgres, psql, pg_dump, pg_restore")
}

// versionKey orders /usr/lib/postgresql/<major>/bin paths numerically.
func versionKey(p string) int {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if s == "postgresql" && i+1 < len(parts) {
			n, _ := strconv.Atoi(parts[i+1])
			return n
		}
	}
	return 0
}

func (pgEngine) version(b any) string {
	out, err := runner{}.out(context.Background(), nil, b.(pgBins).tool("postgres"), "--version")
	if err != nil {
		return ""
	}
	return firstVersion(out)
}

func (pgEngine) restoreOffline() bool { return false }
func (pgEngine) backupExt() string    { return "dump" }
func (pgEngine) extraUnit() []string {
	return []string{"KillSignal=SIGINT", "TimeoutStopSec=120", "LimitNOFILE=65536"}
}

func (pgEngine) init(ctx context.Context, r runner, inst Instance, b any, log io.Writer) error {
	pw := filepath.Join(inst.Dir, ".pwfile")
	if err := writeOwned(pw, []byte(inst.Password+"\n"), 0o600, inst.RunUser); err != nil {
		return err
	}
	defer os.Remove(pw)
	return r.run(ctx, log, true, []string{"LC_ALL=C.UTF-8"}, b.(pgBins).tool("initdb"),
		"-D", inst.DataDir(), "-U", inst.Username, "--pwfile="+pw, "-E", "UTF8",
		"--auth-local=scram-sha-256", "--auth-host=scram-sha-256", "--no-instructions")
}

const pgHBAMarker = "# brpanel: public access"

// configure keeps pg_hba.conf in sync with the public flag.
func (pgEngine) configure(inst Instance, _ any) error {
	path := filepath.Join(inst.DataDir(), "pg_hba.conf")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) { // before initdb
			return nil
		}
		return err
	}
	var kept []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.Contains(l, pgHBAMarker) {
			continue
		}
		kept = append(kept, l)
	}
	out := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"
	if inst.Public {
		out += "host all all 0.0.0.0/0 scram-sha-256 " + pgHBAMarker + "\n"
	}
	return os.WriteFile(path, []byte(out), 0o600)
}

func (pgEngine) execStart(inst Instance, b any) string {
	return fmt.Sprintf("%s -D %s -c listen_addresses=%s -c port=%d -c unix_socket_directories=%s -c max_connections=100",
		b.(pgBins).tool("postgres"), inst.DataDir(), inst.BindAddr(), inst.Port, inst.RunDir())
}

func (pgEngine) env(inst Instance) []string {
	return []string{"PGPASSWORD=" + inst.Password, "PGCONNECT_TIMEOUT=10"}
}

func (e pgEngine) psql(ctx context.Context, r runner, inst Instance, b any, db string, args ...string) (string, error) {
	base := []string{"-h", "127.0.0.1", "-p", strconv.Itoa(inst.Port), "-U", inst.Username, "-d", db, "-X", "-At", "-v", "ON_ERROR_STOP=1"}
	return r.out(ctx, e.env(inst), b.(pgBins).tool("psql"), append(base, args...)...)
}

func (e pgEngine) postStart(ctx context.Context, r runner, inst Instance, b any, log io.Writer) error {
	have, err := e.psql(ctx, r, inst, b, "postgres", "-c", "SELECT 1 FROM pg_database WHERE datname = '"+inst.DBName+"'")
	if err != nil {
		return err
	}
	if have == "1" {
		return nil
	}
	fmt.Fprintf(log, "$ psql -c CREATE DATABASE %s\n", inst.DBName)
	_, err = e.psql(ctx, r, inst, b, "postgres", "-c", `CREATE DATABASE "`+inst.DBName+`"`)
	return err
}

func (pgEngine) url(inst Instance, host string) string {
	return fmt.Sprintf("postgresql://%s:%s@%s:%d/%s", inst.Username, inst.Password, host, inst.Port, inst.DBName)
}

func (e pgEngine) backup(ctx context.Context, r runner, inst Instance, b any, dst string, log io.Writer) error {
	return r.run(ctx, log, false, e.env(inst), b.(pgBins).tool("pg_dump"),
		"-h", "127.0.0.1", "-p", strconv.Itoa(inst.Port), "-U", inst.Username, "-Fc", "-f", dst, inst.DBName)
}

func (e pgEngine) restore(ctx context.Context, r runner, inst Instance, b any, src string, log io.Writer) error {
	// --clean/--if-exists replaces existing objects; errors on a few harmless objects are tolerated by pg_restore's exit code 1
	err := r.run(ctx, log, false, e.env(inst), b.(pgBins).tool("pg_restore"),
		"-h", "127.0.0.1", "-p", strconv.Itoa(inst.Port), "-U", inst.Username, "-d", inst.DBName,
		"--clean", "--if-exists", "--no-owner", "--exit-on-error", src)
	return err
}

func (e pgEngine) setPassword(ctx context.Context, r runner, inst Instance, b any, newPassword string) error {
	_, err := e.psql(ctx, r, inst, b, "postgres", "-c", fmt.Sprintf(`ALTER USER "%s" PASSWORD '%s'`, inst.Username, newPassword))
	return err
}

func (e pgEngine) tables(ctx context.Context, r runner, inst Instance, b any) ([]string, error) {
	out, err := e.psql(ctx, r, inst, b, inst.DBName, "-c",
		`SELECT table_schema || '.' || table_name FROM information_schema.tables
		 WHERE table_schema NOT IN ('pg_catalog','information_schema') AND table_type = 'BASE TABLE' ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

func splitLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

func (e pgEngine) preview(ctx context.Context, r runner, inst Instance, b any, name string) (Preview, error) {
	schema, table, ok := strings.Cut(name, ".")
	if !ok || !identRe.MatchString(schema) || !identRe.MatchString(table) {
		return Preview{}, fmt.Errorf("nome de tabela inválido")
	}
	qual := fmt.Sprintf(`"%s"."%s"`, schema, table)
	cols, err := e.psql(ctx, r, inst, b, inst.DBName, "-c",
		fmt.Sprintf(`SELECT column_name FROM information_schema.columns WHERE table_schema='%s' AND table_name='%s' ORDER BY ordinal_position`, schema, table))
	if err != nil {
		return Preview{}, err
	}
	total, _ := e.psql(ctx, r, inst, b, inst.DBName, "-c", "SELECT count(*) FROM "+qual)
	data, err := e.psql(ctx, r, inst, b, inst.DBName, "-c",
		fmt.Sprintf(`SELECT COALESCE(json_agg(t), '[]'::json) FROM (SELECT * FROM %s LIMIT 50) t`, qual))
	if err != nil {
		return Preview{}, err
	}
	p := Preview{Columns: splitLines(cols), Total: -1}
	p.Total, _ = strconv.Atoi(total)
	var rows []map[string]any
	if err := json.Unmarshal([]byte(data), &rows); err != nil {
		return Preview{}, err
	}
	for _, row := range rows {
		line := make([]string, len(p.Columns))
		for i, c := range p.Columns {
			line[i] = cell(row[c])
		}
		p.Rows = append(p.Rows, line)
	}
	return p, nil
}

// cell renders a JSON value as a table cell.
func cell(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}
