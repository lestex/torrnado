package engine

import (
	"math"
	"time"
)

// updateRates turns the client's cumulative byte counters into speeds.
//
// The library reports totals, never a rate, so a speed is the change
// since the last reading divided by the time between them. Callers must
// hold e.mu.
func (tr *tracked) updateRates(elapsed float64) {
	if elapsed <= 0 {
		return // no time has passed; the previous rates still stand
	}
	stats := tr.t.Stats()
	downloaded := stats.BytesReadUsefulData.Int64()
	uploaded := stats.BytesWrittenData.Int64()

	// Clamped at zero: the counters reset when a torrent is re-added or
	// rechecked, and a negative "speed" helps nobody.
	tr.lastDownBPS = math.Max(0, float64(downloaded-tr.lastDownloaded)/elapsed)
	tr.lastUpBPS = math.Max(0, float64(uploaded-tr.lastUploaded)/elapsed)
	tr.lastDownloaded = downloaded
	tr.lastUploaded = uploaded
}

// enforceRateLimit applies a torrent's own speed cap, roughly.
//
// The library's rate limiters are client-wide; there is no hook to
// throttle a single torrent's network I/O. So this approximates one: each
// tick, a torrent over its cap is forbidden from moving data, and allowed
// again once it falls back under. That averages out near the limit over
// a second or so, but it is bursty -- a real token bucket it is not.
// Callers must hold e.mu.
func (tr *tracked) enforceRateLimit() {
	if tr.paused {
		return // pause already forbids both directions
	}
	if tr.downLimit > 0 && tr.lastDownBPS > float64(tr.downLimit) {
		tr.t.DisallowDataDownload()
	} else {
		tr.t.AllowDataDownload()
	}
	if tr.upLimit > 0 && tr.lastUpBPS > float64(tr.upLimit) {
		tr.t.DisallowDataUpload()
	} else {
		tr.t.AllowDataUpload()
	}
}

// snapshotLocked builds the public view of one torrent. Callers must hold
// e.mu.
func (e *Engine) snapshotLocked(id TorrentID, tr *tracked) TorrentSnapshot {
	t := tr.t

	// Name, length and progress all read through the torrent's metadata,
	// which for a magnet link is not there at the moment it is added --
	// it has to be fetched from peers first. Until then only the name is
	// available, and it falls back to the infohash.
	var total, completed int64
	if t.Info() != nil {
		total = t.Length()
		completed = t.BytesCompleted()
	}

	var progress float64
	if total > 0 {
		progress = float64(completed) / float64(total)
	}

	state := StateDownloading
	switch {
	case tr.lastErr != "":
		state = StateError
	case tr.checking:
		state = StateChecking
	case t.Info() == nil:
		// Waiting for metadata. Reported as checking rather than
		// downloading because no file data is moving yet.
		state = StateChecking
	case tr.paused:
		state = StatePaused
	case total > 0 && completed >= total:
		state = StateSeeding
	}

	stats := t.Stats()
	downloaded := stats.BytesReadUsefulData.Int64()
	uploaded := stats.BytesWrittenData.Int64()

	// A torrent that has uploaded without downloading anything has an
	// infinite ratio -- which is a real state (a torrent you seeded from
	// files already on disk), not an error.
	var ratio float64
	switch {
	case downloaded > 0:
		ratio = float64(uploaded) / float64(downloaded)
	case uploaded > 0:
		ratio = math.Inf(1)
	}

	// Left at zero when the speed is negligible, rather than reporting a
	// number of hours that would be meaningless.
	var eta time.Duration
	if missing := total - completed; missing > 0 && tr.lastDownBPS > 0.5 {
		eta = time.Duration(float64(missing)/tr.lastDownBPS) * time.Second
	}

	return TorrentSnapshot{
		ID:            id,
		Name:          t.Name(),
		InfoHash:      t.InfoHash().HexString(),
		TotalLength:   total,
		Completed:     completed,
		Progress:      progress,
		DownloadBPS:   tr.lastDownBPS,
		UploadBPS:     tr.lastUpBPS,
		Downloaded:    downloaded,
		Uploaded:      uploaded,
		Ratio:         ratio,
		NumPeers:      stats.ActivePeers,
		NumSeeds:      stats.ConnectedSeeders,
		ETA:           eta,
		State:         state,
		Paused:        tr.paused,
		SavePath:      tr.savePath,
		AddedAt:       tr.addedAt,
		Error:         tr.lastErr,
		DownloadLimit: tr.downLimit,
		UploadLimit:   tr.upLimit,
	}
}
