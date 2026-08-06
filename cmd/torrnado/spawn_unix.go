//go:build linux || darwin

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the daemon in its own session so it survives the
// parent (TUI/CLI) process exiting.
//
// Without this the daemon stays in the terminal's process group and a
// Ctrl-C aimed at the shell, or simply closing the terminal, takes the
// downloads with it.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
