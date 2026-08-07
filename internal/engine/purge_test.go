package engine

import (
	"path/filepath"
	"testing"
)

// An unfinished file is written as "<name>.part" and only renamed when
// every one of its pieces has landed, so a deletion that only looks for
// the finished name frees nothing at all on a half-downloaded torrent --
// which is the one most worth purging.
func TestDataPathsCoversPartFiles(t *testing.T) {
	cfg := testConfig(t)
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	torrentPath := writeTestTorrent(t, cfg.DataDir, 64*1024)
	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	tr, err := e.lookup(id)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	paths := dataPaths(tr.savePath, tr.t)

	want := map[string]bool{
		filepath.Join(cfg.DataDir, "payload.bin"):      false,
		filepath.Join(cfg.DataDir, "payload.bin.part"): false,
	}
	for _, p := range paths {
		if _, ok := want[p]; !ok {
			t.Errorf("unexpected path %q", p)
			continue
		}
		want[p] = true
	}
	for p, found := range want {
		if !found {
			t.Errorf("dataPaths did not list %q", p)
		}
	}
}

// Before metadata arrives there is no file list to build paths from, and
// nothing has been written either -- a magnet must not make this panic,
// the way every other call that reads Files() would.
func TestDataPathsIsEmptyWithoutMetadata(t *testing.T) {
	e := newTestEngine(t)

	id, err := e.AddMagnet(testMagnet, AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}
	tr, err := e.lookup(id)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}

	if got := dataPaths(tr.savePath, tr.t); len(got) != 0 {
		t.Errorf("dataPaths without metadata = %v, want none", got)
	}
}
