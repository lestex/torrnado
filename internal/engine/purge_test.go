package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// completedTorrent adds a torrent whose data is already on disk and
// verifies it, so the engine and the piece-completion database both
// believe every piece is there -- which is the state purging has to
// undo.
func completedTorrent(t *testing.T, e *Engine, dir string) (TorrentID, string) {
	t.Helper()

	torrentPath := writeTestTorrent(t, dir, 64*1024)
	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}
	if err := e.ForceRecheck(id); err != nil {
		t.Fatalf("ForceRecheck: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if snap := findSnapshot(t, e, id); !snap.Checking && snap.Progress == 1 {
			return id, filepath.Join(dir, "payload.bin")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the torrent never reached 100%%: %+v", findSnapshot(t, e, id))
	return "", ""
}

func TestPurgeDataDeletesTheFilesAndKeepsTheTorrent(t *testing.T) {
	cfg := testConfig(t)
	cfg.StateDir = t.TempDir()
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	id, dataPath := completedTorrent(t, e, cfg.DataDir)

	if err := e.PurgeData(id); err != nil {
		t.Fatalf("PurgeData: %v", err)
	}

	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Errorf("the data is still on disk: %v", err)
	}

	snap := findSnapshot(t, e, id) // still listed, which is the point
	if !snap.Paused {
		t.Error("the torrent was left running, so it would download it all again")
	}
	// The assertion the whole operation exists for. Deleting the files
	// without clearing the completion database leaves a torrent that
	// still reports 100% and cannot serve a byte of it.
	if snap.Progress != 0 || snap.Completed != 0 {
		t.Errorf("progress = %.2f (%d bytes) after a purge, want 0 -- the completion database was not cleared",
			snap.Progress, snap.Completed)
	}
	if snap.TotalLength == 0 {
		t.Error("the torrent lost its metadata")
	}
}

// The torrent has to come back the same torrent: same save path, same
// limits, same place in the session file, or "keep the entry" is not what
// happened.
func TestPurgeDataKeepsTheTorrentsSettings(t *testing.T) {
	cfg := testConfig(t)
	cfg.StateDir = t.TempDir()
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	saveDir := t.TempDir()
	torrentPath := writeTestTorrent(t, saveDir, 64*1024)
	id, err := e.AddTorrentFile(torrentPath, AddOpts{SavePath: saveDir})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}
	if err := e.SetTorrentRateLimit(id, 1000, 2000); err != nil {
		t.Fatalf("SetTorrentRateLimit: %v", err)
	}

	if err := e.PurgeData(id); err != nil {
		t.Fatalf("PurgeData: %v", err)
	}

	snap := findSnapshot(t, e, id)
	if snap.SavePath != saveDir {
		t.Errorf("save path = %q, want %q", snap.SavePath, saveDir)
	}
	if snap.UploadLimit != 1000 || snap.DownloadLimit != 2000 {
		t.Errorf("rate limits = %d/%d, want 1000/2000", snap.UploadLimit, snap.DownloadLimit)
	}

	// And the metainfo is still on disk, so a restart brings it back
	// rather than needing a peer to describe it again.
	if _, err := os.Stat(e.metainfoPath(id)); err != nil {
		t.Errorf("the saved metainfo went with the data: %v", err)
	}
}

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
	// And purging one says so rather than doing half the work.
	if err := e.PurgeData(id); err == nil {
		t.Error("purging a torrent with no metadata should be refused")
	}
}
