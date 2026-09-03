package engine

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// path.Match's * does not cross a separator, so a pattern matched against
// the full path would pick out nothing in a torrent with any folder
// structure - which is most of them, and exactly the ones worth selecting
// from. A pattern with a slash asks for a path, one without asks for a
// name.
func TestPatternsMatchNamesUnlessTheyAskForAPath(t *testing.T) {
	for _, tc := range []struct {
		path    string
		pattern string
		want    bool
		why     string
	}{
		{"Season 1/ep1.mkv", "*.mkv", true, "a bare pattern matches the base name at any depth"},
		{"ep1.mkv", "*.mkv", true, "and at the top level"},
		{"Season 1/ep1.nfo", "*.mkv", false, "a non-match is a non-match"},
		{"Season 1/ep1.mkv", "Season 1/*", true, "a pattern with a slash matches the path"},
		{"Season 2/ep1.mkv", "Season 1/*", false, "and only that path"},
		{"Extras/behind.mkv", "*.mkv", true, "the folder does not matter to a bare pattern"},
		{"a/b/c.mkv", "*/*.mkv", false, "a slashed pattern is exact about depth"},
		{"a/b/c.mkv", "a/*/*.mkv", true, "as deep as it says"},
		{"ep1.mkv", "[", false, "a malformed pattern matches nothing rather than erroring"},
	} {
		if got := matchesAnyPattern(tc.path, []string{tc.pattern}); got != tc.want {
			t.Errorf("%q vs %q = %v, want %v: %s", tc.path, tc.pattern, got, tc.want, tc.why)
		}
	}
}

// Several patterns are an "or": one file type plus one folder is the
// obvious thing to ask for.
func TestAnyPatternMatchingIsEnough(t *testing.T) {
	pats := []string{"*.mkv", "Subs/*"}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"Season 1/ep1.mkv", true},
		{"Subs/ep1.srt", true},
		{"Extras/readme.txt", false},
	} {
		if got := matchesAnyPattern(tc.path, pats); got != tc.want {
			t.Errorf("%q = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// buildSeasonTorrent writes a multi-file torrent shaped like the case
// this feature exists for: the files you want, and the ones you do not.
func buildSeasonTorrent(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	dir := filepath.Join(parent, "Season 1")
	if err := os.MkdirAll(filepath.Join(dir, "Extras"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("x"), 32*1024)
	for _, f := range []string{"ep1.mkv", "ep2.mkv", "notes.nfo", "Extras/blooper.mkv"} {
		if err := os.WriteFile(filepath.Join(dir, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	info := metainfo.Info{PieceLength: 16 * 1024}
	if err := info.BuildFromFilePath(dir); err != nil {
		t.Fatalf("build info: %v", err)
	}
	info.MetaVersion = 0
	info.FileTree = metainfo.FileTree{}

	var mi metainfo.MetaInfo
	var err error
	if mi.InfoBytes, err = bencode.Marshal(info); err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	path := filepath.Join(parent, "season.torrent")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := mi.Write(f); err != nil {
		t.Fatalf("write torrent: %v", err)
	}
	return path
}

// waitForPriorities polls until every named file has the priority given,
// or fails reporting what it last saw.
//
// Polling rather than reading once: the file list exists as soon as a
// .torrent is added, but the priorities are set by the goroutine that
// waits on the metadata, so a single read races it and would pass or
// fail depending on scheduling.
func waitForPriorities(t *testing.T, e *Engine, id TorrentID, want map[string]Priority) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last map[string]Priority
	for {
		last = prioritiesByName(t, e, id)
		matched := len(last) > 0
		for name, p := range want {
			if last[name] != p {
				matched = false
				break
			}
		}
		if matched {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("priorities never settled: got %v, want %v", last, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// prioritiesByName reports each file's priority by base name, once the
// file list exists.
func prioritiesByName(t *testing.T, e *Engine, id TorrentID) map[string]Priority {
	t.Helper()
	d, err := e.TorrentDetail(id)
	if err != nil {
		t.Fatalf("TorrentDetail: %v", err)
	}
	out := map[string]Priority{}
	for _, f := range d.Files {
		out[path.Base(f.Path)] = f.Priority
	}
	return out
}

// The whole point: adding with a selection must not leave the unwanted
// files wanted, because by the time you could fix it by hand the torrent
// has already been pulling them.
func TestAddingWithFilesDownloadsOnlyThose(t *testing.T) {
	e := newTestEngine(t)

	id, err := e.AddTorrentFile(buildSeasonTorrent(t), AddOpts{Files: []string{"*.mkv"}})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	waitForPriorities(t, e, id, map[string]Priority{
		"ep1.mkv":     PriorityNormal,
		"ep2.mkv":     PriorityNormal,
		"blooper.mkv": PriorityNormal,
		"notes.nfo":   PriorityNone,
	})
}

// A pattern with a slash asks for a path, which is how you take one
// folder and leave another.
func TestAPathPatternSelectsAFolder(t *testing.T) {
	e := newTestEngine(t)

	id, err := e.AddTorrentFile(buildSeasonTorrent(t), AddOpts{Files: []string{"Season 1/Extras/*"}})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	waitForPriorities(t, e, id, map[string]Priority{
		"blooper.mkv": PriorityNormal,
		"ep1.mkv":     PriorityNone,
	})
}

// Without a selection nothing changes: every file is wanted, which is
// what every existing torrent relies on.
func TestAddingWithoutFilesStillWantsEverything(t *testing.T) {
	e := newTestEngine(t)

	id, err := e.AddTorrentFile(buildSeasonTorrent(t), AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	waitForPriorities(t, e, id, map[string]Priority{
		"ep1.mkv":     PriorityNormal,
		"ep2.mkv":     PriorityNormal,
		"blooper.mkv": PriorityNormal,
		"notes.nfo":   PriorityNormal,
	})
}

// A selection that matches nothing is honoured rather than widened back
// to everything - that would defeat the point of asking - but it is said
// out loud, because a torrent downloading nothing otherwise reads as a
// stall.
func TestASelectionMatchingNothingIsHonouredAndLogged(t *testing.T) {
	e, logs := newLoggingEngine(t)

	id, err := e.AddTorrentFile(buildSeasonTorrent(t), AddOpts{Files: []string{"*.avi"}})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	waitForPriorities(t, e, id, map[string]Priority{
		"ep1.mkv":   PriorityNone,
		"notes.nfo": PriorityNone,
	})

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "no files matched") {
		if time.Now().After(deadline) {
			t.Fatalf("the empty selection was not logged:\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
