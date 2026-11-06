package sysinfo

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func fakeCgroup(t *testing.T, root, app string, usec, mem, inactive int) {
	t.Helper()
	dir := filepath.Join(root, "brpanel-"+app+".service")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("cpu.stat", "usage_usec "+itoa(usec)+"\nuser_usec 1\nsystem_usec 1\n")
	w("memory.current", itoa(mem)+"\n")
	w("memory.stat", "anon 1\ninactive_file "+itoa(inactive)+"\nactive_file 5\n")
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestReadCgroup(t *testing.T) {
	root := t.TempDir()
	fakeCgroup(t, root, "web", 5_000_000, 100<<20, 30<<20)
	usec, mem, ok := ReadCgroup(filepath.Join(root, "brpanel-web.service"))
	if !ok || usec != 5_000_000 || mem != 70<<20 {
		t.Fatalf("%d %d %v", usec, mem, ok)
	}
	if _, _, ok := ReadCgroup(filepath.Join(root, "inexistente")); ok {
		t.Error("cgroup ausente deve retornar ok=false")
	}
}

func TestHostStats(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "meminfo"), []byte("MemTotal:        2048000 kB\nMemFree: 1 kB\nMemAvailable:    1024000 kB\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "loadavg"), []byte("0.50 0.40 0.30 1/100 999\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "uptime"), []byte("12345.67 999.0\n"), 0o644)
	old := ProcRoot
	ProcRoot = dir
	defer func() { ProcRoot = old }()
	h := ReadHost(t.TempDir(), 2)
	if h.MemTotal != 2048000*1024 || h.MemUsed != 1024000*1024 || h.Load[0] != 0.5 || h.Uptime != 12345 || (runtime.GOOS == "linux" && h.DiskTotal == 0) { // the disk is only read on Linux
		t.Errorf("%+v", h)
	}
}
