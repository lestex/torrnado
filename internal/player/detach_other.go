//go:build !linux && !darwin

package player

import "os/exec"

// detachProcess is a no-op on platforms without POSIX sessions (see
// detach_unix.go). The player still runs, it just shares the parent's
// process group.
func detachProcess(cmd *exec.Cmd) {}
