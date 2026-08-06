// Package engine tracks torrents and reports their state behind a small
// API plus an event channel. Everything else in torrnado -- the daemon's
// RPC layer, the CLI, the TUI -- goes through here rather than touching
// torrents directly.
package engine

import "time"

// Priority is how badly a file's data is wanted, from not at all through
// to immediately. It applies to a whole file at a time.
type Priority int

const (
	PriorityNone Priority = iota
	PriorityLow
	PriorityNormal
	PriorityHigh
	PriorityNow
)

func (p Priority) String() string {
	switch p {
	case PriorityNone:
		return "none"
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	case PriorityNow:
		return "now"
	default:
		return "unknown"
	}
}

// ParsePriority parses a priority name (case-insensitive) into a Priority.
func ParsePriority(s string) (Priority, bool) {
	switch s {
	case "none", "skip":
		return PriorityNone, true
	case "low":
		return PriorityLow, true
	case "normal", "":
		return PriorityNormal, true
	case "high":
		return PriorityHigh, true
	case "now", "max":
		return PriorityNow, true
	default:
		return 0, false
	}
}

// State is the coarse lifecycle state of a torrent: the one word worth
// showing a user in a list.
type State int

const (
	StateChecking State = iota
	StateQueued
	StatePaused
	StateDownloading
	StateSeeding
	StateError
)

func (s State) String() string {
	switch s {
	case StateChecking:
		return "checking"
	case StateQueued:
		return "queued"
	case StatePaused:
		return "paused"
	case StateDownloading:
		return "downloading"
	case StateSeeding:
		return "seeding"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// TorrentID identifies a torrent by its hex-encoded v1 info hash. Using a
// plain string keeps this package's API -- and the wire protocol built on
// top of it -- free of any torrent library's own types.
type TorrentID string

// TorrentSnapshot is a point-in-time view of a torrent's state, cheap to
// copy and safe to send across goroutines / the IPC wire.
type TorrentSnapshot struct {
	ID          TorrentID
	Name        string
	InfoHash    string
	TotalLength int64
	Completed   int64
	Progress    float64 // 0..1
	DownloadBPS float64
	UploadBPS   float64
	Downloaded  int64 // cumulative, this session
	Uploaded    int64 // cumulative, this session
	Ratio       float64
	NumPeers    int
	NumSeeds    int
	ETA         time.Duration // 0 if unknown/infinite
	State       State
	// Paused is the authoritative pause flag. State can't stand in for it:
	// State reports Checking (or Error) in preference to Paused, so a
	// paused torrent being rechecked reads as "checking", and anything
	// inferring pausedness from State would conclude it is running.
	Paused        bool
	SavePath      string
	AddedAt       time.Time
	Error         string
	DownloadLimit int64 // bytes/sec, 0 = unlimited
	UploadLimit   int64 // bytes/sec, 0 = unlimited
}

// FileInfo describes one file within a torrent.
type FileInfo struct {
	Index     int
	Path      string
	Length    int64
	Completed int64
	Priority  Priority
}

// PeerInfo describes one connected peer.
type PeerInfo struct {
	Addr        string
	Client      string
	Source      string
	DownloadBPS float64
	Progress    float64
	Encrypted   bool
}

// TrackerInfo describes one tracker URL known to a torrent.
type TrackerInfo struct {
	URL  string
	Tier int
}

// TorrentDetail is the full detail view for one torrent: its files, peers
// and trackers, in addition to its snapshot.
type TorrentDetail struct {
	Snapshot TorrentSnapshot
	Files    []FileInfo
	Peers    []PeerInfo
	Trackers []TrackerInfo
}

// GlobalStats aggregates state across all torrents plus daemon-wide info.
type GlobalStats struct {
	DownloadBPS    float64
	UploadBPS      float64
	TotalDownload  int64
	TotalUpload    int64
	NumTorrents    int
	DiskFreeBytes  int64
	DiskTotalBytes int64
	ListenPort     int
	DhtNodes       int
	// UploadLimit/DownloadLimit are the current global rate caps in
	// bytes/sec (0 = unlimited).
	UploadLimit   int64
	DownloadLimit int64
}

// Event is broadcast to subscribers whenever engine state changes: on a
// periodic tick (for live speed/progress/ETA) and immediately after any
// mutating call (add/remove/pause/...) so the UI feels responsive rather
// than waiting for the next tick.
type Event struct {
	Torrents []TorrentSnapshot
	Global   GlobalStats
	At       time.Time
}

// AddOpts controls how a torrent is added.
type AddOpts struct {
	// SavePath, if set, overrides the engine's default download directory
	// for this torrent.
	SavePath string
	// Paused adds the torrent but leaves it paused (no download/upload).
	Paused bool
}
