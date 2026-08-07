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
