//go:build linux

package buildcache

import (
	"io/fs"
	"syscall"
	"time"
)

// accessTime is the last access time of a file, when the filesystem reports it.
func accessTime(info fs.FileInfo) (time.Time, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(int64(st.Atim.Sec), int64(st.Atim.Nsec)), true
}
