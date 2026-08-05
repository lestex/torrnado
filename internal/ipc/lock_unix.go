//go:build linux || darwin

package ipc

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// acquireDaemonLock takes an exclusive, non-blocking flock on
// socketPath + ".lock", proving this process is the only daemon for that
// socket. The returned file must be kept open for the lifetime of the
// daemon -- the lock is released when it is closed (or when the process
// dies, including on a panic or SIGKILL, which is the whole point).
//
// This exists because probing the socket is not a sound exclusion test.
// Dialing answers "is someone accepting right now", not "does a daemon
// own this socket": a daemon busy hash-checking a large torrent can fail
// to accept inside any timeout you pick, and the probe-then-unlink
// sequence is a TOCTOU race besides. Losing that gamble is expensive --
// the loser unlinks the live daemon's socket and binds its own, leaving
// two engines running against the same data directory and the same
// SQLite piece-completion database, corrupting each other's view of
// which pieces are complete.
func acquireDaemonLock(socketPath string) (*os.File, error) {
	path := socketPath + ".lock"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open daemon lock %s: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, fmt.Errorf("another daemon already holds %s", path)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return f, nil
}

// releaseDaemonLock drops the lock. The lock file itself is deliberately
// left on disk: unlinking it would let a daemon starting concurrently
// lock a file this one is about to delete, and both would then think they
// held the lock.
func releaseDaemonLock(f *os.File) {
	if f == nil {
		return
	}
	unix.Flock(int(f.Fd()), unix.LOCK_UN)
	f.Close()
}
