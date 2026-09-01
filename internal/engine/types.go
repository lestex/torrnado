// Package engine tracks torrents and reports their state behind a small
// API plus an event channel. Everything else in torrnado - the daemon's
// RPC layer, the CLI, the TUI - goes through here rather than touching
// torrents directly.
package engine

import (
	"fmt"
	"time"
)

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
	// StateBlocked is a torrent held by the VPN guard: it would be
	// running, but the system is not on a VPN and config says it must be.
	//
	// Appended rather than slotted in beside StatePaused, because these
	// values go over the wire as gob-encoded ints: inserting one would
	// silently renumber every state above it, and a daemon that has been
	// up for a week talking to a freshly built client would report
	// seeding torrents as errored.
	StateBlocked
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
	case StateBlocked:
		return "blocked"
	default:
		return "unknown"
	}
}

// TorrentID identifies a torrent by its hex-encoded v1 info hash. Using a
// plain string keeps this package's API - and the wire protocol built on
// top of it - free of any torrent library's own types.
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
	Paused bool
	// Checking reports whether a hash check is actually running, which
	// State cannot: a torrent waiting for a magnet's metadata is also
	// StateChecking, and nothing is being verified there.
	Checking bool
	// CheckProgress is how far a running hash check has got, 0..1. Only
	// meaningful while Checking is set.
	CheckProgress float64
	SavePath      string
	// DataDir is the directory holding this torrent's files: its own
	// folder under SavePath for a multi-file torrent, SavePath itself for
	// a single-file one. Computed by the daemon so a client does not have
	// to guess which shape it is looking at.
	DataDir       string
	AddedAt       time.Time
	Error         string
	DownloadLimit int64 // bytes/sec, 0 = unlimited
	UploadLimit   int64 // bytes/sec, 0 = unlimited
}

// StatusText is the state as a user should see it, with hash-check
// progress folded in while one is running.
//
// Lives here rather than in either UI so the TUI and `torrnado list`
// cannot word it differently.
func (s TorrentSnapshot) StatusText() string {
	// Keyed off Checking, not off the progress being above zero: a check
	// that has just started is at 0%, and reporting that as "no progress
	// information" left a long verify showing a bare "checking" - the
	// one moment a user most wants a number. A torrent waiting on a
	// magnet's metadata is StateChecking with nothing being checked, and
	// still reads as plain "checking".
	//
	// The second half of the condition is for a daemon older than the
	// Checking field. The daemon outlives the client by design - that is
	// the point of the whole split - so a new TUI talking to a daemon
	// that has been up for a week is normal, and gob leaves a field it
	// has never heard of at its zero value. Anything the daemon can still
	// tell us is worth using.
	if s.Checking || (s.State == StateChecking && s.CheckProgress > 0) {
		return fmt.Sprintf("checking %d%%", int(s.CheckProgress*100))
	}
	return s.State.String()
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
//
// Note the absence of choked/choking flags: anacrolix/torrent keeps both
// (Peer.choking, Peer.peerChoking) unexported with no accessor, and the
// only public path to that information is Client.WriteStatus, which dumps
// the whole client's status as free text. Rather than scrape it, this
// omits the field.
type PeerInfo struct {
	Addr string
	// Client is the peer's self-reported name from the extended handshake
	// (BEP 10), falling back to the printable prefix of its peer ID (BEP
	// 20, e.g. "-qB5210-") when no extended handshake has happened.
	Client string
	// PeerID is the printable prefix of the peer's 20-byte peer ID.
	PeerID      string
	Source      string
	DownloadBPS float64
	UploadBPS   float64
	PiecesHave  int
	PiecesTotal int
	Progress    float64
	Encrypted   bool
}

// PieceRun is a run-length-encoded span of consecutive pieces sharing the
// same state, mirroring torrent.PieceStateRun.
//
// The library's own torrent.PieceState cannot go on the gob wire: it
// embeds storage.Completion, which carries an `error` field, and gob
// refuses interface values whose concrete types aren't registered. Runs
// also keep the wire small - a 50k-piece torrent is a handful of entries
// rather than 50 KB pushed every second.
type PieceRun struct {
	Length int // number of consecutive pieces in this run
	// Known is storage.Completion.Ok: whether the library has actually
	// consulted storage for these pieces yet. It gates Complete, and it
	// is false far more often than you'd expect - a piece stays unknown
	// until something touches it, so a torrent that has finished
	// downloading still reports most of its pieces as unknown until peers
	// request them back. Treating unknown as "missing" would draw a
	// finished torrent as a half-empty map, so it is a state of its own.
	Known    bool
	Complete bool
	Partial  bool // some data obtained, but not the whole piece
	Checking bool // hashing or queued for hashing
}

// TrackerInfo describes one tracker URL known to a torrent.
type TrackerInfo struct {
	URL  string
	Tier int
}

// TorrentDetail is the full detail view for one torrent: files, peers,
// trackers and piece states, in addition to its snapshot.
type TorrentDetail struct {
	Snapshot TorrentSnapshot
	Files    []FileInfo
	Peers    []PeerInfo
	Trackers []TrackerInfo
	// Pieces is the run-length-encoded piece map; the run lengths sum to
	// NumPieces. Both are zero until metadata arrives.
	Pieces      []PieceRun
	NumPieces   int
	PieceLength int64
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
	// VPNRequired is whether the VPN guard is switched on at all. Without
	// it a client cannot tell "not on a VPN" from "not looking", and would
	// have to show an alarming red nothing to everyone who never asked for
	// the guard.
	VPNRequired bool
	// VPNActive is the last verdict, and VPNInterface what traffic would
	// leave by. Only meaningful while VPNRequired: the check is not run
	// otherwise.
	VPNActive    bool
	VPNInterface string
	// UploadLimit/DownloadLimit are the current global rate caps in
	// bytes/sec (0 = unlimited).
	UploadLimit   int64
	DownloadLimit int64
	// Version is the daemon's own build string, and StartedAt when its
	// engine came up. Both are here because the daemon outlives its
	// clients by design: a CLI built this afternoon routinely talks to
	// one started last week, and until now nothing on the wire said so.
	//
	// A daemon older than these fields leaves them at their zero values,
	// which is what gob does with a field it has never heard of - so a
	// reader must treat empty as "that daemon cannot say" rather than as
	// a fact about it.
	Version   string
	StartedAt time.Time
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

// VPNStatus is what a Config.VPNCheck reports: whether the system's
// traffic leaves through a VPN, by which interface, and why not when it
// does not.
//
// Declared here rather than taken from internal/vpn so this package keeps
// no internal dependencies - the daemon converts one to the other. It
// also means a test can drive the guard with a closure instead of a
// network.
type VPNStatus struct {
	Active    bool
	Interface string
	// Reason explains a false Active, for the log line the daemon writes
	// when transfers are held.
	Reason string
}

// AddOpts controls how a torrent is added.
type AddOpts struct {
	// SavePath, if set, overrides the engine's default download directory
	// for this torrent.
	SavePath string
	// Paused adds the torrent but leaves it paused (no download/upload).
	Paused bool
}
