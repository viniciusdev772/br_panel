// Package sysinfo reads Linux kernel interfaces: cgroup v2 counters and /proc.
package sysinfo

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ReadCgroup reads CPU time (µs) and working-set memory (bytes) of a cgroup v2 directory.
// Memory excludes inactive file cache, like `docker stats`, so it reflects what the app really holds.
func ReadCgroup(dir string) (cpuUsec uint64, mem uint64, ok bool) {
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

// ProcRoot is the procfs mount point; tests point it at a fake directory.
var ProcRoot = "/proc"

// Uptime returns the host uptime in seconds (0 if unknown).
func Uptime() float64 {
	b, err := os.ReadFile(filepath.Join(ProcRoot, "uptime"))
	if err != nil {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.Fields(string(b) + " ")[0], 64)
	return f
}

type HostStats struct {
	MemTotal  int64      `json:"mem_total"`
	MemUsed   int64      `json:"mem_used"`
	Load      [3]float64 `json:"load"`
	Uptime    int64      `json:"uptime"`
	DiskTotal int64      `json:"disk_total"`
	DiskUsed  int64      `json:"disk_used"`
	CPUs      int        `json:"cpus"`
}

func ReadHost(dataDir string, cpus int) HostStats {
	h := HostStats{CPUs: cpus}
	if f, err := os.Open(filepath.Join(ProcRoot, "meminfo")); err == nil {
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
	if b, err := os.ReadFile(filepath.Join(ProcRoot, "loadavg")); err == nil {
		fmt.Sscanf(string(b), "%f %f %f", &h.Load[0], &h.Load[1], &h.Load[2])
	}
	h.Uptime = int64(Uptime())
	var st syscall.Statfs_t
	if syscall.Statfs(dataDir, &st) == nil {
		h.DiskTotal = int64(st.Blocks) * int64(st.Bsize)
		h.DiskUsed = h.DiskTotal - int64(st.Bavail)*int64(st.Bsize)
	}
	return h
}
