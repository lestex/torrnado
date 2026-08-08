package engine

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// completedTorrent adds a torrent whose data is already on disk and
// verifies it, so the engine and the piece-completion database both
// believe every piece is there - which is the state purging has to
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
		t.Errorf("progress = %.2f (%d bytes) after a purge, want 0 - the completion database was not cleared",
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
// the finished name frees nothing at all on a half-downloaded torrent -
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
// nothing has been written either - a magnet must not make this panic,
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

// writeMultiFileTorrent builds a torrent of a directory holding two
// files, which is what a real distribution torrent looks like: an image
// and a checksum beside it, inside a directory of the torrent's name.
func writeMultiFileTorrent(t *testing.T, parent string) (torrentPath, payloadDir string) {
	t.Helper()

	payloadDir = filepath.Join(parent, "release")
	if err := os.MkdirAll(payloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 64*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payloadDir, "image.iso"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payloadDir, "CHECKSUM"), []byte("deadbeef\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := metainfo.Info{PieceLength: 16 * 1024}
	if err := info.BuildFromFilePath(payloadDir); err != nil {
		t.Fatalf("build info: %v", err)
	}
	info.MetaVersion = 0
	info.FileTree = metainfo.FileTree{}

	var mi metainfo.MetaInfo
	var err error
	if mi.InfoBytes, err = bencode.Marshal(info); err != nil {
		t.Fatalf("marshal info: %v", err)
	}

	torrentPath = filepath.Join(parent, "release.torrent")
	f, err := os.Create(torrentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := mi.Write(f); err != nil {
		t.Fatalf("write torrent file: %v", err)
	}
	return torrentPath, payloadDir
}

// A real distribution torrent is a directory of files, not one file, and
// a finished one has had every file marked read-only by the library. Both
// have to go - and so does the directory they were in, or purging a
// library of them leaves a tree of empty folders behind.
func TestPurgeDataDeletesAMultiFileTorrentsDirectory(t *testing.T) {
	cfg := testConfig(t)
	cfg.StateDir = t.TempDir()
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	torrentPath, payloadDir := writeMultiFileTorrent(t, cfg.DataDir)
	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	// Verified, so the completion database agrees the data is all there,
	// and read-only, the way the library leaves a finished file.
	if err := e.ForceRecheck(id); err != nil {
		t.Fatalf("ForceRecheck: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if snap := findSnapshot(t, e, id); !snap.Checking && snap.Progress == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, name := range []string{"image.iso", "CHECKSUM"} {
		if err := os.Chmod(filepath.Join(payloadDir, name), 0o444); err != nil {
			t.Fatal(err)
		}
	}

	if err := e.PurgeData(id); err != nil {
		t.Fatalf("PurgeData: %v", err)
	}

	if _, err := os.Stat(payloadDir); !os.IsNotExist(err) {
		left, _ := os.ReadDir(payloadDir)
		t.Errorf("the torrent's directory is still there with %d entries: %v", len(left), err)
	}
	if snap := findSnapshot(t, e, id); snap.Progress != 0 {
		t.Errorf("progress = %.2f after a purge, want 0", snap.Progress)
	}
}

// Which directory holds a torrent's files depends on its shape, and a
// client should not have to work that out from a name that may not match
// what is on disk. The file list says it: a multi-file torrent's paths
// carry their directory, a single-file torrent's do not.
func TestDataDirDependsOnTheTorrentsShape(t *testing.T) {
	cfg := testConfig(t)
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	multiPath, payloadDir := writeMultiFileTorrent(t, cfg.DataDir)
	multi, err := e.AddTorrentFile(multiPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}
	if got := findSnapshot(t, e, multi).DataDir; got != payloadDir {
		t.Errorf("multi-file DataDir = %q, want the torrent's own folder %q", got, payloadDir)
	}

	single, err := e.AddTorrentFile(writeTestTorrent(t, cfg.DataDir, 64*1024), AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}
	if got := findSnapshot(t, e, single).DataDir; got != cfg.DataDir {
		t.Errorf("single-file DataDir = %q, want the save path %q", got, cfg.DataDir)
	}

	// A magnet has no file list yet, and the save path is both a real
	// directory and where the data is going to land.
	magnet, err := e.AddMagnet(testMagnet, AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}
	if got := findSnapshot(t, e, magnet).DataDir; got != cfg.DataDir {
		t.Errorf("DataDir without metadata = %q, want the save path %q", got, cfg.DataDir)
	}
}
