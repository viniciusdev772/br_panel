//go:build !linux

package buildcache

import (
	"io/fs"
	"time"
)

// accessTime is unavailable off Linux (the modification time is used alone); this only keeps the
// panel buildable on a developer's machine.
func accessTime(fs.FileInfo) (time.Time, bool) { return time.Time{}, false }
