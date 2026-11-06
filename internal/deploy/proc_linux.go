//go:build linux

package deploy

import (
	"os/exec"
	"syscall"
)

// ownGroup puts the command in its own process group and makes cancelling it kill the whole tree.
func ownGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		killTree(c.Process.Pid) // runuser starts the build in a session of its own: the group kill alone misses it
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
