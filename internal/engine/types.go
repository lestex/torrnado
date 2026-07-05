// Package engine tracks torrents and reports their state behind a small
// API plus an event channel. Everything else in torrnado -- the daemon's
// RPC layer, the CLI, the TUI -- goes through here rather than touching
// torrents directly.
package engine

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
