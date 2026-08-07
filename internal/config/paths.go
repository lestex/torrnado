package config

import (
	"os"
	"path/filepath"
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
