// Package execx runs host commands (systemctl, nginx, certbot) and logs their output.
package execx

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Run executes name with args as the current user, echoing the command line and
// its combined output to log. Arguments are never interpreted by a shell.
func Run(ctx context.Context, log io.Writer, name string, args ...string) error {
	fmt.Fprintf(log, "$ %s %s\n", name, strings.Join(args, " "))
	c := exec.CommandContext(ctx, name, args...)
	c.Env, c.Stdout, c.Stderr = os.Environ(), log, log
	return c.Run()
}
