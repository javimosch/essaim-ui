//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach puts the child in its own session so closing the launcher, or the
// terminal it was started from, does not take the server down with it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
