package dbengine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/settings"
	"github.com/viniciusdev772/br_panel/internal/store"
)

// Integration tests drive real PostgreSQL / MariaDB / Redis servers through the manager, using a
// stand-in for systemctl. They need root and the server packages; otherwise they are skipped.

func itManager(t *testing.T, k Kind) *Manager {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("precisa de root")
	}
	if _, err := exec.LookPath("runuser"); err != nil {
		t.Skip("runuser ausente")
	}
	e, _ := engineOf(k)
	if _, err := e.locate(); err != nil {
		t.Skipf("%s não instalado: %v", k, err)
	}
	base, err := os.MkdirTemp("", "dbit-")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(base, 0o755)
	state := filepath.Join(base, "state")
	units := filepath.Join(base, "units")
	data := filepath.Join(base, "data")
	for _, d := range []string{state, units, data, filepath.Join(data, "logs")} {
		_ = os.MkdirAll(d, 0o755)
	}
	_ = os.Chmod(state, 0o777)
	shim := filepath.Join(base, "bin")
	_ = os.MkdirAll(shim, 0o755)
	src, _ := os.ReadFile("testdata/fake-systemctl")
	_ = os.WriteFile(filepath.Join(shim, "systemctl"), src, 0o755)
	t.Setenv("PATH", shim+":"+os.Getenv("PATH"))
	t.Setenv("FAKE_UNITS", units)
	t.Setenv("FAKE_STATE", state)

	st, err := store.Open(filepath.Join(base, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	set := settings.Default()
	set.UnitDir, set.DBUser = units, "brdbtest"
	m := New(st, data, func() settings.Settings { return set })
	t.Cleanup(func() {
		for _, d := range st.ListDatabases() {
			_ = m.Delete(d.Name)
		}
		st.Close()
		_ = os.RemoveAll(base)
		_ = exec.Command("userdel", "brdbtest").Run()
	})
	return m
}

func waitReady(t *testing.T, m *Manager, name string) {
	t.Helper()
	deadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(deadline) {
		d, _ := m.Store.GetDatabase(name)
		switch d.Status {
		case store.DBReady:
			return
		case store.DBFailed:
			log, _ := os.ReadFile(m.LogPath(name))
			t.Fatalf("provisionamento falhou:\n%s", log)
		}
		time.Sleep(300 * time.Millisecond)
	}
	log, _ := os.ReadFile(m.LogPath(name))
	t.Fatalf("timeout:\n%s", log)
}

func waitBackup(t *testing.T, m *Manager, name string) BackupInfo {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if m.Busy(name) == "" {
			if b := m.Backups(name); len(b) > 0 {
				return b[0]
			}
			log, _ := os.ReadFile(m.LogPath(name))
			t.Fatalf("backup não gerou arquivo:\n%s", log)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("timeout no backup")
	return BackupInfo{}
}

func waitIdle(t *testing.T, m *Manager, name string) {
	t.Helper()
	for i := 0; i < 600 && m.Busy(name) != ""; i++ {
		time.Sleep(200 * time.Millisecond)
	}
}

func TestIntegrationPostgres(t *testing.T) {
	m := itManager(t, Postgres)
	d, err := m.Create("pg", Postgres)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, m, "pg")
	e, bins, _ := func() (engine, any, error) { e, _ := engineOf(Postgres); b, err := e.locate(); return e, b, err }()
	pg := e.(pgEngine)
	inst := m.Instance(func() store.Database { x, _ := m.Store.GetDatabase("pg"); return x }())
	r := runner{}
	ctx := context.Background()

	// the connection string really works
	out, err := r.out(ctx, nil, bins.(pgBins).tool("psql"), pg.url(inst, "127.0.0.1"), "-X", "-At", "-c", "SELECT current_database()||':'||current_user")
	if err != nil || out != "app:postgres" {
		t.Fatalf("conexão: %q %v", out, err)
	}
	// wrong password is rejected
	bad := strings.Replace(pg.url(inst, "127.0.0.1"), inst.Password, "errada", 1)
	if _, err := r.out(ctx, nil, bins.(pgBins).tool("psql"), bad, "-X", "-At", "-c", "SELECT 1"); err == nil {
		t.Fatal("senha errada deveria ser recusada")
	}
	// data tab: create a table, list and preview it
	if _, err := pg.psql(ctx, r, inst, bins, "app", "-c", "CREATE TABLE clientes (id serial primary key, nome text, extra jsonb); INSERT INTO clientes (nome, extra) VALUES ('Ana','{\"a\":1}'), ('Bia', NULL)"); err != nil {
		t.Fatal(err)
	}
	tables, err := m.Tables(ctx, "pg")
	if err != nil || len(tables) != 1 || tables[0] != "public.clientes" {
		t.Fatalf("tabelas: %v %v", tables, err)
	}
	pv, err := m.Preview(ctx, "pg", "public.clientes")
	if err != nil || pv.Total != 2 || len(pv.Rows) != 2 || strings.Join(pv.Columns, ",") != "id,nome,extra" || pv.Rows[0][1] != "Ana" || pv.Rows[1][2] != "NULL" {
		t.Fatalf("preview: %+v %v", pv, err)
	}
	if _, err := m.Preview(ctx, "pg", `public.clientes"; DROP TABLE clientes; --`); err == nil {
		t.Fatal("nome de tabela malicioso deveria ser recusado")
	}
	// backup, wipe, restore
	if err := m.Backup("pg", "manual"); err != nil {
		t.Fatal(err)
	}
	bk := waitBackup(t, m, "pg")
	if bk.Size == 0 || !strings.HasSuffix(bk.File, ".dump") {
		t.Fatalf("backup: %+v", bk)
	}
	if _, err := pg.psql(ctx, r, inst, bins, "app", "-c", "DROP TABLE clientes"); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore("pg", bk.File); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	waitIdle(t, m, "pg")
	if pv, err := m.Preview(ctx, "pg", "public.clientes"); err != nil || pv.Total != 2 {
		log, _ := os.ReadFile(m.LogPath("pg"))
		t.Fatalf("restauração: %+v %v\n%s", pv, err, log)
	}
	// regenerate password: old one stops working, new one works
	oldURL := pg.url(inst, "127.0.0.1")
	if err := m.RegeneratePassword("pg"); err != nil {
		t.Fatal(err)
	}
	nd, _ := m.Store.GetDatabase("pg")
	if nd.Password == d.Password {
		t.Fatal("senha não mudou")
	}
	if _, err := r.out(ctx, nil, bins.(pgBins).tool("psql"), oldURL, "-X", "-At", "-c", "SELECT 1"); err == nil {
		t.Fatal("senha antiga deveria falhar")
	}
	if o, err := r.out(ctx, nil, bins.(pgBins).tool("psql"), pg.url(m.Instance(nd), "127.0.0.1"), "-X", "-At", "-c", "SELECT 1"); err != nil || o != "1" {
		t.Fatalf("senha nova: %q %v", o, err)
	}
	// public access toggles the bind address and restarts
	if err := m.SetPublic("pg", true); err != nil {
		t.Fatal(err)
	}
	if err := waitTCP(ctx, fmt.Sprintf("127.0.0.1:%d", nd.Port), 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if out, _ := exec.Command("ss", "-ltn").Output(); !strings.Contains(string(out), fmt.Sprintf("0.0.0.0:%d", nd.Port)) {
		unit, _ := os.ReadFile(m.unitFile("pg"))
		ps, _ := exec.Command("sh", "-c", "ps -eo pid,args | grep '[p]ostgres -D'").Output()
		t.Errorf("deveria escutar em 0.0.0.0:%d\n%s\nunit:\n%s\nps:\n%s", nd.Port, out, unit, ps)
	}
	hba, _ := os.ReadFile(filepath.Join(m.Instance(nd).DataDir(), "pg_hba.conf"))
	if !strings.Contains(string(hba), "0.0.0.0/0") {
		t.Error("pg_hba deveria liberar 0.0.0.0/0")
	}
	if err := m.SetPublic("pg", false); err != nil {
		t.Fatal(err)
	}
	hba, _ = os.ReadFile(filepath.Join(m.Instance(nd).DataDir(), "pg_hba.conf"))
	if strings.Contains(string(hba), "0.0.0.0/0") {
		t.Error("pg_hba deveria remover a regra pública")
	}
	// delete removes everything
	if err := m.Delete("pg"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.DBRoot(), "pg")); !os.IsNotExist(err) {
		t.Error("diretório deveria ser removido")
	}
	if u, err := user.Lookup("brdbtest"); err != nil || u == nil {
		t.Error("usuário de serviço deveria ter sido criado")
	}
}

func TestIntegrationRedis(t *testing.T) {
	m := itManager(t, Redis)
	if _, err := m.Create("cache", Redis); err != nil {
		t.Fatal(err)
	}
	waitReady(t, m, "cache")
	d, _ := m.Store.GetDatabase("cache")
	inst := m.Instance(d)
	r := runner{}
	ctx := context.Background()
	redis := redisEngine{}
	b, _ := redis.locate()
	cli := b.(redisBins).cli

	if out, err := r.out(ctx, nil, cli, "-u", redis.url(inst, "127.0.0.1"), "PING"); err != nil || out != "PONG" {
		t.Fatalf("PING: %q %v", out, err)
	}
	if out, _ := r.out(ctx, nil, cli, "-p", fmt.Sprint(d.Port), "PING"); !strings.Contains(out, "NOAUTH") {
		t.Fatalf("sem senha deveria ser recusado: %q", out)
	}
	if _, err := redis.cli(ctx, r, Instance{Database: store.Database{Port: d.Port, Password: "errada"}}, b, "PING"); err == nil {
		t.Fatal("senha errada deveria virar erro em cli()")
	}
	for _, args := range [][]string{{"SET", "saudacao", "olá"}, {"HSET", "user:1", "nome", "Ana", "idade", "30"}, {"RPUSH", "fila", "a", "b"}} {
		if _, err := redis.cli(ctx, r, inst, b, args...); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := m.Tables(ctx, "cache")
	if err != nil || len(keys) != 3 {
		t.Fatalf("chaves: %v %v", keys, err)
	}
	if pv, err := m.Preview(ctx, "cache", "user:1"); err != nil || len(pv.Rows) != 2 || pv.Total != 2 {
		t.Fatalf("hash: %+v %v", pv, err)
	}
	if pv, err := m.Preview(ctx, "cache", "saudacao"); err != nil || pv.Rows[0][2] != "olá" {
		t.Fatalf("string: %+v %v", pv, err)
	}
	// backup, change data, restore (offline: stop → replace → start)
	if err := m.Backup("cache", "manual"); err != nil {
		t.Fatal(err)
	}
	bk := waitBackup(t, m, "cache")
	if _, err := redis.cli(ctx, r, inst, b, "FLUSHALL"); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore("cache", bk.File); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	waitIdle(t, m, "cache")
	if err := waitTCP(ctx, fmt.Sprintf("127.0.0.1:%d", d.Port), 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if keys, err := m.Tables(ctx, "cache"); err != nil || len(keys) != 3 {
		log, _ := os.ReadFile(m.LogPath("cache"))
		t.Fatalf("após restaurar: %v %v\n%s", keys, err, log)
	}
	// regenerate password
	old := redis.url(inst, "127.0.0.1")
	if err := m.RegeneratePassword("cache"); err != nil {
		t.Fatal(err)
	}
	nd, _ := m.Store.GetDatabase("cache")
	if out, _ := r.out(ctx, nil, cli, "-u", old, "PING"); out == "PONG" {
		t.Fatal("senha antiga deveria falhar")
	}
	if out, err := r.out(ctx, nil, cli, "-u", redis.url(m.Instance(nd), "127.0.0.1"), "PING"); err != nil || out != "PONG" {
		t.Fatalf("nova senha: %q %v", out, err)
	}
	// the config file was rewritten, so a restart keeps the new password
	if err := m.Control("cache", "restart"); err != nil {
		t.Fatal(err)
	}
	if err := waitTCP(ctx, fmt.Sprintf("127.0.0.1:%d", d.Port), 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if out, err := r.out(ctx, nil, cli, "-u", redis.url(m.Instance(nd), "127.0.0.1"), "PING"); err != nil || out != "PONG" {
		t.Fatalf("após reiniciar: %q %v", out, err)
	}
}

func TestIntegrationMariaDB(t *testing.T) {
	m := itManager(t, MySQL)
	if _, err := m.Create("sql", MySQL); err != nil {
		t.Fatal(err)
	}
	waitReady(t, m, "sql")
	d, _ := m.Store.GetDatabase("sql")
	inst := m.Instance(d)
	r := runner{}
	ctx := context.Background()
	my := mysqlEngine{}
	b, _ := my.locate()

	if out, err := my.query(ctx, r, inst, b, "SELECT DATABASE(), CURRENT_USER()"); err != nil || !strings.Contains(out, "app") {
		t.Fatalf("conexão: %q %v", out, err)
	}
	if _, err := my.query(ctx, r, inst, b, "CREATE TABLE clientes (id INT AUTO_INCREMENT PRIMARY KEY, nome VARCHAR(50)); INSERT INTO clientes (nome) VALUES ('Ana'), ('Bia')"); err != nil {
		t.Fatal(err)
	}
	tables, err := m.Tables(ctx, "sql")
	if err != nil || len(tables) != 1 || tables[0] != "clientes" {
		t.Fatalf("tabelas: %v %v", tables, err)
	}
	pv, err := m.Preview(ctx, "sql", "clientes")
	if err != nil || pv.Total != 2 || len(pv.Rows) != 2 || strings.Join(pv.Columns, ",") != "id,nome" {
		t.Fatalf("preview: %+v %v", pv, err)
	}
	if err := m.Backup("sql", "manual"); err != nil {
		t.Fatal(err)
	}
	bk := waitBackup(t, m, "sql")
	if _, err := my.query(ctx, r, inst, b, "DROP TABLE clientes"); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore("sql", bk.File); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	waitIdle(t, m, "sql")
	if pv, err := m.Preview(ctx, "sql", "clientes"); err != nil || pv.Total != 2 {
		log, _ := os.ReadFile(m.LogPath("sql"))
		t.Fatalf("restauração: %+v %v\n%s", pv, err, log)
	}
	if err := m.RegeneratePassword("sql"); err != nil {
		t.Fatal(err)
	}
	nd, _ := m.Store.GetDatabase("sql")
	if _, err := my.query(ctx, r, inst, b, "SELECT 1"); err == nil {
		t.Fatal("senha antiga deveria falhar")
	}
	if _, err := my.query(ctx, r, m.Instance(nd), b, "SELECT 1"); err != nil {
		t.Fatalf("nova senha: %v", err)
	}
}
