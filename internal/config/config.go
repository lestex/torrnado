// Package config loads and validates torrnado's TOML configuration.
package config

import (
	"fmt"
	"net/url"
	"os"
	"runtime"
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
	DHT        bool `toml:"dht"`
	PEX        bool `toml:"pex"`
	Encryption bool `toml:"encryption"`
	Seed       bool `toml:"seed"`
}

// SeedLimits stop seeding a torrent once it has given enough back.
//
// Both are off by default. They pause the torrent rather than holding it
// the way the VPN and disk guards do: those are conditions of the machine
// that come and go, this is a decision that a torrent is finished, and it
// should survive a restart rather than being undone by one.
type SeedLimits struct {
	// Ratio stops a torrent once uploaded/downloaded reaches it. Zero is
	// off. Only ever applied to a torrent that has finished downloading -
	// a ratio means nothing while the denominator is still moving.
	Ratio float64 `toml:"ratio"`
	// Time stops a torrent once it has been seeding for this long,
	// counted from when it completed. Zero is off.
	Time Duration `toml:"time"`
}

// VPN gates transfers on the system being connected to a VPN.
type VPN struct {
	// Required holds every transfer - download and upload - while the
	// system is not on a VPN. Off by default: a guard nobody asked for
	// that stops downloads is worse than no guard at all.
	//
	// It stops piece data moving. Tracker announces and DHT traffic
	// continue regardless, so the swarm still learns the real address of
	// a blocked daemon; this is "nothing transfers off-VPN", not a
	// network kill switch.
	Required bool `toml:"required"`
	// Interfaces names devices to count as a VPN whatever the system says
	// about them. The escape hatch for a tunnel the kernel cannot label:
	// policy-based IPsec moves traffic over the physical interface with
	// no tunnel device at all, and nothing can tell that apart from
	// having no VPN. Empty is the normal case - detection needs no
	// configuration.
	Interfaces []string `toml:"interfaces"`
}

// Logging configures the daemon's output.
type Logging struct {
	// Level is the daemon's own verbosity: debug, info, warn or error.
	Level string `toml:"level"`
	// LibraryLevel is the same for the torrent library, which is far
	// noisier than we are - it reports every tracker that misbehaves.
	// Kept separate so its warnings can be quietened without losing ours.
	LibraryLevel string `toml:"library_level"`
	// File redirects the log. Empty means stderr, which is what a service
	// manager wants: journald timestamps and stores it with no further
	// configuration.
	File string `toml:"file"`
}

// Indexer points `torrnado find` at a self-hosted torrent aggregator -
// Jackett or Prowlarr - which fans one query out to every tracker the
// user has configured in it.
//
// torrnado never contacts a tracker itself: find makes a single HTTP GET
// against this URL, usually on localhost. That is why it is not gated on
// [vpn] the way transfers are - the request that reaches a tracker is the
// aggregator's, over the aggregator's route.
//
// All three parts are empty by default, and an empty table is not a
// misconfiguration: most installations have no aggregator at all, and
// find says what to put here rather than failing obscurely.
type Indexer struct {
	// Backend is "jackett" or "prowlarr". There is no default, because
	// the two listen on different ports and speak incompatible APIs -
	// guessing from a URL alone would produce a 404 instead of an error
	// naming the key that is missing.
	Backend string `toml:"backend"`
	// URL is the aggregator's base address, e.g. http://localhost:9117.
	URL string `toml:"url"`
	// APIKey is from the aggregator's own settings page. It is the first
	// secret this file holds, so `torrnado config` reports only whether
	// it is set.
	APIKey string `toml:"api_key"`
}

// Config is the full contents of config.toml.
type Config struct {
	DownloadDir  string `toml:"download_dir"`
	DaemonSocket string `toml:"daemon_socket"`
	// WatchDir is a directory the daemon adds .torrent files from as they
	// appear, so anything that can write a file can add a torrent -
	// rsync, a synced download folder, a file manager on a share. Empty
	// watches nothing.
	//
	// The point is a box nobody logs in to: without it the only ways in
	// are `torrnado add` over SSH and the TUI.
	WatchDir string `toml:"watch_dir"`

	// MinFreeSpace holds every transfer while the download directory's
	// filesystem has less than this free. Zero, the default, never holds
	// anything.
	//
	// A guard rather than a per-torrent check because free space is a
	// property of the disk, not of any one torrent: the torrent that
	// fills it is rarely the one you would have chosen to stop.
	MinFreeSpace Size `toml:"min_free_space"`
	// StateDir is where the daemon keeps what it needs to come back after
	// a restart: the session file and a copy of each torrent's metainfo.
	StateDir string `toml:"state_dir"`
	Theme    string `toml:"theme"`
	// Player is the command run to preview a file, given the stream URL.
	// May carry fixed flags ("mpv --no-terminal"); it is split on spaces,
	// not run through a shell. %f is replaced by the URL, or the URL is
	// appended when the command does not name it.
	Player string `toml:"player"`
	// Opener is the command run to show a torrent's folder, given the
	// directory the same way Player is given the URL - so
	// "alacritty --working-directory %f" opens a terminal there instead
	// of a file manager.
	//
	// The default is the platform's own "open this with whatever is
	// registered": `open` on macOS, `xdg-open` elsewhere, which is the
	// user's file manager rather than one this program picked for them.
	Opener string `toml:"opener"`

	// OnComplete runs once when a torrent finishes, given its data
	// directory the same way Player and Opener are given theirs: split on
	// spaces, %f substituted, never through a shell. Empty runs nothing.
	//
	// The name, id and size also arrive as TORRNADO_* environment
	// variables, so a notification script does not have to parse them
	// back out of a path.
	OnComplete string `toml:"on_complete"`

	RateLimit RateLimits        `toml:"rate_limit"`
	SeedLimit SeedLimits        `toml:"seed_limit"`
	Port      PortRange         `toml:"port"`
	Network   Network           `toml:"network"`
	Indexer   Indexer           `toml:"indexer"`
	VPN       VPN               `toml:"vpn"`
	Log       Logging           `toml:"log"`
	Keybinds  map[string]string `toml:"keybinds"`
}

// LogLevels are the accepted values for log.level and log.library_level.
var LogLevels = []string{"debug", "info", "warn", "error"}

// IndexerBackends are the accepted values for indexer.backend.
//
// Listed here rather than taken from internal/indexer for the same
// reason as KnownActions: config validates a file without importing the
// package that implements the thing, so `torrnado daemon` does not link
// an HTTP client for an indexer it will never call. A test in
// cmd/torrnado, which imports both, keeps the two lists in agreement.
var IndexerBackends = []string{"jackett", "prowlarr"}

func validIndexerBackend(s string) bool {
	for _, b := range IndexerBackends {
		if s == b {
			return true
		}
	}
	return false
}

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
// file without importing it - which also keeps `torrnado daemon`, which
// has no interface at all, from pulling one in.
var KnownActions = []string{
	"up", "down", "top", "bottom",
	"select", "remove", "remove_data", "purge",
	"pause", "recheck",
	"search", "command",
	"detail", "back", "quit",
	"help", "help_alt", "preview", "open",
	"focus_next", "focus_prev",
	"tab_next", "tab_prev",
	"tab_pieces", "tab_peers", "tab_files",
	"toggle_mouse",
	"filter_next", "filter_prev",
}

// defaultOpener is the platform's own "open this the way the desktop
// would" command.
//
// xdg-open rather than a named file manager: it launches whatever this
// user has registered - nautilus on GNOME, dolphin on KDE, thunar on
// XFCE - which is a better guess than any one of them, and naming a
// specific one is what the config key is for.
func defaultOpener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
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
		// The one theme that is right on a machine it was not chosen on:
		// it takes its colors from the terminal's own palette. A named
		// palette here would be a guess about the terminal.
		Theme:     "terminal",
		Player:    "mpv",
		Opener:    defaultOpener(),
		Keybinds:  map[string]string{},
		RateLimit: RateLimits{Upload: 0, Download: 0},
		Port:      PortRange{Low: 51413, High: 51433},
		// Seed off by default. It is the one network setting that keeps
		// costing something after the work is done - bandwidth, an open
		// port, and a continuing announcement of what this machine has -
		// and a default should not commit someone to that indefinitely
		// without their having asked.
		//
		// It is narrower than "do not upload": the library only treats
		// this as altruistic seeding after a torrent completes. While
		// downloading, peers are still uploaded to in order to encourage
		// them to reciprocate, so turning it off is not leeching.
		Network: Network{DHT: true, PEX: true, Encryption: true, Seed: false},
		// Off, so installing torrnado never stops a download for a reason
		// the user did not ask for. Turning it on is a deliberate act.
		VPN: VPN{Required: false},
		// The library warns about every tracker that misbehaves, which is
		// noise unless you are chasing one, so it starts a level above us.
		Log: Logging{Level: "info", LibraryLevel: "warn"},
	}, nil
}

// Load reads and validates the config file at path. If the file doesn't
// exist, it returns Default() with no error - a missing config is not a
// failure, an invalid one is.
//
// An unknown key is one of those failures, including a key this schema
// used to have: `network.lsd` was removed rather than kept as a no-op, so
// a config still carrying it does not load. The error names the key, and
// deleting the line is the whole fix.
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

	// Before Validate, so the checks below see the path that will actually
	// be used rather than the one that was written down.
	if err := cfg.expandPaths(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
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
	if strings.TrimSpace(c.Opener) == "" {
		return fmt.Errorf("opener: must not be empty")
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

	if c.SeedLimit.Ratio < 0 {
		return fmt.Errorf("seed_limit.ratio: must not be negative")
	}
	if c.SeedLimit.Time < 0 {
		return fmt.Errorf("seed_limit.time: must not be negative")
	}
	if c.MinFreeSpace < 0 {
		return fmt.Errorf("min_free_space: must not be negative")
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

	// The indexer is opt-in, so a wholly empty table has to pass - and
	// that is load-bearing rather than lenient: Template(Default())
	// writes an empty one, and the template tests load what it wrote, so
	// rejecting it would mean `torrnado init` producing a file that will
	// not start. Validate only once the user has said something.
	if ix := c.Indexer; ix.Backend != "" || ix.URL != "" || ix.APIKey != "" {
		if ix.Backend == "" {
			// No safe guess: see the note on Indexer.Backend.
			return fmt.Errorf("indexer.backend: must be set when indexer.url is (one of %s)",
				strings.Join(IndexerBackends, ", "))
		}
		if !validIndexerBackend(ix.Backend) {
			return fmt.Errorf("indexer.backend: %q is not one of %s",
				ix.Backend, strings.Join(IndexerBackends, ", "))
		}
		if ix.URL == "" {
			return fmt.Errorf("indexer.url: must be set when indexer.backend is, e.g. http://localhost:9117")
		}
		u, err := url.Parse(ix.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("indexer.url: %q must be an http(s) URL, e.g. http://localhost:9117", ix.URL)
		}
		if ix.APIKey == "" {
			// Both products require one, so an empty key only buys a 401
			// at the first search - later, and further from the cause.
			return fmt.Errorf("indexer.api_key: must be set when indexer.url is (from the aggregator's settings page)")
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
