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

// VPN gates transfers on the system being connected to a VPN.
type VPN struct {
	// Required holds every transfer -- download and upload -- while the
	// system is not on a VPN. Off by default: a guard nobody asked for
	// that stops downloads is worse than no guard at all.
	//
	// It stops piece data moving. Tracker announces and DHT traffic
	// continue regardless, so the swarm still learns the real address of
	// a blocked daemon; this is "nothing transfers off-VPN", not a
	// network kill switch. See docs/reference/limitations.md.
	Required bool `toml:"required"`
	// Interfaces names devices to count as a VPN whatever the system says
	// about them. The escape hatch for a tunnel the kernel cannot label:
	// policy-based IPsec moves traffic over the physical interface with
	// no tunnel device at all, and nothing can tell that apart from
	// having no VPN. Empty is the normal case -- detection needs no
	// configuration.
	Interfaces []string `toml:"interfaces"`
}

// Logging configures the daemon's output.
type Logging struct {
	// Level is the daemon's own verbosity: debug, info, warn or error.
	Level string `toml:"level"`
	// LibraryLevel is the same for the torrent library, which is far
	// noisier than we are -- it reports every tracker that misbehaves.
	// Kept separate so its warnings can be quietened without losing ours.
	LibraryLevel string `toml:"library_level"`
	// File redirects the log. Empty means stderr, which is what a service
	// manager wants: journald timestamps and stores it with no further
	// configuration.
	File string `toml:"file"`
}

// Config is the full contents of config.toml.
type Config struct {
	DownloadDir  string `toml:"download_dir"`
	DaemonSocket string `toml:"daemon_socket"`
	// StateDir is where the daemon keeps what it needs to come back after
	// a restart: the session file and a copy of each torrent's metainfo.
	StateDir string `toml:"state_dir"`
	Theme    string `toml:"theme"`
	// Player is the command run to preview a file, given the stream URL
	// as its final argument. May carry fixed flags ("mpv --no-terminal");
	// it is split on spaces, not run through a shell.
	Player string `toml:"player"`

	RateLimit RateLimits        `toml:"rate_limit"`
	Port      PortRange         `toml:"port"`
	Network   Network           `toml:"network"`
	VPN       VPN               `toml:"vpn"`
	Log       Logging           `toml:"log"`
	Keybinds  map[string]string `toml:"keybinds"`
}

// LogLevels are the accepted values for log.level and log.library_level.
var LogLevels = []string{"debug", "info", "warn", "error"}

func validLogLevel(s string) bool {
	for _, l := range LogLevels {
		if s == l {
			return true
		}
	}
	return false
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
	"search", "command",
	"detail", "back", "quit",
	"help", "preview",
	"focus_next", "focus_prev",
	"tab_next", "tab_prev",
	"filter_next", "filter_prev",
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
	stateDir, err := DefaultDataDir()
	if err != nil {
		return Config{}, err
	}
	return Config{
		DownloadDir:  downloadDir,
		DaemonSocket: socket,
		StateDir:     stateDir,
		Theme:        "dracula",
		Player:       "mpv",
		Keybinds:     map[string]string{},
		RateLimit:    RateLimits{Upload: 0, Download: 0},
		Port:         PortRange{Low: 51413, High: 51433},
		Network:      Network{DHT: true, PEX: true, LSD: true, Encryption: true, Seed: true},
		// Off, so installing torrnado never stops a download for a reason
		// the user did not ask for. Turning it on is a deliberate act.
		VPN: VPN{Required: false},
		// The library warns about every tracker that misbehaves, which is
		// noise unless you are chasing one, so it starts a level above us.
		Log: Logging{Level: "info", LibraryLevel: "warn"},
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
	if strings.TrimSpace(c.Player) == "" {
		return fmt.Errorf("player: must not be empty")
	}
	if c.StateDir == "" {
		return fmt.Errorf("state_dir: must not be empty")
	}
	if !validLogLevel(c.Log.Level) {
		return fmt.Errorf("log.level: %q is not one of %s", c.Log.Level, strings.Join(LogLevels, ", "))
	}
	if !validLogLevel(c.Log.LibraryLevel) {
		return fmt.Errorf("log.library_level: %q is not one of %s", c.Log.LibraryLevel, strings.Join(LogLevels, ", "))
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

	// An empty name would match no interface and silently weaken a guard
	// the user believes they configured.
	for i, name := range c.VPN.Interfaces {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("vpn.interfaces[%d]: must not be empty", i)
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
