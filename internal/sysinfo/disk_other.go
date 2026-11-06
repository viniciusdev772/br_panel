//go:build !linux

package sysinfo

// diskUsage is unknown off Linux: the panel only serves from a Linux server, this keeps it
// buildable on a developer's machine.
func diskUsage(string) (total, used int64) { return 0, 0 }
