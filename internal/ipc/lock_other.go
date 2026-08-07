//go:build !linux && !darwin

package ipc

import "os"

// acquireDaemonLock is a no-op on platforms without flock. Unix-socket
// IPC doesn't run there anyway (see the README's non-goals), so this
// exists to keep the package compiling rather than to provide exclusion.
func acquireDaemonLock(string) (*os.File, error) { return nil, nil }

func releaseDaemonLock(*os.File) {}
