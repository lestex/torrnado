package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// xdgConfigHome resolves $XDG_CONFIG_HOME, defaulting to ~/.config. Go's
// os.UserConfigDir() only honours XDG_CONFIG_HOME on Linux (on macOS it
// returns ~/Library/Application Support, ignoring the env var), but the
// spec here wants XDG compliance everywhere, so this is resolved by hand.
func xdgConfigHome() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// xdgDataHome resolves $XDG_DATA_HOME, defaulting to ~/.local/share, for
// the same reason as xdgConfigHome.
func xdgDataHome() (string, error) {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

// DefaultPath returns the config file path: $XDG_CONFIG_HOME/torrnado/config.toml.
func DefaultPath() (string, error) {
	dir, err := xdgConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "torrnado", "config.toml"), nil
}

// DefaultThemesDir returns $XDG_CONFIG_HOME/torrnado/themes.
func DefaultThemesDir() (string, error) {
	dir, err := xdgConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "torrnado", "themes"), nil
}

// DefaultSocketPath returns $XDG_DATA_HOME/torrnado/daemon.sock.
func DefaultSocketPath() (string, error) {
	dir, err := xdgDataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "torrnado", "daemon.sock"), nil
}

// DefaultDataDir returns $XDG_DATA_HOME/torrnado (used for the daemon's
// log file and other run-time state alongside the socket).
func DefaultDataDir() (string, error) {
	dir, err := xdgDataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "torrnado"), nil
}

// DefaultDownloadDir returns ~/Downloads/torrnado.
func DefaultDownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads", "torrnado"), nil
}

// ExpandHome replaces a leading ~ with the user's home directory.
//
// A shell does this before a program ever sees an argument, which makes a
// config file the one place a path written "~/Downloads" arrives with the
// tilde still on it. Taken literally it is a perfectly valid relative
// path, so nothing fails: the daemon creates a directory actually called
// "~" beside whatever its working directory happens to be, and the
// downloads go somewhere nobody thinks to look. The sample config in the
// docs is written with tildes, so this is the shape people copy.
//
// Only a leading "~" or "~/" is touched. "~user" is left alone: resolving
// another account's home needs the user database, and a path beginning
// with a tilde that is not the current user's home is likelier to be a
// filename than a request anyone means.
func ExpandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

// expandPaths resolves a leading ~ in every value naming a file or a
// directory.
//
// Player and Opener are left out on purpose. They are commands, split on
// spaces and free to carry flags, so a tilde in one is not necessarily at
// the front of a path - and the thing they are pointed at is a binary on
// $PATH far more often than a file in a home directory.
func (c *Config) expandPaths() error {
	for _, field := range []*string{&c.DownloadDir, &c.DaemonSocket, &c.StateDir, &c.Log.File, &c.WatchDir} {
		v, err := ExpandHome(*field)
		if err != nil {
			return err
		}
		*field = v
	}
	return nil
}
