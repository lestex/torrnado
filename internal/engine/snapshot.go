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

// applyDataFlow turns the two switches that decide whether a torrent may
// move piece data, from every reason it might not be allowed to.
//
// One funnel, because the reasons are independent and each was setting
// the switches on its own: the tick's rate limiting used to call
// AllowDataDownload unconditionally whenever a torrent was under its cap,
// which would have turned any other reason back on a second after it was
// applied. Anything that wants to stop or start data goes through here.
//
// The rate-limit half is an approximation. The library's limiters are
// client-wide, with no hook to throttle a single torrent's network I/O,
// so a torrent over its cap is forbidden from moving data until it falls
// back under. That averages out near the limit over a second or so, but
// it is bursty - a real token bucket it is not.
//
// Callers must hold e.mu.
func (tr *tracked) applyDataFlow(blocked bool) {
	down, up := tr.dataFlow(blocked)

	if down {
		tr.t.AllowDataDownload()
	} else {
		tr.t.DisallowDataDownload()
	}

	if up {
		tr.t.AllowDataUpload()
	} else {
		tr.t.DisallowDataUpload()
	}
}

// dataFlow is applyDataFlow's decision, without the library calls - which
// is the whole of the logic and the only part that can be asserted on:
// anacrolix/torrent has no accessor for either switch, so a test can set
// one and never read it back.
//
// Callers must hold e.mu.
func (tr *tracked) dataFlow(blocked bool) (down, up bool) {
	// blocked is the VPN guard and holdData a move in progress; both are
	// conditions of the moment. paused is what the user asked for. None of
	// them is allowed to overwrite another, which is why they are read
	// together here rather than each turning the switches itself.
	if tr.paused || blocked || tr.holdData {
		return false, false
	}
	down = tr.downLimit <= 0 || tr.lastDownBPS <= float64(tr.downLimit)
	up = tr.upLimit <= 0 || tr.lastUpBPS <= float64(tr.upLimit)
	return down, up
}

// snapshotLocked builds the public view of one torrent. Callers must hold
// e.mu.
func (e *Engine) snapshotLocked(id TorrentID, tr *tracked) TorrentSnapshot {
	t := tr.t

	// Name, length and progress all read through the torrent's metadata,
	// which for a magnet link is not there at the moment it is added -
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
		// Above blocked: a paused torrent is paused for its own reason and
		// stays that way when the VPN comes back, so saying "blocked"
		// would promise it is about to start.
		state = StatePaused
	case e.blocked:
		state = StateBlocked
	case total > 0 && completed >= total:
		state = StateSeeding
	}

	stats := t.Stats()
	downloaded := stats.BytesReadUsefulData.Int64()
	uploaded := stats.BytesWrittenData.Int64()

	// A torrent that has uploaded without downloading anything has an
	// infinite ratio - which is a real state (a torrent you seeded from
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

	var checkProgress float64
	if tr.checking && tr.checkTotal > 0 {
		checkProgress = float64(tr.checkDone) / float64(tr.checkTotal)
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
		DataDir:       dataDir(tr.savePath, t),
		AddedAt:       tr.addedAt,
		Error:         tr.lastErr,
		Checking:      tr.checking,
		CheckProgress: checkProgress,
		DownloadLimit: tr.downLimit,
		UploadLimit:   tr.upLimit,
	}
}
