package engine

import (
	"errors"
	"testing"
)

const testMagnet = "magnet:?xt=urn:btih:abc&dn=Example+Torrent"

func TestAddMagnet(t *testing.T) {
	e := newTestEngine(t)

	id, err := e.AddMagnet(testMagnet, AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}
	if id == "" {
		t.Fatal("AddMagnet returned an empty id")
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

func TestTickAdvancesTrackedTorrents(t *testing.T) {
	e := newTestEngine(t)
	id, _ := e.AddMagnet(testMagnet, AddOpts{})

	e.tick()

	d, _ := e.TorrentDetail(id)
	if d.Snapshot.Completed == 0 {
		t.Error("a tick should have advanced the torrent")
	}
	if d.Snapshot.State != StateDownloading {
		t.Errorf("State = %v, want downloading", d.Snapshot.State)
	}
}
