package engine

import (
	"errors"
	"path/filepath"
	"testing"
)

// A real client parses and validates the magnet, so the infohash has to
// be a genuine 40-character hex string rather than any old text.
const testInfoHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testMagnet = "magnet:?xt=urn:btih:" + testInfoHash + "&dn=Example+Torrent"

func TestAddMagnet(t *testing.T) {
	e := newTestEngine(t)

	id, err := e.AddMagnet(testMagnet, AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}
	if string(id) != testInfoHash {
		t.Errorf("id = %q, want the magnet's infohash %q", id, testInfoHash)
	}

	list := e.ListTorrents()
	if len(list) != 1 {
		t.Fatalf("got %d torrents, want 1", len(list))
	}
	// The display name from the magnet's "dn" parameter, url-decoded.
	if list[0].Name != "Example Torrent" {
		t.Errorf("Name = %q, want %q", list[0].Name, "Example Torrent")
	}
}

func TestAddMagnetRejectsNonMagnet(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.AddMagnet("https://example.com/x.torrent", AddOpts{}); err == nil {
		t.Error("a non-magnet URI should be rejected")
	}
}

// Anything that is not a magnet is treated as a path to a .torrent file,
// so a typo must be rejected rather than added as a torrent that can
// never download.
func TestAddTorrentFileRequiresTheFileToExist(t *testing.T) {
	e := newTestEngine(t)

	if _, err := e.AddTorrentFile(filepath.Join(t.TempDir(), "absent.torrent"), AddOpts{}); err == nil {
		t.Error("adding a missing .torrent file should fail")
	}

}

// Adding the same torrent twice is not an error: a real client keys
// torrents by infohash, so the second add finds the first already there.
func TestAddIsIdempotent(t *testing.T) {
	e := newTestEngine(t)

	first, _ := e.AddMagnet(testMagnet, AddOpts{})
	second, err := e.AddMagnet(testMagnet, AddOpts{})
	if err != nil {
		t.Fatalf("second AddMagnet: %v", err)
	}
	if first != second {
		t.Errorf("ids differ: %q then %q", first, second)
	}
	if n := len(e.ListTorrents()); n != 1 {
		t.Errorf("got %d torrents, want 1", n)
	}
}

func TestAddPausedStartsPaused(t *testing.T) {
	e := newTestEngine(t)

	id, _ := e.AddMagnet(testMagnet, AddOpts{Paused: true})
	d, err := e.TorrentDetail(id)
	if err != nil {
		t.Fatalf("TorrentDetail: %v", err)
	}
	if !d.Snapshot.Paused {
		t.Error("AddOpts.Paused was ignored")
	}
}

func TestSetPaused(t *testing.T) {
	e := newTestEngine(t)
	id, _ := e.AddMagnet(testMagnet, AddOpts{})

	if err := e.SetPaused(id, true); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}
	d, _ := e.TorrentDetail(id)
	if !d.Snapshot.Paused {
		t.Error("torrent should be paused")
	}

	if err := e.SetPaused(id, false); err != nil {
		t.Fatalf("SetPaused(false): %v", err)
	}
	d, _ = e.TorrentDetail(id)
	if d.Snapshot.Paused {
		t.Error("torrent should be running again")
	}
}

func TestRemoveTorrent(t *testing.T) {
	e := newTestEngine(t)
	id, _ := e.AddMagnet(testMagnet, AddOpts{})

	if err := e.RemoveTorrent(id, false); err != nil {
		t.Fatalf("RemoveTorrent: %v", err)
	}
	if n := len(e.ListTorrents()); n != 0 {
		t.Errorf("got %d torrents after remove, want 0", n)
	}
}

// Every lookup of an id nobody answers to reports the same error, so
// callers have one thing to check for.
func TestUnknownIDReportsNotFound(t *testing.T) {
	e := newTestEngine(t)

	if err := e.RemoveTorrent("nope", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("RemoveTorrent: %v, want ErrNotFound", err)
	}
	if err := e.SetPaused("nope", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetPaused: %v, want ErrNotFound", err)
	}
	if _, err := e.TorrentDetail("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("TorrentDetail: %v, want ErrNotFound", err)
	}
}

// A magnet with no peers never gets metadata, and the library's whole
// -torrent verify calls NumPieces without checking for it - a nil
// dereference that takes the daemon down with every other torrent. It has
// to be refused here instead.
func TestForceRecheckWithoutMetadataIsRefused(t *testing.T) {
	e := newTestEngine(t)
	id, err := e.AddMagnet(testMagnet, AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}

	// A panic here would kill the test binary, which is the point: this
	// must return an error, not crash.
	if err := e.ForceRecheck(id); err == nil {
		t.Fatal("rechecking a torrent with no metadata should fail")
	}

	// And the torrent must not be left stuck reporting a check that is
	// not running.
	d, _ := e.TorrentDetail(id)
	if d.Snapshot.CheckProgress != 0 {
		t.Errorf("CheckProgress = %v after a refused recheck", d.Snapshot.CheckProgress)
	}
}

func TestForceRecheckUnknownID(t *testing.T) {
	if err := newTestEngine(t).ForceRecheck("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ForceRecheck(unknown) = %v, want ErrNotFound", err)
	}
}

// A snapshot has to say a check is running before the first piece
// finishes. Deriving that from "progress above zero" meant the status
// fell back to a bare "checking" at exactly the moment the check began -
// on a large torrent, the moment a user is most likely to look.
func TestASnapshotReportsACheckBeforeItsFirstPiece(t *testing.T) {
	e := newTestEngine(t)
	id, err := e.AddMagnet(testMagnet, AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}

	// Set up by hand rather than by starting a real check: this is about
	// the instant before any piece has been verified, which a running
	// check passes through too quickly to catch.
	e.mu.Lock()
	tr := e.torrents[id]
	tr.checking = true
	tr.checkDone = 0
	tr.checkTotal = 24000
	snap := e.snapshotLocked(id, tr)
	e.mu.Unlock()

	if !snap.Checking {
		t.Error("a running check is not reported in the snapshot")
	}
	if got := snap.StatusText(); got != "checking 0%" {
		t.Errorf("StatusText = %q, want %q", got, "checking 0%")
	}
}

// Waiting for a magnet's metadata is reported as checking too, but
// nothing is being verified - a percentage there would be a lie.
func TestWaitingForMetadataIsNotAChecking(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.AddMagnet(testMagnet, AddOpts{}); err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}

	snap := e.ListTorrents()[0]
	if snap.State != StateChecking {
		t.Fatalf("state = %v, want checking", snap.State)
	}
	if snap.Checking {
		t.Error("a torrent waiting for metadata is reported as being hash-checked")
	}
	if got := snap.StatusText(); got != "checking" {
		t.Errorf("StatusText = %q, want %q", got, "checking")
	}
}

// A file switched off while a torrent is paused stays off when it is
// resumed. Resuming has to mark files wanted - a torrent added paused
// never had that done - but "nobody has touched this file" and "the user
// switched this file off" are the same PiecePriorityNone to the library,
// so reading the answer back off it re-wants everything that was
// deselected.
func TestResumeKeepsAFileThatWasSwitchedOff(t *testing.T) {
	cfg := testConfig(t)
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	torrentPath := writeTestTorrent(t, cfg.DataDir, 4*1024*1024)

	id, err := e.AddTorrentFile(torrentPath, AddOpts{Paused: true})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}
	if err := e.SetFilePriority(id, 0, PriorityNone); err != nil {
		t.Fatalf("SetFilePriority: %v", err)
	}

	if err := e.SetPaused(id, false); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	detail, err := e.TorrentDetail(id)
	if err != nil {
		t.Fatalf("TorrentDetail: %v", err)
	}
	if got := detail.Files[0].Priority; got != PriorityNone {
		t.Errorf("file priority after resuming = %v, want %v", got, PriorityNone)
	}
}

// The other half of the same rule: a torrent added paused with nothing
// chosen must still start downloading when it is resumed.
func TestResumeMarksUntouchedFilesWanted(t *testing.T) {
	cfg := testConfig(t)
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	torrentPath := writeTestTorrent(t, cfg.DataDir, 4*1024*1024)

	id, err := e.AddTorrentFile(torrentPath, AddOpts{Paused: true})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}
	if err := e.SetPaused(id, false); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	detail, err := e.TorrentDetail(id)
	if err != nil {
		t.Fatalf("TorrentDetail: %v", err)
	}
	if got := detail.Files[0].Priority; got != PriorityNormal {
		t.Errorf("file priority after resuming = %v, want %v", got, PriorityNormal)
	}
}
