package monitor

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
)

func fakeCgroup(t *testing.T, root, app string, usec, mem, inactive int) {
	t.Helper()
	dir := filepath.Join(root, systemd.UnitName(app))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("cpu.stat", "usage_usec "+strconv.Itoa(usec)+"\nuser_usec 1\nsystem_usec 1\n")
	w("memory.current", strconv.Itoa(mem)+"\n")
	w("memory.stat", "anon 1\ninactive_file "+strconv.Itoa(inactive)+"\nactive_file 5\n")
}

func TestSamplerComputesCPUPercent(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	_ = s.Create(store.App{Name: "web", Repo: "r", Branch: "main", StartCmd: "x"})
	root := t.TempDir()
	sm := &sampler{store: s, cgRoot: root, prev: map[string]cpuPrev{}}

	t0 := time.Unix(1_800_000_000, 0)
	fakeCgroup(t, root, "web", 1_000_000, 50<<20, 0)
	sm.sample(t0)
	// 15 s later the app consumed 7.5 s of CPU => 50 %
	fakeCgroup(t, root, "web", 8_500_000, 60<<20, 0)
	sm.sample(t0.Add(15 * time.Second))

	pts := s.Metrics("web", t0.Add(-time.Minute), 1)
	if len(pts) != 2 {
		t.Fatalf("%+v", pts)
	}
	if pts[0].CPU != 0 || pts[1].CPU != 50 || pts[1].Mem != 60<<20 {
		t.Errorf("%+v", pts)
	}
	// stopped service leaves a gap and clears the baseline
	_ = os.RemoveAll(filepath.Join(root, systemd.UnitName("web")))
	sm.sample(t0.Add(30 * time.Second))
	if len(s.Metrics("web", t0.Add(-time.Minute), 1)) != 2 || len(sm.prev) != 0 {
		t.Error("serviço parado não deve gerar amostra")
	}
}

func TestBucket(t *testing.T) {
	if Bucket(time.Hour) != 15 || Bucket(24*time.Hour) != 360 {
		t.Errorf("Bucket %d %d", Bucket(time.Hour), Bucket(24*time.Hour))
	}
}
