package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newPersistentEngine builds an engine that saves its session into a
// directory the testing package cleans up.
func newPersistentEngine(t *testing.T, stateDir string) *Engine {
	t.Helper()
	cfg := testConfig(t)
	cfg.StateDir = stateDir
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func readSession(t *testing.T, stateDir string) sessionFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateDir, "session.json"))
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	var sf sessionFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("parse session: %v", err)
	}
	return sf
}

func TestAddingATorrentWritesTheSession(t *testing.T) {
	dir := t.TempDir()
	e := newPersistentEngine(t, dir)

	id, err := e.AddMagnet(testMagnet, AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}

	sf := readSession(t, dir)
	if sf.Version != sessionVersion {
		t.Errorf("version = %d, want %d", sf.Version, sessionVersion)
	}
	if len(sf.Torrents) != 1 {
		t.Fatalf("saved %d torrents, want 1", len(sf.Torrents))
	}
	rec := sf.Torrents[0]
	if rec.InfoHash != string(id) {
		t.Errorf("info hash = %q, want %q", rec.InfoHash, id)
	}
	// Without the magnet URI a torrent whose metadata never arrived has
	// no way back at all - there is no metainfo file for it either.
	if rec.Magnet != testMagnet {
		t.Errorf("magnet = %q, want %q", rec.Magnet, testMagnet)
	}
}

func TestPausingIsSaved(t *testing.T) {
	dir := t.TempDir()
	e := newPersistentEngine(t, dir)

	id, _ := e.AddMagnet(testMagnet, AddOpts{})
	if err := e.SetPaused(id, true); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	if sf := readSession(t, dir); !sf.Torrents[0].Paused {
		t.Error("a paused torrent was saved as running")
	}
}

func TestRateLimitsAreSaved(t *testing.T) {
	dir := t.TempDir()
	e := newPersistentEngine(t, dir)

	id, _ := e.AddMagnet(testMagnet, AddOpts{})
	if err := e.SetTorrentRateLimit(id, 1000, 2000); err != nil {
		t.Fatalf("SetTorrentRateLimit: %v", err)
	}

	rec := readSession(t, dir).Torrents[0]
	if rec.UpLimit != 1000 || rec.DownLimit != 2000 {
		t.Errorf("limits = %d/%d, want 1000/2000", rec.UpLimit, rec.DownLimit)
	}
}

func TestRemovingATorrentDropsItFromTheSession(t *testing.T) {
	dir := t.TempDir()
	e := newPersistentEngine(t, dir)

	id, _ := e.AddMagnet(testMagnet, AddOpts{})
	if err := e.RemoveTorrent(id, false); err != nil {
		t.Fatalf("RemoveTorrent: %v", err)
	}

	if sf := readSession(t, dir); len(sf.Torrents) != 0 {
		t.Errorf("a removed torrent is still in the session: %+v", sf.Torrents)
	}
}

// The point of the whole file: a second engine over the same state
// directory comes back with what the first one had.
func TestARestartRestoresTheTorrentList(t *testing.T) {
	dir := t.TempDir()

	first := newPersistentEngine(t, dir)
	id, _ := first.AddMagnet(testMagnet, AddOpts{Paused: true})
	if err := first.SetTorrentRateLimit(id, 4096, 8192); err != nil {
		t.Fatalf("SetTorrentRateLimit: %v", err)
	}
	first.Close()

	second := newPersistentEngine(t, dir)
	n, err := second.RestoreSession()
	if err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if n != 1 {
		t.Fatalf("restored %d torrents, want 1", n)
	}

	list := second.ListTorrents()
	if len(list) != 1 {
		t.Fatalf("engine lists %d torrents after restore, want 1", len(list))
	}
	got := list[0]
	if got.ID != id {
		t.Errorf("id = %q, want %q", got.ID, id)
	}
	if !got.Paused {
		t.Error("a torrent saved paused came back running")
	}
	if got.UploadLimit != 4096 || got.DownloadLimit != 8192 {
		t.Errorf("limits = %d/%d, want 4096/8192", got.UploadLimit, got.DownloadLimit)
	}
}

// A restart should not make everything look freshly added.
func TestRestoreKeepsTheOriginalAddedTime(t *testing.T) {
	dir := t.TempDir()

	first := newPersistentEngine(t, dir)
	id, _ := first.AddMagnet(testMagnet, AddOpts{})
	first.mu.Lock()
	want := time.Now().Add(-72 * time.Hour).Round(time.Second)
	first.torrents[id].addedAt = want
	first.mu.Unlock()
	if err := first.SaveSession(); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	first.Close()

	second := newPersistentEngine(t, dir)
	if _, err := second.RestoreSession(); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	second.mu.Lock()
	got := second.torrents[id].addedAt
	second.mu.Unlock()

	if !got.Equal(want) {
		t.Errorf("added at %v, want the saved %v", got, want)
	}
}

// A server that will not start because one byte of JSON is wrong is worse
// than one that starts with nothing restored.
func TestACorruptSessionIsAnErrorAndNotAPanic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newPersistentEngine(t, dir)
	n, err := e.RestoreSession()
	if err == nil {
		t.Fatal("a corrupt session should be reported")
	}
	if n != 0 {
		t.Errorf("restored %d torrents from a corrupt file", n)
	}
	// The engine has to stay usable: the daemon logs this and carries on.
	if _, err := e.AddMagnet(testMagnet, AddOpts{}); err != nil {
		t.Errorf("engine unusable after a corrupt session: %v", err)
	}
}

// One unreadable record must not take the readable ones with it.
func TestABadRecordIsSkippedAndTheRestRestored(t *testing.T) {
	dir := t.TempDir()
	sf := sessionFile{
		Version: sessionVersion,
		Torrents: []torrentRecord{
			// Neither a magnet nor a saved metainfo: nothing to re-add from.
			{InfoHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			{InfoHash: testInfoHash, Magnet: testMagnet},
		},
	}
	data, _ := json.Marshal(sf)
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	e := newPersistentEngine(t, dir)
	n, err := e.RestoreSession()
	if err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if n != 1 {
		t.Fatalf("restored %d torrents, want the 1 good one", n)
	}
}

// Reading a file whose fields may have been redefined is worse than
// reading none of it.
func TestANewerSessionVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	data, _ := json.Marshal(sessionFile{Version: sessionVersion + 1})
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	e := newPersistentEngine(t, dir)
	if _, err := e.RestoreSession(); err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Errorf("err = %v, want one naming a newer version", err)
	}
}

// The first run has no session file, and that is not a failure.
func TestRestoringWithNoSessionFileIsNotAnError(t *testing.T) {
	e := newPersistentEngine(t, t.TempDir())
	n, err := e.RestoreSession()
	if err != nil {
		t.Errorf("RestoreSession on a fresh state dir: %v", err)
	}
	if n != 0 {
		t.Errorf("restored %d torrents from nothing", n)
	}
}

// An embedder that never sets a state directory should get the old
// behavior, not a scattering of files in the working directory.
func TestNoStateDirMeansNoFiles(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.AddMagnet(testMagnet, AddOpts{}); err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}
	if err := e.SaveSession(); err != nil {
		t.Errorf("SaveSession with no state dir: %v", err)
	}
	if _, err := os.Stat("session.json"); err == nil {
		os.Remove("session.json")
		t.Error("a session file was written with no state dir configured")
	}
}

// A half-written session is unreadable, and the moment it is needed is
// exactly the moment the machine died mid-write.
func TestWriteFileAtomicLeavesNoPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	if err := writeFileAtomic(path, []byte("first")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	if err := writeFileAtomic(path, []byte("second")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", got, "second")
	}

	// The temporary files must not survive the rename.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), ".tmp-") {
			t.Errorf("left a temporary file behind: %s", ent.Name())
		}
	}
}

// A ratio has to survive a restart, or a seed limit built on it would
// never fire on the one machine it is for: the library counts bytes per
// torrent instance, and a restart makes a new instance.
func TestLifetimeTotalsSurviveARestart(t *testing.T) {
	cfg := testConfig(t)
	cfg.StateDir = t.TempDir()

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	torrentPath := writeTestTorrent(t, cfg.DataDir, 256*1024)
	id, err := e.AddTorrentFile(torrentPath, AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	// Stand in for bytes moved: the test has no swarm, so the totals are
	// set directly and then have to come back the same.
	e.mu.Lock()
	e.torrents[id].baseUploaded = 3 << 20
	e.torrents[id].baseDownloaded = 1 << 20
	e.mu.Unlock()
	if err := e.SaveSession(); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	e.Close()

	next, err := New(cfg)
	if err != nil {
		t.Fatalf("New (restart): %v", err)
	}
	t.Cleanup(func() { next.Close() })
	if _, err := next.RestoreSession(); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	var snap TorrentSnapshot
	for _, s := range next.ListTorrents() {
		if s.ID == id {
			snap = s
		}
	}
	if snap.Uploaded < 3<<20 {
		t.Errorf("uploaded = %d after restart, want at least %d", snap.Uploaded, 3<<20)
	}
	if snap.Downloaded < 1<<20 {
		t.Errorf("downloaded = %d after restart, want at least %d", snap.Downloaded, 1<<20)
	}
	// 3 MiB up over 1 MiB down.
	if snap.Ratio < 2.9 || snap.Ratio > 3.1 {
		t.Errorf("ratio = %v after restart, want ~3", snap.Ratio)
	}
}

// A move drops and re-adds the torrent, which resets the library's
// counters. The totals must not go backwards.
func TestLifetimeTotalsSurviveAMove(t *testing.T) {
	cfg := testConfig(t)
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

	e.mu.Lock()
	e.torrents[id].baseUploaded = 5 << 20
	e.mu.Unlock()

	if err := e.MoveStorage(id, filepath.Join(t.TempDir(), "dest")); err != nil {
		t.Fatalf("MoveStorage: %v", err)
	}

	var snap TorrentSnapshot
	for _, s := range e.ListTorrents() {
		if s.ID == id {
			snap = s
		}
	}
	if snap.Uploaded < 5<<20 {
		t.Errorf("uploaded = %d after a move, want at least %d", snap.Uploaded, 5<<20)
	}
}
