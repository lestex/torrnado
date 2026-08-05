// Package ipc implements the daemon <-> client RPC protocol: a small,
// hand-rolled call/reply/event protocol carried by encoding/gob over a
// Unix domain socket. Deliberately not net/rpc: net/rpc is request/reply
// only, and the TUI needs the daemon to push state changes to it
// (Engine's event channel) over the same long-lived connection used for
// commands. Deliberately not REST/HTTP: this is a local-machine IPC
// channel, and HTTP's overhead (framing, headers, a server mux) buys
// nothing here.
package ipc

import (
	"time"

	"github.com/lestex/torrnado/internal/engine"
)

// Method names the daemon-side engine operation a Request invokes.
type Method string

const (
	MethodPing                   Method = "Ping"
	MethodAddMagnet              Method = "AddMagnet"
	MethodAddTorrentFile         Method = "AddTorrentFile"
	MethodAddBatch               Method = "AddBatch"
	MethodRemove                 Method = "Remove"
	MethodSetPaused              Method = "SetPaused"
	MethodForceRecheck           Method = "ForceRecheck"
	MethodSetFilePriority        Method = "SetFilePriority"
	MethodSetGlobalUploadLimit   Method = "SetGlobalUploadLimit"
	MethodSetGlobalDownloadLimit Method = "SetGlobalDownloadLimit"
	MethodSetTorrentRateLimit    Method = "SetTorrentRateLimit"
	MethodMoveStorage            Method = "MoveStorage"
	MethodList                   Method = "List"
	MethodDetail                 Method = "Detail"
	MethodPreviewURL             Method = "PreviewURL"
)

// Request is a kitchen-sink call envelope: only the fields relevant to
// Method are populated. A single flat struct (rather than one type per
// method, carried in an interface{} field) keeps the wire format simple
// and gob-friendly -- gob can't encode interface values without every
// concrete type being registered up front, and a discriminated union of
// plain fields sidesteps that entirely.
type Request struct {
	Seq    uint64
	Method Method

	ID      string   // torrent id (hex infohash) for single-torrent ops
	Source  string   // magnet uri or .torrent file path, for single add
	Sources []string // for AddBatch, already expanded by the caller
	Opts    engine.AddOpts

	DeleteData bool
	Paused     bool

	FileIndex int
	Priority  engine.Priority

	UploadBps   int64
	DownloadBps int64

	NewDir string
}

// Response is the reply envelope. Err is set (and OK false) on failure;
// other fields are populated according to which Method was called.
type Response struct {
	Seq uint64
	OK  bool
	Err string

	ID string
	// AddBatch reports both halves of a partial success: the ids that
	// were added, and a message per source that failed. They are not
	// index-aligned -- a batch can half work, and both facts matter.
	IDs  []string
	Errs []string

	Snapshot []engine.TorrentSnapshot
	Detail   *engine.TorrentDetail

	// URL is the loopback stream URL for PreviewURL. The bytes never
	// cross this socket -- see internal/stream for why they cannot.
	URL string
}

type msgKind byte

const (
	kindCall msgKind = iota
	kindReply
	kindEvent
)

// message is the single envelope type gob-encoded onto the wire in both
// directions, so one gob.Encoder/Decoder pair per connection is enough
// to carry calls, replies and pushed events without any type-registration
// gymnastics.
type message struct {
	Kind  msgKind
	Req   *Request
	Resp  *Response
	Event *engine.Event
}

// callTimeout bounds how long a client waits for a reply to a Request
// before giving up.
const callTimeout = 30 * time.Second
