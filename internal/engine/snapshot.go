package engine

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
	case t.Info() == nil:
		// Waiting for metadata. Reported as checking rather than
		// downloading because no file data is moving yet.
		state = StateChecking
	case tr.paused:
		state = StatePaused
	case total > 0 && completed >= total:
		state = StateSeeding
	}

	return TorrentSnapshot{
		ID:          id,
		Name:        t.Name(),
		InfoHash:    t.InfoHash().HexString(),
		TotalLength: total,
		Completed:   completed,
		Progress:    progress,
		State:       state,
		Paused:      tr.paused,
		SavePath:    tr.savePath,
		AddedAt:     tr.addedAt,
	}
}
