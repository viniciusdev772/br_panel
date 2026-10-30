package dbengine

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

func TestStandardPort(t *testing.T) {
	for k, want := range map[Kind]int{Postgres: 5432, MySQL: 3306, MongoDB: 27017, Redis: 6379, Kind("x"): 0} {
		if got := StandardPort(k); got != want {
			t.Errorf("StandardPort(%s) = %d", k, got)
		}
	}
	for _, s := range Specs() { // the managed port never collides with the standard one by default
		if s.BasePort == StandardPort(s.Kind) {
			t.Errorf("%s: a porta base não pode ser a padrão", s.Kind)
		}
	}
}

func TestRenderForward(t *testing.T) {
	sock, svc := renderForward("mysql", 3306, 3307, "/usr/lib/systemd/systemd-socket-proxyd", true)
	for _, want := range []string{"ListenStream=127.0.0.1:3306", "ListenStream=[::1]:3306", "WantedBy=sockets.target"} {
		if !strings.Contains(sock, want) {
			t.Errorf("socket sem %q:\n%s", want, sock)
		}
	}
	for _, want := range []string{"ExecStart=/usr/lib/systemd/systemd-socket-proxyd 127.0.0.1:3307", "Requires=brpaneldb-mysql.service", "After=brpaneldb-mysql.service", "DynamicUser=yes"} {
		if !strings.Contains(svc, want) {
			t.Errorf("service sem %q:\n%s", want, svc)
		}
	}
	if sock4, _ := renderForward("mysql", 3306, 3307, "/p", false); strings.Contains(sock4, "[::1]") {
		t.Error("sem IPv6 não deve ouvir em ::1")
	}
	if strings.Contains(sock, "0.0.0.0") {
		t.Error("o repasse só pode ouvir em loopback")
	}
}

// fakeSystemctl logs its arguments to a file and fails when args contain failOn.
func fakeSystemctl(t *testing.T, failOn string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	body := "#!/bin/sh\necho \"$@\" >> '" + log + "'\ncase \"$*\" in *'" + failOn + "'*) echo falhou; exit 1;; esac\n"
	if failOn == "" {
		body = "#!/bin/sh\necho \"$@\" >> '" + log + "'\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func newForwardFixture(t *testing.T) (*Manager, store.Database) {
	t.Helper()
	m := newTestManager(t)
	d := store.Database{Name: "mysql", Kind: string(MySQL), Port: freePort(t), Username: "u", Password: "p", DBName: "d"}
	if err := m.Store.CreateDatabase(d); err != nil {
		t.Fatal(err)
	}
	return m, d
}

func TestEnableAndDisableForward(t *testing.T) {
	calls := fakeSystemctl(t, "")
	m, d := newForwardFixture(t)
	std := freePort(t)

	if m.ForwardState(d).Active {
		t.Fatal("começa sem repasse")
	}
	if err := m.enableForward(d, std, "/usr/lib/systemd/systemd-socket-proxyd"); err != nil {
		t.Fatal(err)
	}
	if !m.ForwardState(d).Active {
		t.Error("deveria estar ativo")
	}
	sock, _ := os.ReadFile(m.fwdFile("mysql", ".socket"))
	svc, _ := os.ReadFile(m.fwdFile("mysql", ".service"))
	if !strings.Contains(string(sock), "ListenStream=127.0.0.1:"+strconv.Itoa(std)) || !strings.Contains(string(svc), "127.0.0.1:"+strconv.Itoa(d.Port)) {
		t.Errorf("units:\n%s\n%s", sock, svc)
	}
	if err := m.enableForward(d, std, "/p"); err != nil { // idempotent
		t.Errorf("ativar de novo: %v", err)
	}

	if err := m.DisableForward("mysql"); err != nil {
		t.Fatal(err)
	}
	if m.ForwardState(d).Active {
		t.Error("deveria estar desativado")
	}
	if err := m.DisableForward("mysql"); err != nil { // nothing to do
		t.Errorf("desativar de novo: %v", err)
	}
	got, _ := os.ReadFile(calls)
	for _, want := range []string{"enable --now brpanelfwd-mysql.socket", "disable --now brpanelfwd-mysql.socket", "stop brpanelfwd-mysql.service", "daemon-reload"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("faltou systemctl %q em:\n%s", want, got)
		}
	}
}

func TestEnableForwardRefusals(t *testing.T) {
	fakeSystemctl(t, "")
	m, d := newForwardFixture(t)

	if err := m.enableForward(d, d.Port, "/p"); err == nil || !strings.Contains(err.Error(), "já usa a porta padrão") {
		t.Errorf("mesma porta: %v", err)
	}
	if err := m.enableForward(d, 0, "/p"); err == nil {
		t.Error("tipo sem porta padrão deveria falhar")
	}

	l, err := net.Listen("tcp4", "127.0.0.1:0") // something already listening there
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	busy := l.Addr().(*net.TCPAddr).Port
	if err := m.enableForward(d, busy, "/p"); err == nil || !strings.Contains(err.Error(), "ocupada") {
		t.Errorf("porta ocupada: %v", err)
	}
	other := store.Database{Name: "outro", Kind: string(MySQL), Port: freePort(t), Username: "u", Password: "p", DBName: "d"}
	if err := m.Store.CreateDatabase(other); err != nil {
		t.Fatal(err)
	}
	if err := m.enableForward(d, other.Port, "/p"); err == nil || !strings.Contains(err.Error(), "outro app ou banco") {
		t.Errorf("porta de outro banco: %v", err)
	}
	if m.ForwardState(d).Active {
		t.Error("nenhuma recusa pode deixar unit para trás")
	}
}

func TestEnableForwardRollsBackWhenSystemdFails(t *testing.T) {
	fakeSystemctl(t, "enable")
	m, d := newForwardFixture(t)
	err := m.enableForward(d, freePort(t), "/p")
	if err == nil || !strings.Contains(err.Error(), "falhou") {
		t.Errorf("err = %v", err)
	}
	for _, ext := range []string{".socket", ".service"} {
		if _, statErr := os.Stat(m.fwdFile("mysql", ext)); statErr == nil {
			t.Errorf("sobrou %s depois da falha", ext)
		}
	}
}

func TestForwardStateNotPossibleOnStandardPort(t *testing.T) {
	m := newTestManager(t)
	d := store.Database{Name: "pg", Kind: string(Postgres), Port: 5432}
	if m.ForwardState(d).Possible {
		t.Error("banco já na porta padrão não precisa de repasse")
	}
	if !m.ForwardState(store.Database{Name: "pg", Kind: string(Postgres), Port: 5433}).Possible {
		t.Error("banco em outra porta deveria permitir repasse")
	}
}
