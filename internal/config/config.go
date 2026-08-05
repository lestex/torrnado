// Package config loads and validates torrnado's TOML configuration.
package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// PortRange bounds the ports the engine tries to bind for BitTorrent/uTP/DHT
// traffic.
type PortRange struct {
	Low  int `toml:"low"`
	High int `toml:"high"`
}

// RateLimits holds the global (client-wide) upload/download caps.
type RateLimits struct {
	Upload   Rate `toml:"upload"`
	Download Rate `toml:"download"`
}

// Network toggles the peer-discovery and transport features anacrolix/torrent
// supports globally (see internal/engine.Config for the caveats on each).
type Network struct {
	DHT bool `toml:"dht"`
	PEX bool `toml:"pex"`
	// LSD is accepted and validated like any other key, but has no effect:
	// anacrolix/torrent does not implement Local Service Discovery. Kept
	// in the schema so config files written against the spec don't fail
	// validation, and so the limitation is discoverable from `torrnado
	// -help-config` / the README rather than silently ignored.
	LSD        bool `toml:"lsd"`
	Encryption bool `toml:"encryption"`
	Seed       bool `toml:"seed"`
}

// Config is the full contents of config.toml.
type Config struct {
	DownloadDir  string `toml:"download_dir"`
	DaemonSocket string `toml:"daemon_socket"`
	Theme        string `toml:"theme"`

	RateLimit RateLimits        `toml:"rate_limit"`
	Port      PortRange         `toml:"port"`
	Network   Network           `toml:"network"`
	Keybinds  map[string]string `toml:"keybinds"`
}

// KnownActions are the TUI actions that may appear as keys in [keybinds].
//
// Defined here rather than in the TUI package so config can validate a
// file without importing it -- which also keeps `torrnado daemon`, which
// has no interface at all, from pulling one in.
var KnownActions = []string{
	"up", "down", "top", "bottom",
	"select", "remove", "remove_data",
	"pause", "recheck",
	"search", "back", "quit",
	"help",
}

// Default returns a Config with every field set to its XDG-aware default.
func Default() (Config, error) {
	downloadDir, err := DefaultDownloadDir()
	if err != nil {
		return Config{}, err
	}
	socket, err := DefaultSocketPath()
	if err != nil {
		return Config{}, err
	}
	return Config{
		DownloadDir:  downloadDir,
		DaemonSocket: socket,
		Theme:        "dracula",
		Keybinds:     map[string]string{},
		RateLimit:    RateLimits{Upload: 0, Download: 0},
		Port:         PortRange{Low: 51413, High: 51433},
		Network:      Network{DHT: true, PEX: true, LSD: true, Encryption: true, Seed: true},
	}, nil
}

// Load reads and validates the config file at path. If the file doesn't
// exist, it returns Default() with no error -- a missing config is not a
// failure, an invalid one is.
func Load(path string) (Config, error) {
	cfg, err := Default()
	if err != nil {
		return Config{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	meta, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		sort.Strings(keys)
		return Config{}, fmt.Errorf("config %s: unknown key(s): %s", path, strings.Join(keys, ", "))
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// Validate checks value ranges and cross-field constraints that the TOML
// decoder itself can't enforce, returning an error that names the
// offending key.
func (c Config) Validate() error {
	if c.DownloadDir == "" {
		return fmt.Errorf("download_dir: must not be empty")
	}
	if c.DaemonSocket == "" {
		return fmt.Errorf("daemon_socket: must not be empty")
	}
	if c.Theme == "" {
		return fmt.Errorf("theme: must not be empty")
	}

	if c.RateLimit.Upload < 0 {
		return fmt.Errorf("rate_limit.upload: must not be negative")
	}
	if c.RateLimit.Download < 0 {
		return fmt.Errorf("rate_limit.download: must not be negative")
	}

	if c.Port.Low != 0 || c.Port.High != 0 {
		if c.Port.Low < 1 || c.Port.Low > 65535 {
			return fmt.Errorf("port.low: %d is out of range 1-65535", c.Port.Low)
		}
		if c.Port.High < 1 || c.Port.High > 65535 {
			return fmt.Errorf("port.high: %d is out of range 1-65535", c.Port.High)
		}
		if c.Port.High < c.Port.Low {
			return fmt.Errorf("port.high (%d) must be >= port.low (%d)", c.Port.High, c.Port.Low)
		}
	}

	// A binding naming an action that does not exist is a typo, and
	// silently ignoring it leaves a key the user believes they rebound
	// still doing the old thing.
	known := make(map[string]bool, len(KnownActions))
	for _, a := range KnownActions {
		known[a] = true
	}
	for action, key := range c.Keybinds {
		if !known[action] {
			return fmt.Errorf("keybinds.%s: unknown action (known actions: %s)", action, strings.Join(KnownActions, ", "))
		}
		if key == "" {
			return fmt.Errorf("keybinds.%s: must not be empty", action)
		}
	}

	return nil
}
