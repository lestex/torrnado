package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lestex/torrnado/internal/ipc"
)

// dialOrSpawn connects to a running daemon, starting one first if nothing
// answers.
//
// This is why a torrent keeps downloading after the command that added it
// exits: the work happens in a separate, long-lived process, and every
// command here is a thin client to it.
func dialOrSpawn() (*ipc.Client, error) {
	sock, err := socketPath()
	if err != nil {
		return nil, err
	}

	if c, err := ipc.Dial(sock); err == nil {
		return c, nil
	}
	if err := spawnDaemon(); err != nil {
		return nil, fmt.Errorf("spawn daemon: %w", err)
	}

	// The daemon needs a moment to bind its socket, so retry rather than
	// failing on the first attempt.
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		c, err := ipc.Dial(sock)
		if err == nil {
			return c, nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("daemon did not come up on %s: %w", sock, lastErr)
}

// spawnDaemon launches `torrnado daemon` as a detached background
// process, logging to the data directory.
func spawnDaemon() error {
	// The daemon is this same binary with a different subcommand, so ask
	// the OS where we are rather than hunting for it on PATH.
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	dir, err := dataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	logFile, err := os.OpenFile(filepath.Join(dir, "daemon.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "daemon")
	// The child must not share our terminal: it outlives us, and anything
	// it printed would land in the middle of a later shell prompt.
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	detachProcess(cmd)

	if err := cmd.Start(); err != nil {
		return err
	}
	// Release rather than Wait: we are about to exit, and the daemon is
	// meant to carry on without us. Releasing also means no zombie is
	// left behind for a parent that will not be around to reap it.
	return cmd.Process.Release()
}
