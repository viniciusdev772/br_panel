package sysinfo

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"testing"
)

const tcpHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

func TestParseListeners(t *testing.T) {
	data := tcpHeader +
		"   0: 00000000:0C12 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0\n" +
		"   1: 0100007F:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000   101        0 777 1 0000000000000000 100 0 0 10 0\n" +
		"   2: 0100007F:9C40 0100007F:0C12 01 00000000:00000000 00:00000000 00000000     0        0 888 1 0000000000000000 20 4 30 10 -1\n" + // ESTABLISHED
		"   3: 0000000000000000FFFF00000100007F:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 999 1 0 100 0 0 10 0\n" +
		"   4: lixo\n" +
		"   5: 00000000:0000 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 55 1\n" + // port 0
		"   6: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 0 1\n" // inode 0
	got := ParseListeners(data)
	want := []Listener{{3090, 12345}, {53, 777}, {8080, 999}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseListeners = %+v, quero %+v", got, want)
	}
	if len(ParseListeners("")) != 0 || len(ParseListeners(tcpHeader)) != 0 {
		t.Error("sem linhas não há escuta")
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("este sistema não cria links simbólicos (%v); o teste roda no Linux", err)
	}
}

// fakeSystem builds a /proc and a cgroup tree with two services: web (pid 100, listens on 3090 in
// IPv6 and on 9229) and api (pid 200 in a nested cgroup, listens on 3001).
func fakeSystem(t *testing.T) {
	t.Helper()
	proc, cg := t.TempDir(), t.TempDir()
	oldProc, oldCg := ProcRoot, CgroupMount
	ProcRoot, CgroupMount = proc, cg
	t.Cleanup(func() { ProcRoot, CgroupMount = oldProc, oldCg })

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(proc, "net", "tcp"), tcpHeader+
		"   0: 0100007F:2455 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 5002 1\n"+ // 9301? no: 0x2455 = 9301 -> used for web's second socket below
		"   1: 0100007F:0BB9 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 6001 1\n") // 3001
	write(filepath.Join(proc, "net", "tcp6"), tcpHeader+
		"   0: 00000000000000000000000000000000:0C12 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 5001 1\n") // 3090
	write(filepath.Join(proc, "100", "cgroup"), "0::/system.slice/brpanel-web.service\n")
	write(filepath.Join(proc, "200", "cgroup"), "0::/system.slice/brpanel-api.service/payload\n")
	write(filepath.Join(proc, "300", "comm"), "nginx\n")
	write(filepath.Join(cg, "system.slice", "brpanel-web.service", "cgroup.procs"), "100\n")
	write(filepath.Join(cg, "system.slice", "brpanel-api.service", "payload", "cgroup.procs"), "200\n") // nested

	for pid, inodes := range map[string][]string{"100": {"5001", "5002"}, "200": {"6001"}} {
		if err := os.MkdirAll(filepath.Join(proc, pid, "fd"), 0o755); err != nil {
			t.Fatal(err)
		}
		for i, ino := range inodes {
			symlinkOrSkip(t, "socket:["+ino+"]", filepath.Join(proc, pid, "fd", strconv.Itoa(3+i)))
		}
	}
	if err := os.MkdirAll(filepath.Join(proc, "100", "fd"), 0o755); err == nil {
		symlinkOrSkip(t, "/dev/null", filepath.Join(proc, "100", "fd", "0")) // a non-socket fd is ignored
	}
}

func TestListeningPortsAndHolders(t *testing.T) {
	fakeSystem(t)

	if got := ListeningPorts("/system.slice/brpanel-web.service"); !reflect.DeepEqual(got, []int{3090, 9301}) {
		t.Errorf("web escuta em %v, quero [3090 9301] (IPv6 incluído)", got)
	}
	if got := ListeningPorts("/system.slice/brpanel-api.service"); !reflect.DeepEqual(got, []int{3001}) {
		t.Errorf("api (cgroup aninhado) escuta em %v, quero [3001]", got)
	}
	for _, bad := range []string{"", "/../../etc", "/system.slice/nao-existe.service"} {
		if got := ListeningPorts(bad); len(got) != 0 {
			t.Errorf("%q não deveria listar portas: %v", bad, got)
		}
	}

	if got := PortHolders(3090); !reflect.DeepEqual(got, []string{"brpanel-web.service"}) {
		t.Errorf("quem segura a 3090: %v", got)
	}
	if got := PortHolders(3001); !reflect.DeepEqual(got, []string{"brpanel-api.service"}) {
		t.Errorf("quem segura a 3001 (cgroup aninhado): %v", got)
	}
	if got := PortHolders(4444); len(got) != 0 {
		t.Errorf("porta livre não tem dono: %v", got)
	}
}

// The real kernel: a listener this test opens must be found, with its owner.
func TestRealKernelListener(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("precisa do /proc do Linux")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	var found bool
	for _, l := range HostListeners() {
		found = found || l.Port == port
	}
	if !found {
		t.Fatalf("a porta %d aberta por este teste não apareceu em /proc/net/tcp", port)
	}
	if h := PortHolders(port); len(h) == 0 {
		t.Errorf("PortHolders(%d) vazio: o dono deveria ser este processo", port)
	}
}
