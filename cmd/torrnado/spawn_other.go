//go:build !linux && !darwin

package main

import "os/exec"

// detachProcess is a no-op on platforms without POSIX sessions (see
// spawn_unix.go). The daemon still runs in the background, it just isn't
// fully detached from the parent's process group.
//
// The `//go:build` lines at the top of these two files are how Go picks
// one implementation per platform: only the file whose constraint matches
// is compiled.
func detachProcess(cmd *exec.Cmd) {}
