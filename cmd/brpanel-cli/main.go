// Command brpanel-cli is the BR Panel command line on its own, for the computer you work on (Linux,
// macOS, Windows): it only talks to a remote panel after `brpanel login`. On the server itself the
// `brpanel` binary already contains the same CLI. Installed under the name brpanel.
package main

import (
	"os"

	"github.com/viniciusdev772/br_panel/internal/cli"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"help"}
	}
	os.Exit(cli.Run(args, os.Stdin, os.Stdout, os.Stderr))
}
