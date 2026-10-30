package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	sampleEvery      = 15 * time.Second
	metricsRetention = 24 * time.Hour
	maxChartPoints   = 240
)

// Point is one monitoring sample: CPU in percent of one core, memory in bytes.
type Point struct {
	TS  int64   `json:"ts"`
	CPU float64 `json:"cpu"`
	Mem int64   `json:"mem"`
}

// ---- storage ----

func (s *Store) InsertMetrics(app string, pts []Point) error {
	if len(pts) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range pts {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO metrics (app, ts, cpu, mem) VALUES (?, ?, ?, ?)`, app, p.TS, p.CPU, p.Mem); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Metrics returns samples since `since`, averaged (CPU) / maxed (memory) into buckets of `bucket` seconds.
func (s *Store) Metrics(app string, since time.Time, bucket int64) []Point {
	if bucket < 1 {
		bucket = 1
	}
	rows, err := s.db.Query(`SELECT (ts / ?) * ?, AVG(cpu), MAX(mem) FROM metrics
		WHERE app = ? AND ts >= ? GROUP BY ts / ? ORDER BY 1`, bucket, bucket, app, since.Unix(), bucket)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var pts []Point
	for rows.Next() {
		var p Point
		if rows.Scan(&p.TS, &p.CPU, &p.Mem) == nil {
			pts = append(pts, p)
		}
	}
	return pts
}

// LatestMetrics returns each app's newest sample taken at or after `since`.
func (s *Store) LatestMetrics(since time.Time) map[string]Point {
	out := map[string]Point{}
	rows, err := s.db.Query(`SELECT m.app, m.ts, m.cpu, m.mem FROM metrics m
		JOIN (SELECT app, MAX(ts) AS ts FROM metrics WHERE ts >= ? GROUP BY app) l ON l.app = m.app AND l.ts = m.ts`, since.Unix())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var app string
		var p Point
		if rows.Scan(&app, &p.TS, &p.CPU, &p.Mem) == nil {
			out[app] = p
		}
	}
	return out
}

func (s *Store) PruneMetrics(before time.Time) {
	_, _ = s.db.Exec(`DELETE FROM metrics WHERE ts < ?`, before.Unix())
}

// ---- cgroup v2 readers ----

// readCgroup reads CPU time (µs) and working-set memory (bytes) of a cgroup v2 directory.
// Memory excludes inactive file cache, like `docker stats`, so it reflects what the app really holds.
func readCgroup(dir string) (cpuUsec uint64, mem uint64, ok bool) {
	cpu, err := os.ReadFile(filepath.Join(dir, "cpu.stat"))
	if err != nil {
		return 0, 0, false
	}
	found := false
	for _, line := range strings.Split(string(cpu), "\n") {
		if v, isUsage := strings.CutPrefix(line, "usage_usec "); isUsage {
			cpuUsec, _ = strconv.ParseUint(strings.TrimSpace(v), 10, 64)
			found = true
		}
	}
	cur, err := os.ReadFile(filepath.Join(dir, "memory.current"))
	if err != nil || !found {
		return 0, 0, false
	}
	mem, _ = strconv.ParseUint(strings.TrimSpace(string(cur)), 10, 64)
	if st, err := os.ReadFile(filepath.Join(dir, "memory.stat")); err == nil {
		for _, line := range strings.Split(string(st), "\n") {
			if v, isFile := strings.CutPrefix(line, "inactive_file "); isFile {
				if in, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64); in < mem {
					mem -= in
				}
			}
		}
	}
	return cpuUsec, mem, true
}

type cpuPrev struct {
	usec uint64
	at   time.Time
}

// sampler periodically records CPU and RAM of every app's service cgroup.
type sampler struct {
	s      *Server
	cgRoot string // /sys/fs/cgroup/system.slice
	prev   map[string]cpuPrev
}

func (s *Server) runSampler(ctx context.Context) {
	sm := &sampler{s: s, cgRoot: "/sys/fs/cgroup/system.slice", prev: map[string]cpuPrev{}}
	tick := time.NewTicker(sampleEvery)
	defer tick.Stop()
	lastPrune := time.Time{}
	for {
		sm.sample(time.Now())
		if time.Since(lastPrune) > time.Hour {
			s.store.PruneMetrics(time.Now().Add(-metricsRetention))
			lastPrune = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (sm *sampler) sample(now time.Time) {
	seen := map[string]bool{}
	for _, a := range sm.s.store.List() {
		usec, mem, ok := readCgroup(filepath.Join(sm.cgRoot, unitName(a.Name)))
		if !ok { // stopped: leave a gap in the chart
			continue
		}
		seen[a.Name] = true
		cpu := 0.0
		if p, had := sm.prev[a.Name]; had && usec >= p.usec {
			if dt := now.Sub(p.at).Microseconds(); dt > 0 {
				cpu = float64(usec-p.usec) / float64(dt) * 100
			}
		}
		sm.prev[a.Name] = cpuPrev{usec, now}
		_ = sm.s.store.InsertMetrics(a.Name, []Point{{TS: now.Unix(), CPU: round1(cpu), Mem: int64(mem)}})
	}
	for name := range sm.prev {
		if !seen[name] {
			delete(sm.prev, name)
		}
	}
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

// chartBucket picks a bucket size so a range has at most maxChartPoints points.
func chartBucket(d time.Duration) int64 {
	b := int64(d.Seconds()) / maxChartPoints
	if b < int64(sampleEvery.Seconds()) {
		b = int64(sampleEvery.Seconds())
	}
	return b
}

// ---- service status ----

type ServiceInfo struct {
	State    string `json:"state"`
	SubState string `json:"sub_state"`
	Restarts int    `json:"restarts"`
	Uptime   int64  `json:"uptime"` // seconds, 0 when not running
	PID      int    `json:"pid"`
}

var procRoot = "/proc"

func procUptime() float64 {
	b, err := os.ReadFile(filepath.Join(procRoot, "uptime"))
	if err != nil {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.Fields(string(b) + " ")[0], 64)
	return f
}

func parseServiceShow(out string, upSeconds float64) ServiceInfo {
	kv := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			kv[k] = strings.TrimSpace(v)
		}
	}
	info := ServiceInfo{State: kv["ActiveState"], SubState: kv["SubState"]}
	if info.State == "" {
		info.State = "unknown"
	}
	info.Restarts, _ = strconv.Atoi(kv["NRestarts"])
	info.PID, _ = strconv.Atoi(kv["MainPID"])
	if mono, _ := strconv.ParseFloat(kv["ActiveEnterTimestampMonotonic"], 64); mono > 0 && info.State == "active" {
		if u := int64(upSeconds - mono/1e6); u > 0 {
			info.Uptime = u
		}
	}
	return info
}

func serviceInfo(app string) ServiceInfo {
	out, _ := systemctl("show", unitName(app), "-p", "ActiveState", "-p", "SubState", "-p", "NRestarts",
		"-p", "MainPID", "-p", "ActiveEnterTimestampMonotonic")
	return parseServiceShow(out, procUptime())
}

// ---- host ----

type HostStats struct {
	MemTotal  int64      `json:"mem_total"`
	MemUsed   int64      `json:"mem_used"`
	Load      [3]float64 `json:"load"`
	Uptime    int64      `json:"uptime"`
	DiskTotal int64      `json:"disk_total"`
	DiskUsed  int64      `json:"disk_used"`
	CPUs      int        `json:"cpus"`
}

func readHostStats(dataDir string, cpus int) HostStats {
	h := HostStats{CPUs: cpus}
	if f, err := os.Open(filepath.Join(procRoot, "meminfo")); err == nil {
		var total, avail int64
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var key string
			var kb int64
			if _, err := fmt.Sscanf(sc.Text(), "%s %d", &key, &kb); err == nil {
				switch key {
				case "MemTotal:":
					total = kb * 1024
				case "MemAvailable:":
					avail = kb * 1024
				}
			}
		}
		f.Close()
		h.MemTotal, h.MemUsed = total, total-avail
	}
	if b, err := os.ReadFile(filepath.Join(procRoot, "loadavg")); err == nil {
		fmt.Sscanf(string(b), "%f %f %f", &h.Load[0], &h.Load[1], &h.Load[2])
	}
	h.Uptime = int64(procUptime())
	var st syscall.Statfs_t
	if syscall.Statfs(dataDir, &st) == nil {
		h.DiskTotal = int64(st.Blocks) * int64(st.Bsize)
		h.DiskUsed = h.DiskTotal - int64(st.Bavail)*int64(st.Bsize)
	}
	return h
}
