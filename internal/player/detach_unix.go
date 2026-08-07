//go:build linux || darwin

package player

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the player in its own session so it survives the
// TUI exiting, and so a Ctrl-C aimed at the TUI doesn't also kill it.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
