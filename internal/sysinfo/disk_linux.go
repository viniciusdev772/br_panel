//go:build linux

package sysinfo

import "syscall"

// diskUsage reports the size and the used bytes of the filesystem that holds dir (0, 0 on failure).
func diskUsage(dir string) (total, used int64) {
	var st syscall.Statfs_t
	if syscall.Statfs(dir, &st) != nil {
		return 0, 0
	}
	total = int64(st.Blocks) * int64(st.Bsize)
	return total, total - int64(st.Bavail)*int64(st.Bsize)
}
