package engine

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// writeTestTorrent creates a file of size bytes and a .torrent for it,
// returning the path of the .torrent.
//
// The file spans several pieces on purpose. A single-piece file is the
// one shape that hides bugs in the piece-layer handling, because the
// library skips those files when it checks for v2 piece roots - which is
// exactly how a move that failed for every real torrent went unnoticed.
func writeTestTorrent(t *testing.T, dir string, size int64) string {
	t.Helper()

	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("random data: %v", err)
	}
	dataPath := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(dataPath, data, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	info := metainfo.Info{PieceLength: 16 * 1024}
	if err := info.BuildFromFilePath(dataPath); err != nil {
		t.Fatalf("build info: %v", err)
	}
	// v1 only. BuildFromFilePath also fills in the v2 file tree, and a
	// hybrid torrent needs per-file piece roots this does not carry.
	info.MetaVersion = 0
	info.FileTree = metainfo.FileTree{}

	var mi metainfo.MetaInfo
	var err error
	if mi.InfoBytes, err = bencode.Marshal(info); err != nil {
		t.Fatalf("marshal info: %v", err)
	}

	torrentPath := filepath.Join(dir, "payload.torrent")
	f, err := os.Create(torrentPath)
	if err != nil {
		t.Fatalf("create torrent file: %v", err)
	}
	defer f.Close()
	if err := mi.Write(f); err != nil {
		t.Fatalf("write torrent file: %v", err)
	}
	return torrentPath
}

// Moving a torrent rebuilds its spec from the running torrent's
// metainfo, and that metainfo carries an allocated-but-empty piece-layers
// map for a v1 torrent. A non-nil map is what the library reads as "this
// is v2", so the re-add demanded a piece root every file spanning more
// than one piece cannot have, and move failed with "no piece root set" -
// on every real torrent.
func TestMoveStorageWorksOnAMultiPieceTorrent(t *testing.T) {
	cfg := testConfig(t)
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	// Built inside the engine's own data directory, so the torrent is
	// added with its data already there - there has to be something to
	// move. 4MiB at a 16KiB piece length is 256 pieces in one file: more
	// than one piece is the point of the test, and enough of them that
	// the verification below lasts long enough to be seen.
	torrentPath := writeTestTorrent(t, cfg.DataDir, 4*1024*1024)

	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	newDir := t.TempDir()
	if err := e.MoveStorage(id, newDir); err != nil {
		t.Fatalf("MoveStorage: %v", err)
	}

	if _, err := os.Stat(filepath.Join(newDir, "payload.bin")); err != nil {
		t.Errorf("the data is not at the new location: %v", err)
	}

	// The move reports its verification as a hash check, which is what
	// puts a percentage in front of the user rather than a bare
	// "checking". Polled rather than sampled once: MoveStorage sets the
	// flag before it returns, but the goroutine clearing it can win.
	var snap TorrentSnapshot
	for deadline := time.Now().Add(5 * time.Second); ; {
		snap = findSnapshot(t, e, id)
		if snap.Checking || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !snap.Checking {
		t.Error("a move does not report the verification it starts")
	}
	if snap.SavePath != newDir {
		t.Errorf("save path = %q, want %q", snap.SavePath, newDir)
	}

	// And it finishes, rather than leaving the torrent checking forever.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !findSnapshot(t, e, id).Checking {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("the verification after a move never finished")
}

// An unfinished file lives at <path>.part, and a move that looked only
// for the finished name found nothing, reported success, and left every
// incomplete file where it was - so the torrent re-downloaded data that
// was already on disk and the .part was orphaned at the old location.
func TestMoveStorageTakesPartFilesWithIt(t *testing.T) {
	cfg := testConfig(t)
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	torrentPath := writeTestTorrent(t, cfg.DataDir, 4*1024*1024)

	// The payload renamed to what the file storage calls a file it has
	// not finished. The torrent was built from the complete file, so this
	// is exactly the on-disk shape of a download in progress.
	complete := filepath.Join(cfg.DataDir, "payload.bin")
	part := complete + partFileSuffix
	if err := os.Rename(complete, part); err != nil {
		t.Fatalf("rename to .part: %v", err)
	}

	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	newDir := t.TempDir()
	if err := e.MoveStorage(id, newDir); err != nil {
		t.Fatalf("MoveStorage: %v", err)
	}

	if _, err := os.Stat(filepath.Join(newDir, "payload.bin"+partFileSuffix)); err != nil {
		t.Errorf("the incomplete data did not move: %v", err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Errorf("the incomplete data was left behind at the old location (err = %v)", err)
	}
}

// Moving re-adds the torrent against new storage, and the re-added one is
// a fresh instance with no priority history - so without putting them
// back, a move silently re-wants every file the user had switched off.
func TestMoveStorageKeepsFilePriorities(t *testing.T) {
	cfg := testConfig(t)
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	torrentPath := writeTestTorrent(t, cfg.DataDir, 4*1024*1024)

	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}
	// An add marks its files wanted from a goroutine that waits on
	// metadata, and for a .torrent that metadata is already there - so
	// setting a priority immediately races it, and the loser is silently
	// overwritten. Wait for it to have run before choosing anything.
	waitForFilePriority(t, e, id, PriorityNormal)

	if err := e.SetFilePriority(id, 0, PriorityNone); err != nil {
		t.Fatalf("SetFilePriority: %v", err)
	}

	if err := e.MoveStorage(id, t.TempDir()); err != nil {
		t.Fatalf("MoveStorage: %v", err)
	}

	detail, err := e.TorrentDetail(id)
	if err != nil {
		t.Fatalf("TorrentDetail: %v", err)
	}
	if len(detail.Files) == 0 {
		t.Fatal("the moved torrent reports no files")
	}
	if got := detail.Files[0].Priority; got != PriorityNone {
		t.Errorf("file priority after the move = %v, want %v", got, PriorityNone)
	}
}

// waitForFilePriority blocks until a torrent's first file reports want,
// so a test can act after the add has finished rather than during it.
func waitForFilePriority(t *testing.T, e *Engine, id TorrentID, want Priority) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		d, err := e.TorrentDetail(id)
		if err == nil && len(d.Files) > 0 && d.Files[0].Priority == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("file 0 never reached priority %v", want)
}

func findSnapshot(t *testing.T, e *Engine, id TorrentID) TorrentSnapshot {
	t.Helper()
	for _, s := range e.ListTorrents() {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("torrent %s is not in the list", id)
	return TorrentSnapshot{}
}

// A move that fails partway must let the torrent go again.
//
// The hold is invisible in a snapshot - a held torrent is neither paused
// nor errored - so leaving it set stopped the torrent transferring for
// the life of the daemon while the list went on reporting it as healthy.
func TestFailedMoveReleasesTheHold(t *testing.T) {
	cfg := testConfig(t)
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	torrentPath := writeTestTorrent(t, cfg.DataDir, 512*1024)
	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	// A destination the payload cannot be written to: a directory sitting
	// exactly where the file has to land. Every other way of failing a
	// move needs a full disk or a revoked permission.
	newDir := filepath.Join(t.TempDir(), "dest")
	if err := os.MkdirAll(filepath.Join(newDir, "payload.bin"), 0o755); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}

	if err := e.MoveStorage(id, newDir); err == nil {
		t.Fatal("MoveStorage onto a directory should have failed")
	}

	tr, err := e.lookup(id)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	e.mu.Lock()
	hold := tr.holdData
	down, up := tr.dataFlow(e.blocked)
	snap := e.snapshotLocked(id, tr)
	e.mu.Unlock()

	if hold {
		t.Error("holdData still set: the torrent can never transfer again")
	}
	if !down || !up {
		t.Errorf("data flow still off after a failed move (down=%v up=%v)", down, up)
	}
	// The hold cannot be seen, so the reason for the failure has to be.
	if snap.Error == "" {
		t.Error("a failed move left nothing on the torrent to explain it")
	}
	if snap.State != StateError {
		t.Errorf("state = %s after a failed move, want error", snap.State)
	}
}

// Nothing serializes two operations against one torrent: each IPC
// connection gets its own goroutine and dispatches inline. Both replace
// tr.t, and a verification started by one runs for hours reading it.
//
// Only meaningful under -race, which `make test-race` and CI both run.
func TestConcurrentMovesDoNotRace(t *testing.T) {
	cfg := testConfig(t)
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	torrentPath := writeTestTorrent(t, cfg.DataDir, 1024*1024)
	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	// One of the two may legitimately fail - they are moving the same
	// files out from under each other. What must not happen is a race, or
	// a torrent left held.
	base := t.TempDir()
	var wg sync.WaitGroup
	for _, dir := range []string{"a", "b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.MoveStorage(id, filepath.Join(base, dir))
		}()
	}
	wg.Wait()
}
