package engine

import "time"

// Seeding limits: stop a torrent once it has given enough back.
//
// These pause rather than hold. The VPN and disk guards are conditions of
// the machine, so they leave the user's pause flag alone and release by
// themselves; a seeding limit is the opposite - a decision that this
// torrent is finished, which has to survive a restart rather than be
// undone by one. So it goes through the same path a user pausing would.

// seedLimitsFor resolves a torrent's effective limits against the
// engine-wide defaults. Callers must hold e.mu.
//
// Zero on the torrent means "no opinion, use the default"; negative means
// "no limit for this one". That distinction is what lets a single torrent
// opt out of a default that would otherwise stop it, which a plain zero
// could not express.
func (e *Engine) seedLimitsFor(tr *tracked) (ratio float64, seedTime time.Duration) {
	ratio, seedTime = e.cfg.SeedRatio, e.cfg.SeedTime
	if tr.seedRatio != 0 {
		ratio = tr.seedRatio
	}
	if tr.seedTime != 0 {
		seedTime = tr.seedTime
	}
	if ratio < 0 {
		ratio = 0
	}
	if seedTime < 0 {
		seedTime = 0
	}
	return ratio, seedTime
}

// seedLimitReached reports whether tr has met a seeding limit, and which.
//
// Only ever true for a torrent that has finished: a ratio while the
// denominator is still growing is not a ratio anyone meant, and "seeding
// time" has not started. Callers must hold e.mu, and pass the totals from
// the snapshot so the two cannot disagree.
func (e *Engine) seedLimitReached(tr *tracked, snap TorrentSnapshot, now time.Time) (string, bool) {
	if tr.paused || snap.TotalLength <= 0 || snap.Completed < snap.TotalLength {
		return "", false
	}
	ratio, seedTime := e.seedLimitsFor(tr)

	// Checked before the clock, so the message names the limit that would
	// have stopped it first on a torrent that has met both.
	if ratio > 0 && snap.Ratio >= ratio {
		return "ratio", true
	}
	if seedTime > 0 && !tr.completedAt.IsZero() && now.Sub(tr.completedAt) >= seedTime {
		return "time", true
	}
	return "", false
}

// markCompletedLocked starts the seeding clock the first time a torrent
// is seen finished. Callers must hold e.mu.
//
// Recorded rather than derived from AddedAt: a torrent added from data
// already on disk completes immediately, and one that took a week to
// download should not be treated as having seeded for that week.
// Reports whether it started one, because that is a transition worth
// writing to disk: the tick does not persist, so without saying so the
// clock would only reach the session file at the next mutating operation
// - and a restart before that would start it again from zero.
func markCompletedLocked(tr *tracked, snap TorrentSnapshot, now time.Time) bool {
	if tr.completedAt.IsZero() && snap.TotalLength > 0 && snap.Completed >= snap.TotalLength {
		tr.completedAt = now
		return true
	}
	return false
}

// SetSeedLimit sets one torrent's own seeding limits, overriding the
// configured defaults.
//
// Zero means "use the default" and a negative value means "no limit for
// this one" - the same convention SetTorrentRateLimit uses for its
// directions, and the only way a single torrent can opt out of a default
// that would otherwise stop it.
func (e *Engine) SetSeedLimit(id TorrentID, ratio float64, seedTime time.Duration) error {
	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	tr.seedRatio = ratio
	tr.seedTime = seedTime
	e.mu.Unlock()

	e.persist()
	e.snapshotAndBroadcastNow()
	return nil
}
