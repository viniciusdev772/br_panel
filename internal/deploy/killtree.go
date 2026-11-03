package deploy

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// killTree sends SIGKILL to every descendant of root (found through /proc, whatever session or
// process group it is in) and then to root itself. The descendants are collected first, so a
// process that dies cannot reparent the rest out of the search.
func killTree(root int) {
	children := map[int][]int{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// "pid (comm) S ppid …": comm may hold spaces and parentheses, so cut after the last ")".
		s := string(stat)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		fields := strings.Fields(s[i+1:])
		if len(fields) < 2 {
			continue
		}
		if ppid, err := strconv.Atoi(fields[1]); err == nil {
			children[ppid] = append(children[ppid], pid)
		}
	}
	var victims []int
	var walk func(p int)
	walk = func(p int) {
		for _, c := range children[p] {
			victims = append(victims, c)
			walk(c)
		}
	}
	walk(root)
	for _, p := range victims {
		_ = syscall.Kill(p, syscall.SIGKILL)
	}
	_ = syscall.Kill(root, syscall.SIGKILL)
}
