package main

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
)

// What `list --json` and `status --json` print is a contract with
// whatever reads it, so it is written out here as its own structs rather
// than by marshalling the engine's. Those cross the wire as gob and change
// whenever the engine needs them to; a script's idea of "download_rate"
// must not move because a field was renamed three packages away.
//
// Conventions, the same everywhere:
//
//   - sizes are bytes and rates are bytes per second, as plain numbers,
//     never "5.8GiB" - a script that wants a unit can divide
//   - a fraction is 0..1, not a percentage
//   - null means "not known", and is used where zero would be a different
//     answer: no ratio yet is not a ratio of nought, and an unknown ETA is
//     not an ETA of now
//   - times are RFC 3339, in UTC
//   - keys are never omitted, so a reader does not have to tell a missing
//     key from a null one

// torrentJSON is one torrent in `list --json`.
type torrentJSON struct {
	ID    string `json:"id"` // the hex info hash
	Name  string `json:"name"`
	State string `json:"state"`
	// Paused is its own field because State cannot stand in for it: a
	// torrent that is paused and also being hash-checked reports
	// "checking", so anything reading State alone would think it running.
	Paused    bool    `json:"paused"`
	Progress  float64 `json:"progress"`  // 0..1
	Size      int64   `json:"size"`      // bytes; 0 until a magnet's metadata arrives
	Completed int64   `json:"completed"` // bytes on disk and wanted
	// Downloaded and Uploaded are totals for the life of the torrent, not
	// just this run.
	Downloaded   int64 `json:"downloaded"`
	Uploaded     int64 `json:"uploaded"`
	DownloadRate int64 `json:"download_rate"`
	UploadRate   int64 `json:"upload_rate"`
	// Ratio is null when nothing has been downloaded to divide by - which
	// is also what a torrent seeding data that was already on disk looks
	// like, and the one place a bare number would have to be infinity.
	Ratio *float64 `json:"ratio"`
	Peers int      `json:"peers"` // connected peers, seeds included
	Seeds int      `json:"seeds"` // how many of those are seeds
	// ETASeconds is null when it is not known: the speed is negligible, or
	// there is nothing left to wait for.
	ETASeconds *int64 `json:"eta_seconds"`
	// CheckProgress is a fraction, and null unless a hash check is running.
	CheckProgress *float64   `json:"check_progress"`
	Label         string     `json:"label"` // "" when unlabelled
	SavePath      string     `json:"save_path"`
	DataDir       string     `json:"data_dir"`
	AddedAt       *time.Time `json:"added_at"`
	DownloadLimit int64      `json:"download_limit"` // bytes/sec, 0 for none
	UploadLimit   int64      `json:"upload_limit"`
	Error         string     `json:"error"` // "" when there is none
}

// stateToken is a torrent's state as a stable word.
//
// The engine's own strings are for people - "low disk" has a space in it -
// so this is where they become something a script can switch on. The set
// is: checking, queued, paused, downloading, seeding, error, blocked,
// low_disk, unknown.
func stateToken(s engine.State) string {
	return strings.ReplaceAll(s.String(), " ", "_")
}

func toTorrentJSON(s engine.TorrentSnapshot) torrentJSON {
	t := torrentJSON{
		ID:            string(s.ID),
		Name:          s.Name,
		State:         stateToken(s.State),
		Paused:        s.Paused,
		Progress:      round(s.Progress, 4),
		Size:          s.TotalLength,
		Completed:     s.Completed,
		Downloaded:    s.Downloaded,
		Uploaded:      s.Uploaded,
		DownloadRate:  int64(math.Round(s.DownloadBPS)),
		UploadRate:    int64(math.Round(s.UploadBPS)),
		Peers:         s.NumPeers,
		Seeds:         s.NumSeeds,
		Label:         s.Label,
		SavePath:      s.SavePath,
		DataDir:       s.DataDir,
		DownloadLimit: s.DownloadLimit,
		UploadLimit:   s.UploadLimit,
		Error:         s.Error,
	}
	// encoding/json refuses to encode an infinity or a NaN at all - the
	// whole listing fails, not just the one field - and the engine reports
	// +Inf for exactly the ordinary case of seeding data that was already
	// on disk. So it is null, here, rather than an error there.
	if r := s.Ratio; !math.IsInf(r, 0) && !math.IsNaN(r) {
		r = round(r, 3)
		t.Ratio = &r
	}
	if s.ETA > 0 {
		secs := int64(s.ETA / time.Second)
		t.ETASeconds = &secs
	}
	if s.Checking {
		p := round(s.CheckProgress, 4)
		t.CheckProgress = &p
	}
	if !s.AddedAt.IsZero() {
		at := s.AddedAt.UTC()
		t.AddedAt = &at
	}
	return t
}

// writeTorrentsJSON prints the listing. Always an array, and "[]" rather
// than "null" when there is nothing, so a reader has something to iterate.
func writeTorrentsJSON(out io.Writer, snaps []engine.TorrentSnapshot) error {
	list := make([]torrentJSON, 0, len(snaps))
	for _, s := range snaps {
		list = append(list, toTorrentJSON(s))
	}
	return encodeJSON(out, list)
}

// statusJSON is `status --json`.
//
// Running and Answering are different questions, and the difference is the
// one worth a field each: a daemon that holds its lock but does not answer
// is most likely busy hash-checking, and mistaking it for a dead one means
// two engines on one data directory. Everything the daemon says about
// itself is null unless it answered.
type statusJSON struct {
	Running   bool   `json:"running"`
	Answering bool   `json:"answering"`
	PID       *int   `json:"pid"` // null when it cannot be named
	Socket    string `json:"socket"`
	Lock      string `json:"lock"`
	// Version is the daemon's own build string, null for a daemon too old
	// to report it - which is ordinary, since it outlives its clients.
	Version *string `json:"version"`
	// ClientVersion is the build of the torrnado that ran this command,
	// and is always present: it is a fact about the caller, not something
	// the daemon has to answer for.
	ClientVersion string `json:"client_version"`
	// VersionSkew is whether the daemon is a different build from that one,
	// the same comparison the TUI and the text report make. true includes a
	// daemon too old to report a version at all. null when there is nothing
	// to compare: no daemon, one that did not answer, or no client version.
	VersionSkew   *bool          `json:"version_skew"`
	StartedAt     *time.Time     `json:"started_at"`
	UptimeSeconds *int64         `json:"uptime_seconds"`
	ListenPort    *int           `json:"listen_port"`
	Transfers     *transfersJSON `json:"transfers"`
	VPN           *vpnJSON       `json:"vpn"`
	Disk          *diskJSON      `json:"disk"`
}

type transfersJSON struct {
	Torrents      int   `json:"torrents"`
	DownloadRate  int64 `json:"download_rate"`
	UploadRate    int64 `json:"upload_rate"`
	DownloadLimit int64 `json:"download_limit"` // bytes/sec, 0 for none
	UploadLimit   int64 `json:"upload_limit"`
}

// vpnJSON is always present when the daemon answered. Required is false
// for everyone who never asked for the guard, and Active is meaningless
// then.
type vpnJSON struct {
	Required  bool   `json:"required"`
	Active    bool   `json:"active"`
	Interface string `json:"interface"`
}

// diskJSON is null when the daemon cannot read the filesystem.
type diskJSON struct {
	FreeBytes    int64 `json:"free_bytes"`
	TotalBytes   int64 `json:"total_bytes"`
	Low          bool  `json:"low"`            // the free-space guard is holding transfers
	MinFreeBytes int64 `json:"min_free_bytes"` // the guard's floor; 0 when it is off
}

// buildStatusJSON assembles the report from what was observed. Pure, with
// the clock and the client's own build passed in, so a test can pin an
// uptime and a version.
func buildStatusJSON(socket string, info ipc.DaemonStatus, global engine.GlobalStats, answering bool, now time.Time, client string) statusJSON {
	st := statusJSON{
		Running:       info.Running,
		Answering:     info.Running && answering,
		Socket:        socket,
		Lock:          info.LockPath,
		ClientVersion: client,
	}
	if info.PID != 0 {
		pid := info.PID
		st.PID = &pid
	}
	if !st.Answering {
		return st
	}

	if global.Version != "" {
		v := global.Version
		st.Version = &v
	}
	// Left null for a client that cannot say what it is: there is nothing
	// to compare, and "not skewed" would be a claim.
	if client != "" {
		_, skewed := engine.VersionSkew(client, global.Version)
		st.VersionSkew = &skewed
	}
	if !global.StartedAt.IsZero() {
		at := global.StartedAt.UTC()
		st.StartedAt = &at
		up := int64(now.Sub(global.StartedAt) / time.Second)
		if up < 0 {
			up = 0
		}
		st.UptimeSeconds = &up
	}
	if global.ListenPort != 0 {
		p := global.ListenPort
		st.ListenPort = &p
	}
	st.Transfers = &transfersJSON{
		Torrents:      global.NumTorrents,
		DownloadRate:  int64(math.Round(global.DownloadBPS)),
		UploadRate:    int64(math.Round(global.UploadBPS)),
		DownloadLimit: global.DownloadLimit,
		UploadLimit:   global.UploadLimit,
	}
	st.VPN = &vpnJSON{Required: global.VPNRequired, Active: global.VPNActive, Interface: global.VPNInterface}
	if global.DiskTotalBytes > 0 {
		st.Disk = &diskJSON{
			FreeBytes:    global.DiskFreeBytes,
			TotalBytes:   global.DiskTotalBytes,
			Low:          global.DiskLow,
			MinFreeBytes: global.MinFreeSpace,
		}
	}
	return st
}

// writeStatusJSON probes the daemon the way the text report does - never
// starting one - and prints what it found.
func writeStatusJSON(out io.Writer, socket string, info ipc.DaemonStatus) error {
	var (
		global    engine.GlobalStats
		answering bool
	)
	if info.Running {
		global, answering = probeDaemon(socket)
	}
	return encodeJSON(out, buildStatusJSON(socket, info, global, answering, time.Now(), currentBuild().String()))
}

// checkListFlags refuses what has no sensible meaning, before anything
// connects to a daemon: --watch redraws a table until interrupted, and
// there is no honest JSON for that.
func checkListFlags(asJSON, watch bool) error {
	if asJSON && watch {
		return errors.New("--json cannot be combined with --watch: a watch redraws a table, and there is no single document to print")
	}
	return nil
}

// checkStatusFlags: --quiet prints nothing at all, so asking for JSON as
// well contradicts itself.
func checkStatusFlags(quiet, asJSON bool) error {
	if quiet && asJSON {
		return errors.New("--quiet prints nothing and --json prints a document; use one or the other")
	}
	return nil
}

func encodeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// round to a number of decimal places, so a ratio of 0.30000000000000004
// does not reach a script.
func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}
