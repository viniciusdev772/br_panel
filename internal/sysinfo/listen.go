package sysinfo

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CgroupMount is where the cgroup v2 hierarchy is mounted; tests point it at a fake directory.
var CgroupMount = "/sys/fs/cgroup"

// Listener is a TCP socket in the LISTEN state.
type Listener struct {
	Port  int
	Inode uint64
}

// ParseListeners reads the text of /proc/net/tcp (or tcp6) and keeps the sockets that listen.
// A row is "sl local_address rem_address st tx:rx tr:tm retrnsmt uid timeout inode ...", with the
// address as HEXIP:HEXPORT and the state 0A for LISTEN. Rows it cannot read are skipped.
func ParseListeners(data string) []Listener {
	var out []Listener
	for i, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 10 || f[3] != "0A" {
			continue
		}
		_, hexPort, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		port, err := strconv.ParseUint(hexPort, 16, 16)
		inode, err2 := strconv.ParseUint(f[9], 10, 64)
		if err != nil || err2 != nil || port == 0 || inode == 0 {
			continue
		}
		out = append(out, Listener{Port: int(port), Inode: inode})
	}
	return out
}

// HostListeners lists the listening TCP sockets of the host, IPv4 and IPv6: a server bound to
// "::" (Node's default) shows up only in tcp6.
func HostListeners() []Listener {
	var out []Listener
	for _, name := range []string{"tcp", "tcp6"} {
		if b, err := os.ReadFile(filepath.Join(ProcRoot, "net", name)); err == nil {
			out = append(out, ParseListeners(string(b))...)
		}
	}
	return out
}

// socketInodes is the set of sockets a process holds open (its /proc/<pid>/fd links read
// "socket:[inode]"). A process that is gone or not readable has none.
func socketInodes(pid string) map[uint64]bool {
	dir := filepath.Join(ProcRoot, pid, "fd")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	set := map[uint64]bool{}
	for _, e := range ents {
		link, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if s, ok := strings.CutPrefix(link, "socket:["); ok {
			if n, err := strconv.ParseUint(strings.TrimSuffix(s, "]"), 10, 64); err == nil {
				set[n] = true
			}
		}
	}
	return set
}

// cgroupPIDs lists the processes of a cgroup directory and of the ones nested in it.
func cgroupPIDs(dir string, depth int) []string {
	var pids []string
	if b, err := os.ReadFile(filepath.Join(dir, "cgroup.procs")); err == nil {
		pids = strings.Fields(string(b))
	}
	if depth > 0 {
		if ents, err := os.ReadDir(dir); err == nil {
			for _, e := range ents {
				if e.IsDir() {
					pids = append(pids, cgroupPIDs(filepath.Join(dir, e.Name()), depth-1)...)
				}
			}
		}
	}
	return pids
}

// ListeningPorts returns the TCP ports that processes of a systemd unit are listening on, given
// the unit's ControlGroup path (as "systemctl show -p ControlGroup" prints it).
func ListeningPorts(controlGroup string) []int {
	if controlGroup == "" || strings.Contains(controlGroup, "..") {
		return nil
	}
	byInode := map[uint64]int{}
	for _, l := range HostListeners() {
		byInode[l.Inode] = l.Port
	}
	if len(byInode) == 0 {
		return nil
	}
	seen := map[int]bool{}
	for _, pid := range cgroupPIDs(filepath.Join(CgroupMount, controlGroup), 4) {
		for ino := range socketInodes(pid) {
			if p, ok := byInode[ino]; ok {
				seen[p] = true
			}
		}
	}
	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

// maxScanned bounds how many processes PortHolders looks at.
const maxScanned = 20000

// PortHolders names the systemd units (or, failing that, the commands) whose processes listen on
// port. It walks every process, so call it only to explain a failure.
func PortHolders(port int) []string {
	want := map[uint64]bool{}
	for _, l := range HostListeners() {
		if l.Port == port {
			want[l.Inode] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	ents, err := os.ReadDir(ProcRoot)
	if err != nil {
		return nil
	}
	found := map[string]bool{}
	for i, e := range ents {
		if i >= maxScanned {
			break
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		for ino := range socketInodes(e.Name()) {
			if want[ino] {
				found[holderName(e.Name())] = true
				break
			}
		}
	}
	out := make([]string, 0, len(found))
	for h := range found {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// holderName is the unit a process runs in (cgroup v2 line "0::/system.slice/x.service"), or its
// command name when it belongs to no service.
func holderName(pid string) string {
	if b, err := os.ReadFile(filepath.Join(ProcRoot, pid, "cgroup")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if _, path, ok := strings.Cut(line, "::"); ok {
				for _, part := range strings.Split(path, "/") {
					if strings.HasSuffix(part, ".service") {
						return part
					}
				}
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(ProcRoot, pid, "comm")); err == nil {
		return strings.TrimSpace(string(b)) + " (pid " + pid + ")"
	}
	return "pid " + pid
}
