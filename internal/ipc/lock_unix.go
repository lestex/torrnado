//go:build linux || darwin

package ipc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// acquireDaemonLock takes an exclusive, non-blocking flock on
// socketPath + ".lock", proving this process is the only daemon for that
// socket. The returned file must be kept open for the lifetime of the
// daemon - the lock is released when it is closed (or when the process
// dies, including on a panic or SIGKILL, which is the whole point).
//
// This exists because probing the socket is not a sound exclusion test.
// Dialing answers "is someone accepting right now", not "does a daemon
// own this socket": a daemon busy hash-checking a large torrent can fail
// to accept inside any timeout you pick, and the probe-then-unlink
// sequence is a TOCTOU race besides. Losing that gamble is expensive -
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

	// Retried briefly rather than failed on the first refusal. A real
	// competing daemon holds this for its whole life, so it still fails
	// below; what the retry is for is DaemonInfo, which answers "is one
	// running" by trying to take the same lock and dropping it again
	// microseconds later. Without this, asking whether a daemon is
	// running could stop one from starting.
	const attempts = 5
	for i := 0; ; i++ {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EWOULDBLOCK {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if i == attempts-1 {
			f.Close()
			return nil, fmt.Errorf("another daemon already holds %s", path)
		}
		time.Sleep(40 * time.Millisecond)
	}

	// The pid goes in the file so anything else can name the process
	// holding the lock. flock(2) has no "who holds this" call - fcntl
	// locks do, via F_GETLK, but they release on any close() of the file
	// anywhere in the process, which is a worse trade for a lock that has
	// to be held for the daemon's whole life.
	//
	// A failure here is not fatal: the lock is what provides exclusion,
	// and the pid is only there to be read by `torrnado status`.
	writeLockPID(f)
	return f, nil
}

// writeLockPID stamps the holder's pid into an already-locked file.
func writeLockPID(f *os.File) {
	if err := f.Truncate(0); err != nil {
		return
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		return
	}
	f.Sync()
}

// DaemonInfo reports whether a daemon holds socketPath's lock, and which
// process it is.
//
// Ownership is the lock, not the socket and not the pid file: only the
// process holding the flock is the daemon, and the lock is released by
// the kernel however that process dies. The pid is read from the file's
// contents and is advisory - a daemon from a release before this one
// holds the lock without having written anything, which is normal, since
// the daemon outliving its clients is the point of the whole design.
func DaemonInfo(socketPath string) (DaemonStatus, error) {
	path := socketPath + ".lock"
	st := DaemonStatus{LockPath: path}

	// No O_CREATE: asking whether a daemon is running should not leave a
	// file behind when the answer is no.
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil // never started here
		}
		return st, fmt.Errorf("open daemon lock %s: %w", path, err)
	}
	defer f.Close()

	// flock on a read-only descriptor is allowed; taking it is the only
	// way to ask whether anybody else has it. Held for as long as it
	// takes to fail, and dropped immediately when it succeeds.
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		return st, nil // nobody holds it; any pid in the file is stale
	}
	if err != unix.EWOULDBLOCK {
		return st, fmt.Errorf("lock %s: %w", path, err)
	}

	st.Running = true
	st.PID = readLockPID(f)
	return st, nil
}

// readLockPID returns the pid written in the lock file, or 0 when there
// is not a plausible one there.
func readLockPID(f *os.File) int {
	buf := make([]byte, 32)
	n, _ := f.ReadAt(buf, 0)
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
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
