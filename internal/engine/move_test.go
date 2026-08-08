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
