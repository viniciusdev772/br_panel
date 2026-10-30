// Package monitor samples CPU and memory of every app's service cgroup into the database.
package monitor

import (
	"context"
	"path/filepath"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/sysinfo"
	"github.com/viniciusdev772/br_panel/internal/systemd"
)

const (
	// SampleEvery is the sampling interval.
	SampleEvery = 15 * time.Second
	// Retention is how long samples are kept.
	Retention = 24 * time.Hour
	// MaxChartPoints bounds the points returned for a chart.
	MaxChartPoints = 240
	// CgroupRoot is where systemd places system services in cgroup v2.
	CgroupRoot = "/sys/fs/cgroup/system.slice"
)

type cpuPrev struct {
	usec uint64
	at   time.Time
}

// sampler periodically records CPU and RAM of every app's service cgroup.
// Sampler periodically records CPU and RAM of every app.
type sampler struct {
	store  *store.Store
	cgRoot string // /sys/fs/cgroup/system.slice
	prev   map[string]cpuPrev
}

func Run(ctx context.Context, st *store.Store) {
	sm := &sampler{store: st, cgRoot: CgroupRoot, prev: map[string]cpuPrev{}}
	tick := time.NewTicker(SampleEvery)
	defer tick.Stop()
	lastPrune := time.Time{}
	for {
		sm.sample(time.Now())
		if time.Since(lastPrune) > time.Hour {
			st.PruneMetrics(time.Now().Add(-Retention))
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
	record := func(key, unit string) {
		usec, mem, ok := sysinfo.ReadCgroup(filepath.Join(sm.cgRoot, unit))
		if !ok { // stopped: leave a gap in the chart
			return
		}
		seen[key] = true
		cpu := 0.0
		if p, had := sm.prev[key]; had && usec >= p.usec {
			if dt := now.Sub(p.at).Microseconds(); dt > 0 {
				cpu = float64(usec-p.usec) / float64(dt) * 100
			}
		}
		sm.prev[key] = cpuPrev{usec, now}
		_ = sm.store.InsertMetrics(key, []store.Point{{TS: now.Unix(), CPU: round1(cpu), Mem: int64(mem)}})
	}
	for _, a := range sm.store.List() {
		record(a.Name, systemd.UnitName(a.Name))
	}
	for _, d := range sm.store.ListDatabases() { // databases are stored under "db:<name>"
		record(DBKey(d.Name), systemd.DBUnitName(d.Name))
	}
	for name := range sm.prev {
		if !seen[name] {
			delete(sm.prev, name)
		}
	}
}

// DBKey is the metrics key of a database (app names cannot contain a colon, so keys never collide).
func DBKey(name string) string { return "db:" + name }

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

// Bucket picks a bucket size so a range has at most MaxChartPoints points.
func Bucket(d time.Duration) int64 {
	b := int64(d.Seconds()) / MaxChartPoints
	if b < int64(SampleEvery.Seconds()) {
		b = int64(SampleEvery.Seconds())
	}
	return b
}
