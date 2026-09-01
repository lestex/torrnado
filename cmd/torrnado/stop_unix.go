//go:build linux || darwin

package main

import "syscall"

// signalStop asks the daemon to shut down the way a service manager
// would: SIGTERM, which the daemon handles by saving its session and
// closing the engine cleanly. Never SIGKILL - that is the caller's to
// choose, after being told the polite request did not work.
func signalStop(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
