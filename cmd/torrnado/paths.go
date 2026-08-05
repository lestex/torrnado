package main

import (
	"os"
	"path/filepath"
)

// Where the daemon keeps its socket and where downloads go.
//
// These are hardcoded for now. They become configurable later; keeping
// them in one place means that change lands here and nowhere else.

// socketPath is the Unix socket clients dial to reach the daemon.
func socketPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.sock"), nil
}

// dataDir is where torrnado keeps its own files (the socket, logs).
// It follows the XDG base directory spec: $XDG_DATA_HOME if set,
// otherwise ~/.local/share.
func dataDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "torrnado"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "torrnado"), nil
}

// downloadDir is where torrent data is written.
func downloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads", "torrnado"), nil
}
