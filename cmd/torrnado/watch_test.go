package main

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testWatcher(t *testing.T) (*watcher, string, *[]string) {
	t.Helper()
	dir := t.TempDir()
	var added []string
	w := newWatcher(dir, func(p string) error {
		added = append(added, filepath.Base(p))
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return w, dir, &added
}

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// A file is only taken once its size has settled. A .torrent arriving
// over a share is written in pieces, and one read halfway through is half
// of a good file - taking it would fail to parse and mark a perfectly
// good torrent failed.
func TestWatcherWaitsForAFileToSettle(t *testing.T) {
	w, dir, added := testWatcher(t)
	write(t, dir, "a.torrent", "partial")

	w.scan()
	if len(*added) != 0 {
		t.Fatalf("added on the first sighting: %v", *added)
	}

	// Still growing.
	write(t, dir, "a.torrent", "partial plus more")
	w.scan()
	if len(*added) != 0 {
		t.Fatalf("added while still being written: %v", *added)
	}

	// Unchanged since the last scan.
	w.scan()
	if len(*added) != 1 || (*added)[0] != "a.torrent" {
		t.Fatalf("added = %v, want [a.torrent]", *added)
	}
}

// Renamed rather than deleted: the dropped file may be the only copy.
// The marker is also what stops it being added twice, so nothing has to
// be remembered across a restart.
func TestWatcherMarksWhatItTook(t *testing.T) {
	w, dir, added := testWatcher(t)
	write(t, dir, "a.torrent", "x")
	w.scan()
	w.scan()

	if len(*added) != 1 {
		t.Fatalf("added = %v, want one", *added)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.torrent"+addedSuffix)); err != nil {
		t.Errorf("no %s marker left behind: %v", addedSuffix, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.torrent")); !os.IsNotExist(err) {
		t.Error("the original name is still there, so it will be added again")
	}

	// And a fresh watcher - which is what a restart is - must not take it
	// a second time.
	next := newWatcher(dir, func(string) error {
		t.Error("a file already marked was added again")
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	next.scan()
	next.scan()
}

// A torrent that will never parse must not be retried every couple of
// seconds forever, filling the log with the same error.
func TestWatcherMarksWhatItCouldNotAdd(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	w := newWatcher(dir, func(string) error {
		calls++
		return errors.New("not a torrent")
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	write(t, dir, "bad.torrent", "nonsense")
	w.scan()
	w.scan()
	w.scan()
	w.scan()

	if calls != 1 {
		t.Errorf("add attempted %d times, want 1", calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.torrent"+failedSuffix)); err != nil {
		t.Errorf("no %s marker left behind: %v", failedSuffix, err)
	}
}

// The same rule `torrnado add <dir>` uses: .torrent files directly
// inside, nothing else and no recursive walk.
func TestWatcherIgnoresEverythingElse(t *testing.T) {
	w, dir, added := testWatcher(t)
	write(t, dir, "notes.txt", "x")
	write(t, dir, "already.torrent"+addedSuffix, "x")
	write(t, dir, "broken.torrent"+failedSuffix, "x")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write(t, dir, "sub/deep.torrent", "x")

	w.scan()
	w.scan()

	if len(*added) != 0 {
		t.Errorf("added = %v, want nothing", *added)
	}
}

// Case matters on some filesystems and not others; the add command
// already treats the extension case-insensitively.
func TestWatcherAcceptsAnUppercaseExtension(t *testing.T) {
	w, dir, added := testWatcher(t)
	write(t, dir, "A.TORRENT", "x")
	w.scan()
	w.scan()

	if len(*added) != 1 {
		t.Errorf("added = %v, want the uppercase file", *added)
	}
}

// A watch directory on a share that is not mounted yet is a normal state
// at boot, not something to crash or spam the log over.
func TestWatcherSurvivesAMissingDirectory(t *testing.T) {
	w := newWatcher(filepath.Join(t.TempDir(), "absent"), func(string) error {
		t.Error("added from a directory that does not exist")
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.scan()
	w.scan()
}
