package engine

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// A limit applies only to a torrent that has finished. A ratio while the
// denominator is still growing is not a ratio anyone meant, and seeding
// time has not started.
func TestSeedLimitOnlyAppliesToACompletedTorrent(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.SeedRatio = 2.0
	tr := &tracked{}
	now := time.Now()

	half := TorrentSnapshot{TotalLength: 100, Completed: 50, Ratio: 9.0}
	if _, reached := e.seedLimitReached(tr, half, now); reached {
		t.Error("stopped a torrent that had not finished downloading")
	}

	whole := TorrentSnapshot{TotalLength: 100, Completed: 100, Ratio: 9.0}
	why, reached := e.seedLimitReached(tr, whole, now)
	if !reached || why != "ratio" {
		t.Errorf("reached=%v why=%q, want true/ratio", reached, why)
	}
}

// The clock runs from completion, not from when the torrent was added: a
// torrent that took a week to download has not been seeding for a week.
func TestSeedTimeCountsFromCompletion(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.SeedTime = time.Hour

	now := time.Now()
	tr := &tracked{addedAt: now.Add(-30 * 24 * time.Hour)}
	done := TorrentSnapshot{TotalLength: 100, Completed: 100}

	// Not finished until now, however long ago it was added.
	markCompletedLocked(tr, done, now)
	if _, reached := e.seedLimitReached(tr, done, now); reached {
		t.Error("a torrent added a month ago was treated as having seeded for a month")
	}

	// And it does fire once it has actually seeded that long.
	if _, reached := e.seedLimitReached(tr, done, now.Add(2*time.Hour)); !reached {
		t.Error("did not stop after the seeding time elapsed")
	}
}

// The completion moment is recorded once. A torrent that finished days
// ago must not have its clock restarted by a later tick.
func TestCompletionMomentIsRecordedOnce(t *testing.T) {
	tr := &tracked{}
	done := TorrentSnapshot{TotalLength: 100, Completed: 100}

	first := time.Now().Add(-48 * time.Hour)
	markCompletedLocked(tr, done, first)
	markCompletedLocked(tr, done, time.Now())

	if !tr.completedAt.Equal(first) {
		t.Errorf("completedAt = %v, want it left at %v", tr.completedAt, first)
	}
}

// Zero on a torrent means "use the default"; negative means "no limit for
// this one". Without that distinction a torrent could not opt out of a
// default that would otherwise stop it.
func TestPerTorrentLimitsOverrideTheDefaults(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.SeedRatio = 2.0
	e.cfg.SeedTime = time.Hour

	for _, c := range []struct {
		name      string
		tr        *tracked
		wantRatio float64
		wantTime  time.Duration
	}{
		{"unset follows the default", &tracked{}, 2.0, time.Hour},
		{"own ratio wins", &tracked{seedRatio: 5.0}, 5.0, time.Hour},
		{"own time wins", &tracked{seedTime: 3 * time.Hour}, 2.0, 3 * time.Hour},
		{"negative opts out", &tracked{seedRatio: -1, seedTime: -1}, 0, 0},
	} {
		ratio, seedTime := e.seedLimitsFor(c.tr)
		if ratio != c.wantRatio || seedTime != c.wantTime {
			t.Errorf("%s: ratio=%v time=%v, want ratio=%v time=%v",
				c.name, ratio, seedTime, c.wantRatio, c.wantTime)
		}
	}
}

// A torrent that has opted out is not stopped by a default that would
// otherwise have caught it.
func TestOptedOutTorrentIsNotStopped(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.SeedRatio = 2.0

	tr := &tracked{seedRatio: -1}
	done := TorrentSnapshot{TotalLength: 100, Completed: 100, Ratio: 99}
	if _, reached := e.seedLimitReached(tr, done, time.Now()); reached {
		t.Error("stopped a torrent that had opted out of the ratio limit")
	}
}

// Already paused is already stopped; re-stopping it every tick would
// rewrite the session file once a second forever.
func TestAPausedTorrentIsNotStoppedAgain(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.SeedRatio = 2.0

	tr := &tracked{paused: true}
	done := TorrentSnapshot{TotalLength: 100, Completed: 100, Ratio: 99}
	if _, reached := e.seedLimitReached(tr, done, time.Now()); reached {
		t.Error("a paused torrent was selected for stopping again")
	}
}

// Off by default, like every other limit here.
func TestNoSeedLimitByDefault(t *testing.T) {
	e := newTestEngine(t)
	tr := &tracked{}
	done := TorrentSnapshot{TotalLength: 100, Completed: 100, Ratio: 500}
	if _, reached := e.seedLimitReached(tr, done, time.Now()); reached {
		t.Error("stopped a torrent with no limit configured")
	}
}

// The whole path: a completed torrent over its ratio is paused by the
// tick, persisted, and stays stopped - not held, and not stopped twice.
func TestTickStopsATorrentAtItsRatio(t *testing.T) {
	cfg := testConfig(t)
	cfg.StateDir = t.TempDir()
	cfg.SeedRatio = 2.0
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	// Built inside the data dir, so it is complete the moment it is added.
	torrentPath := writeTestTorrent(t, cfg.DataDir, 256*1024)
	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	// Stand in for having uploaded three times what was downloaded.
	e.mu.Lock()
	e.torrents[id].baseDownloaded = 1 << 20
	e.torrents[id].baseUploaded = 3 << 20
	e.mu.Unlock()

	e.tick()

	tr, err := e.lookup(id)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	e.mu.Lock()
	paused, completedAt := tr.paused, tr.completedAt
	e.mu.Unlock()

	if !paused {
		t.Fatal("a torrent past its seed ratio was not stopped")
	}
	if completedAt.IsZero() {
		t.Error("the seeding clock was never started")
	}

	// Paused, not held: the distinction is what makes it survive a
	// restart rather than being undone by one.
	var snap TorrentSnapshot
	for _, s := range e.ListTorrents() {
		if s.ID == id {
			snap = s
		}
	}
	if !snap.Paused {
		t.Error("the snapshot does not report it as paused")
	}

	// And a second tick must not act again - re-pausing every second
	// would rewrite the session file forever.
	e.mu.Lock()
	again, _ := e.seedLimitsReachedLocked([]TorrentSnapshot{snap}, time.Now())
	e.mu.Unlock()
	if len(again) != 0 {
		t.Errorf("an already-stopped torrent was selected again: %+v", again)
	}
}

// The seeding clock has to reach disk when it starts, not at whatever
// mutating operation happens to come next. The tick does not persist, so
// without that a restart would start the clock again from zero - and a
// "seed for 48h" limit would never come due on a box that reboots, which
// is the one it is for.
func TestTheSeedingClockIsPersistedWhenItStarts(t *testing.T) {
	cfg := testConfig(t)
	cfg.StateDir = t.TempDir()
	cfg.SeedTime = 48 * time.Hour
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	torrentPath := writeTestTorrent(t, cfg.DataDir, 256*1024)
	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	// One tick, and nothing else - in particular no operation that would
	// have persisted for its own reasons.
	e.tick()

	data, err := os.ReadFile(e.sessionPath())
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	var sf sessionFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("parse session: %v", err)
	}
	var rec torrentRecord
	for _, r := range sf.Torrents {
		if r.InfoHash == string(id) {
			rec = r
		}
	}
	if rec.CompletedAt.IsZero() {
		t.Error("the seeding clock was not written to disk when it started")
	}
}
