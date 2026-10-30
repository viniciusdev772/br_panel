package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func fakeCgroup(t *testing.T, root, app string, usec, mem, inactive int) {
	t.Helper()
	dir := filepath.Join(root, unitName(app))
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
	usec, mem, ok := readCgroup(filepath.Join(root, unitName("web")))
	if !ok || usec != 5_000_000 || mem != 70<<20 {
		t.Fatalf("%d %d %v", usec, mem, ok)
	}
	if _, _, ok := readCgroup(filepath.Join(root, "inexistente")); ok {
		t.Error("cgroup ausente deve retornar ok=false")
	}
}

func TestSamplerComputesCPUPercent(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Create(App{Name: "web", Repo: "r", Branch: "main", StartCmd: "x"})
	root := t.TempDir()
	sm := &sampler{s: &Server{store: s}, cgRoot: root, prev: map[string]cpuPrev{}}

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
	_ = os.RemoveAll(filepath.Join(root, unitName("web")))
	sm.sample(t0.Add(30 * time.Second))
	if len(s.Metrics("web", t0.Add(-time.Minute), 1)) != 2 || len(sm.prev) != 0 {
		t.Error("serviço parado não deve gerar amostra")
	}
}

func TestMetricsBucketsAndLatest(t *testing.T) {
	s, _ := newTestStore(t)
	base := time.Now().Add(-10 * time.Minute).Truncate(time.Minute)
	var pts []Point
	for i := 0; i < 8; i++ { // 8 samples, 15 s apart
		pts = append(pts, Point{TS: base.Add(time.Duration(i) * 15 * time.Second).Unix(), CPU: float64(i * 10), Mem: int64(i+1) << 20})
	}
	if err := s.InsertMetrics("web", pts); err != nil {
		t.Fatal(err)
	}
	got := s.Metrics("web", base, 60) // one-minute buckets
	if len(got) != 2 || got[0].CPU != 15 || got[0].Mem != 4<<20 {
		t.Errorf("%+v", got)
	}
	if l := s.LatestMetrics(base)["web"]; l.Mem != 8<<20 {
		t.Errorf("latest: %+v", l)
	}
	if len(s.LatestMetrics(time.Now())) != 0 {
		t.Error("amostras antigas não devem aparecer")
	}
	s.PruneMetrics(time.Now())
	if len(s.Metrics("web", base, 60)) != 0 {
		t.Error("prune")
	}
	if chartBucket(time.Hour) != 15 || chartBucket(24*time.Hour) != 360 {
		t.Errorf("chartBucket %d %d", chartBucket(time.Hour), chartBucket(24*time.Hour))
	}
}

func TestParseServiceShow(t *testing.T) {
	out := "ActiveState=active\nSubState=running\nNRestarts=2\nMainPID=4121\nActiveEnterTimestampMonotonic=100000000\n"
	i := parseServiceShow(out, 400)
	if i.State != "active" || i.Restarts != 2 || i.PID != 4121 || i.Uptime != 300 {
		t.Errorf("%+v", i)
	}
	if i := parseServiceShow("", 10); i.State != "unknown" || i.Uptime != 0 {
		t.Errorf("%+v", i)
	}
}

func TestHostStats(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "meminfo"), []byte("MemTotal:        2048000 kB\nMemFree: 1 kB\nMemAvailable:    1024000 kB\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "loadavg"), []byte("0.50 0.40 0.30 1/100 999\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "uptime"), []byte("12345.67 999.0\n"), 0o644)
	old := procRoot
	procRoot = dir
	defer func() { procRoot = old }()
	h := readHostStats(t.TempDir(), 2)
	if h.MemTotal != 2048000*1024 || h.MemUsed != 1024000*1024 || h.Load[0] != 0.5 || h.Uptime != 12345 || h.DiskTotal == 0 {
		t.Errorf("%+v", h)
	}
}
