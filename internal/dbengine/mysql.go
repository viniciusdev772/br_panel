package dbengine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type mysqlEngine struct{}

type mysqlBins struct {
	server, installDB, client, dump string
	maria                           bool
}

var mysqlExtra = []string{"/usr/sbin", "/usr/libexec", "/usr/local/mysql/bin", "/usr/local/bin"}

func (mysqlEngine) locate() (any, error) {
	b := mysqlBins{
		server:    which(mysqlExtra, "mariadbd", "mysqld"),
		installDB: which(mysqlExtra, "mariadb-install-db", "mysql_install_db"),
		client:    which(mysqlExtra, "mariadb", "mysql"),
		dump:      which(mysqlExtra, "mariadb-dump", "mysqldump"),
	}
	b.maria = strings.Contains(filepath.Base(b.server), "mariadb")
	found := map[string]string{"mariadbd/mysqld": b.server, "mariadb/mysql": b.client, "mariadb-dump/mysqldump": b.dump}
	if b.maria {
		found["mariadb-install-db"] = b.installDB
	}
	return b, missingList(found)
}

func (mysqlEngine) version(b any) string {
	out, err := runner{}.out(context.Background(), nil, b.(mysqlBins).server, "--version")
	if err != nil {
		return ""
	}
	return firstVersion(out)
}

func (mysqlEngine) restoreOffline() bool { return false }
func (mysqlEngine) backupExt() string    { return "sql.gz" }
func (mysqlEngine) extraUnit() []string  { return []string{"TimeoutStopSec=120", "LimitNOFILE=65536"} }

func (mysqlEngine) init(ctx context.Context, r runner, inst Instance, b any, log io.Writer) error {
	m := b.(mysqlBins)
	if m.maria {
		return r.run(ctx, log, true, nil, m.installDB, "--no-defaults", "--datadir="+inst.DataDir(),
			"--auth-root-authentication-method=normal", "--skip-test-db")
	}
	return r.run(ctx, log, true, nil, m.server, "--no-defaults", "--initialize-insecure", "--datadir="+inst.DataDir())
}

// configure writes the init file that (re)creates the database and the admin user at each start.
func (mysqlEngine) configure(inst Instance, _ any) error {
	// mysql-install-db leaves password-less root@127.0.0.1 / root@::1 accounts that would shadow
	// our root@'%' for TCP clients, so they are dropped on every start.
	sql := "DROP USER IF EXISTS 'root'@'127.0.0.1';\nDROP USER IF EXISTS 'root'@'::1';\n" + fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4;\n"+
		"CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s';\n"+
		"ALTER USER '%s'@'%%' IDENTIFIED BY '%s';\n"+
		"GRANT ALL PRIVILEGES ON *.* TO '%s'@'%%' WITH GRANT OPTION;\n",
		inst.DBName, inst.Username, inst.Password, inst.Username, inst.Password, inst.Username)
	return writeOwned(filepath.Join(inst.Dir, "init.sql"), []byte(sql), 0o600, inst.RunUser)
}

func (mysqlEngine) execStart(inst Instance, b any) string {
	return fmt.Sprintf("%s --no-defaults --datadir=%s --port=%d --bind-address=%s --socket=%s --pid-file=%s --skip-name-resolve "+
		"--character-set-server=utf8mb4 --max-connections=100 --init-file=%s",
		b.(mysqlBins).server, inst.DataDir(), inst.Port, inst.BindAddr(),
		filepath.Join(inst.RunDir(), "mysqld.sock"), filepath.Join(inst.RunDir(), "mysqld.pid"), filepath.Join(inst.Dir, "init.sql"))
}

func (mysqlEngine) postStart(context.Context, runner, Instance, any, io.Writer) error { return nil }

func (mysqlEngine) url(inst Instance, host string) string {
	return fmt.Sprintf("mysql://%s:%s@%s:%d/%s", inst.Username, inst.Password, host, inst.Port, inst.DBName)
}

func (mysqlEngine) env(inst Instance) []string { return []string{"MYSQL_PWD=" + inst.Password} }

func (e mysqlEngine) clientArgs(inst Instance, extra ...string) []string {
	return append([]string{"--no-defaults", "-h", "127.0.0.1", "-P", strconv.Itoa(inst.Port), "-u", inst.Username}, extra...)
}

func (e mysqlEngine) backup(ctx context.Context, r runner, inst Instance, b any, dst string, log io.Writer) error {
	tmp := dst + ".tmp"
	defer os.Remove(tmp)
	err := r.run(ctx, log, false, e.env(inst), b.(mysqlBins).dump, e.clientArgs(inst,
		"--single-transaction", "--routines", "--triggers", "--result-file="+tmp, inst.DBName)...)
	if err != nil {
		return err
	}
	f, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer f.Close()
	return gzipTo(f, dst)
}

func (e mysqlEngine) restore(ctx context.Context, r runner, inst Instance, b any, src string, log io.Writer) error {
	in, err := gunzipOpen(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fmt.Fprintf(log, "$ mariadb < %s\n", filepath.Base(src))
	c := r.command(ctx, false, e.env(inst), b.(mysqlBins).client, e.clientArgs(inst, inst.DBName)...)
	c.Stdin, c.Stdout, c.Stderr = in, log, log
	return c.Run()
}

func (e mysqlEngine) setPassword(ctx context.Context, r runner, inst Instance, b any, newPassword string) error {
	_, err := r.out(ctx, e.env(inst), b.(mysqlBins).client,
		e.clientArgs(inst, "-e", fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY '%s'", inst.Username, newPassword))...)
	return err
}

func (e mysqlEngine) query(ctx context.Context, r runner, inst Instance, b any, sql string) (string, error) {
	return r.out(ctx, e.env(inst), b.(mysqlBins).client, e.clientArgs(inst, "-B", "--raw", "-e", sql, inst.DBName)...)
}

func (e mysqlEngine) tables(ctx context.Context, r runner, inst Instance, b any) ([]string, error) {
	out, err := e.query(ctx, r, inst, b, "SHOW TABLES")
	if err != nil {
		return nil, err
	}
	lines := splitLines(out)
	if len(lines) > 0 { // first line is the column header
		lines = lines[1:]
	}
	return lines, nil
}

func (e mysqlEngine) preview(ctx context.Context, r runner, inst Instance, b any, name string) (Preview, error) {
	if !identRe.MatchString(name) {
		return Preview{}, fmt.Errorf("nome de tabela inválido")
	}
	out, err := e.query(ctx, r, inst, b, fmt.Sprintf("SELECT * FROM `%s` LIMIT 50", name))
	if err != nil {
		return Preview{}, err
	}
	p := Preview{Total: -1}
	lines := splitLines(out)
	if len(lines) > 0 {
		p.Columns = strings.Split(lines[0], "\t")
		for _, l := range lines[1:] {
			p.Rows = append(p.Rows, strings.Split(l, "\t"))
		}
	}
	if cnt, err := e.query(ctx, r, inst, b, fmt.Sprintf("SELECT COUNT(*) FROM `%s`", name)); err == nil {
		if ls := splitLines(cnt); len(ls) == 2 {
			p.Total, _ = strconv.Atoi(ls[1])
		}
	}
	return p, nil
}
