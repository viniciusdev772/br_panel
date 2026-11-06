//go:build !linux

package deploy

import "os/exec"

// ownGroup only keeps the panel buildable on a developer's machine: deploys run on Linux, where
// proc_linux.go kills the whole process tree. Here the default cancel kills just the command.
func ownGroup(*exec.Cmd) {}
