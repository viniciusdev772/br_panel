package dbengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/settings"
	"github.com/viniciusdev772/br_panel/internal/store"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set := settings.Default()
	set.UnitDir = t.TempDir()
	return New(st, t.TempDir(), func() settings.Settings { return set })
}

func TestValidBackupFile(t *testing.T) {
	for _, ok := range []string{"20261030-153045.dump", "20261030-153045.sql.gz", "20261030-153045.archive.gz", "20261030-153045.rdb"} {
		if !ValidBackupFile(ok) {
			t.Errorf("%s deveria ser válido", ok)
		}
	}
	for _, bad := range []string{"", "../x.dump", "20261030-153045", "20261030-153045.dump/../../etc", "a.dump", "20261030-153045.DUMP", "x/20261030-153045.dump"} {
		if ValidBackupFile(bad) {
			t.Errorf("%q deveria ser inválido", bad)
		}
	}
}

func TestBackupsListingPathAndPrune(t *testing.T) {
	m := newTestManager(t)
	dir := filepath.Join(m.DBRoot(), "pg", "backups")
	_ = os.MkdirAll(dir, 0o700)
	for i := 0; i < keepBackups+3; i++ {
		name := time.Date(2026, 10, 1+i, 2, 0, 0, 0, time.UTC).Format("20060102-150405") + ".dump"
		_ = os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600)
	}
	_ = os.WriteFile(filepath.Join(dir, "lixo.txt"), []byte("x"), 0o600)
	b := m.Backups("pg")
	if len(b) != keepBackups+3 || b[0].File < b[1].File {
		t.Fatalf("listagem: %d, mais novo primeiro? %v", len(b), b[0].File)
	}
	if _, ok := m.BackupPath("pg", "../../../etc/passwd"); ok {
		t.Error("travessia de diretório")
	}
	if p, ok := m.BackupPath("pg", b[0].File); !ok || !strings.HasPrefix(p, dir) {
		t.Errorf("%s %v", p, ok)
	}
	m.prune("pg")
	if got := m.Backups("pg"); len(got) != keepBackups || got[0].File != b[0].File {
		t.Errorf("prune manteve %d", len(got))
	}
	if err := m.DeleteBackup("pg", "20261030-000000.dump"); err == nil {
		t.Error("apagar backup inexistente deve falhar")
	}
	if err := m.DeleteBackup("pg", b[0].File); err != nil {
		t.Error(err)
	}
}

func TestPickPortAndCreateValidation(t *testing.T) {
	m := newTestManager(t)
	_ = m.Store.CreateDatabase(store.Database{Name: "a", Kind: "postgres", Port: 5433, Username: "u", Password: "p"})
	if p := m.pickPort(5433); p != 5434 {
		t.Errorf("deveria pular a porta 5433 de outro banco: %d", p)
	}
	if _, err := m.Create("../x", Postgres); err == nil {
		t.Error("nome inválido")
	}
	if _, err := m.Create("ok", Kind("oracle")); err == nil {
		t.Error("tipo desconhecido")
	}
}

func TestConnectionsAndConsole(t *testing.T) {
	m := newTestManager(t)
	d := store.Database{Name: "pg", Kind: "postgres", Port: 5433, Username: "postgres", Password: "pw", DBName: "app"}
	c := m.Connections(d)
	if c[0].Value != "postgresql://postgres:pw@127.0.0.1:5433/app" || !c[0].Secret || len(c) != 6 {
		t.Errorf("%+v", c)
	}
	d.Public = true
	c = m.Connections(d)
	if c[1].Label != "URL pública" || strings.Contains(c[1].Value, "127.0.0.1") {
		t.Errorf("URL pública: %+v", c[1])
	}
	if got := m.ConsoleCommand(d); got != `psql "postgresql://postgres:pw@127.0.0.1:5433/app"` {
		t.Errorf("%s", got)
	}
	if got := m.ConsoleCommand(store.Database{Kind: "redis", Port: 6380, Username: "default", Password: "x"}); !strings.HasPrefix(got, "redis-cli -u ") {
		t.Errorf("%s", got)
	}
}

func TestKindsReportsMissing(t *testing.T) {
	m := newTestManager(t)
	t.Setenv("PATH", t.TempDir()) // nothing installed
	for _, k := range m.Kinds() {
		if k.Installed && k.Kind != Postgres { // postgres may still be found under /usr/lib/postgresql
			t.Errorf("%s não deveria estar instalado", k.Kind)
		}
	}
	for _, k := range m.Kinds() {
		if k.Kind == MongoDB && (k.Installed || k.Missing == "" || k.CanInstall) {
			t.Errorf("mongodb: %+v", k)
		}
	}
	if err := m.Install(MongoDB); err == nil {
		t.Error("MongoDB não tem instalação automática")
	}
}

func TestEngineCommandLines(t *testing.T) {
	inst := Instance{Database: store.Database{Name: "x", Port: 5433, Username: "postgres", Password: "pw", DBName: "app"}, Dir: "/d/x", RunUser: "brdb"}
	pg := pgEngine{}.execStart(inst, pgBins{dir: "/usr/lib/postgresql/16/bin"})
	for _, want := range []string{"/usr/lib/postgresql/16/bin/postgres -D /d/x/data", "listen_addresses=127.0.0.1", "port=5433", "unix_socket_directories=/d/x/run"} {
		if !strings.Contains(pg, want) {
			t.Errorf("postgres: faltou %q em %s", want, pg)
		}
	}
	if strings.Contains(pg, "pw") {
		t.Error("a senha não pode aparecer na linha de comando do serviço")
	}
	inst.Public = true
	if !strings.Contains(pgEngine{}.execStart(inst, pgBins{}), "listen_addresses=0.0.0.0") {
		t.Error("acesso público deve escutar em 0.0.0.0")
	}
	my := mysqlEngine{}.execStart(inst, mysqlBins{server: "/usr/sbin/mariadbd"})
	if !strings.Contains(my, "--no-defaults") || !strings.Contains(my, "--bind-address=0.0.0.0") || strings.Contains(my, "pw") {
		t.Errorf("mysql: %s", my)
	}
	mg := mongoEngine{}.execStart(inst, mongoBins{server: "/usr/bin/mongod"})
	if !strings.Contains(mg, "--auth") || strings.Contains(mg, "pw") {
		t.Errorf("mongo: %s", mg)
	}
	rd := redisEngine{}.execStart(inst, redisBins{server: "/usr/bin/redis-server"})
	if rd != "/usr/bin/redis-server /d/x/redis.conf" {
		t.Errorf("redis: %s", rd)
	}
}

func TestFirstVersion(t *testing.T) {
	for in, want := range map[string]string{
		"postgres (PostgreSQL) 16.15 (Ubuntu 16.15-0ubuntu0.24.04.1)":          "16.15",
		"mariadbd  Ver 10.11.14-MariaDB-0ubuntu0.24.04.1 for debian-linux-gnu": "10.11.14",
		"Redis server v=7.0.15 sha=00000000:0 malloc=jemalloc-5.3.0 bits=64":   "7.0.15",
		"db version v7.0.2": "7.0.2",
		"sem versão":        "",
	} {
		if got := firstVersion(in); got != want {
			t.Errorf("%q => %q", in, got)
		}
	}
}
