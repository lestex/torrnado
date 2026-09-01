//go:build !linux && !darwin

package ipc

import (
	"errors"
	"os"
)

// acquireDaemonLock is a no-op on platforms without flock. Unix-socket
// IPC doesn't run there anyway (see the README's non-goals), so this
// exists to keep the package compiling rather than to provide exclusion.
func acquireDaemonLock(string) (*os.File, error) { return nil, nil }

func releaseDaemonLock(*os.File) {}

// DaemonInfo cannot answer without the lock the platform does not
// provide, so it says so rather than reporting a confident "not
// running" that would have `torrnado stop` give up on a live daemon.
func DaemonInfo(socketPath string) (DaemonStatus, error) {
	return DaemonStatus{LockPath: socketPath + ".lock"},
		errors.New("daemon status needs file locking, which this platform does not provide")
}
