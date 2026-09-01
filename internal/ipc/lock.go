package ipc

// DaemonStatus is what DaemonInfo reports: whether a daemon owns a
// socket, and which process it is.
//
// Declared outside the build-tagged files so both platforms describe the
// answer the same way, even where one of them cannot produce it.
type DaemonStatus struct {
	// Running is whether the daemon lock is held. This is the
	// authoritative answer - see DaemonInfo for why it is the lock rather
	// than the socket or the pid.
	Running bool
	// PID is the process holding the lock, or 0 when it cannot be named:
	// nothing is running, or the daemon is an older build that took the
	// lock without recording a pid.
	PID int
	// LockPath is the file consulted, worth reporting because it is
	// derived from the configured socket and a reader may be looking at
	// the wrong state directory.
	LockPath string
}
