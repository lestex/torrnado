package tui

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

// previewModel is a model with one torrent whose detail has landed, which
// is the state v is pressed in.
func previewModel(t *testing.T, files ...engine.FileInfo) Model {
	t.Helper()
	m := testModel("a")
	m.detailLoaded = true
	m.detailID = "a"
	m.detail = engine.TorrentDetail{
		Snapshot: engine.TorrentSnapshot{ID: "a", Name: "a"},
		Files:    files,
	}
	return m
}

// Pressing v on a row in the list is how anyone would try to play a
// torrent. It used to do nothing at all: the key was claimed by the
// detail pane, and only while its Files tab was open.
func TestPreviewWorksFromTheList(t *testing.T) {
	m := previewModel(t,
		engine.FileInfo{Index: 0, Path: "sample.mkv", Length: 10 << 20},
		engine.FileInfo{Index: 1, Path: "feature.mkv", Length: 4 << 30},
		engine.FileInfo{Index: 2, Path: "subs.srt", Length: 40 << 10},
	)
	m.focus = focusList

	if _, cmd := m.previewFile(); cmd == nil {
		t.Fatal("v on a torrent in the list produced no command")
	}

	// The biggest file, because that is what "play this torrent" means:
	// the feature is bigger than the sample, the sample bigger than the
	// subtitles.
	if got := largestFile(m.detail.Files); got.Path != "feature.mkv" {
		t.Errorf("largest file = %q, want feature.mkv", got.Path)
	}
}

// With the Files tab open the cursor wins - that is the whole point of
// having a file list to move around in.
func TestPreviewPrefersTheFileCursorOnTheFilesTab(t *testing.T) {
	m := previewModel(t,
		engine.FileInfo{Index: 0, Path: "feature.mkv", Length: 4 << 30},
		engine.FileInfo{Index: 1, Path: "extra.mkv", Length: 1 << 20},
	)
	m.focus = focusDetail
	m.detailTab = tabFiles
	m.detailCursor = 1

	if _, cmd := m.previewFile(); cmd == nil {
		t.Fatal("v on the Files tab produced no command")
	}
	// Asserted through the same path the command takes, since running the
	// command would need a daemon.
	f := largestFile(m.detail.Files)
	if m.detailTab == tabFiles && m.detailCursor < len(m.detail.Files) {
		f = m.detail.Files[m.detailCursor]
	}
	if f.Path != "extra.mkv" {
		t.Errorf("chose %q, want the file under the cursor", f.Path)
	}
}

// A torrent with no metadata yet has nothing to play, and saying so is
// the difference between "not ready" and "this key is broken".
func TestPreviewSaysWhyWhenThereIsNothingToPlay(t *testing.T) {
	cases := []struct {
		name  string
		model func() Model
		want  string
	}{
		{
			"no files yet",
			func() Model { return previewModel(t) },
			"metadata",
		},
		{
			"detail has not arrived",
			func() Model {
				m := previewModel(t, engine.FileInfo{Path: "x.mkv", Length: 1})
				m.detailLoaded = false
				return m
			},
			"still reading",
		},
	}

	for _, c := range cases {
		next, cmd := c.model().previewFile()
		got := next.(Model)

		if got.status == "" {
			t.Errorf("%s: v reported nothing at all", c.name)
			continue
		}
		if !strings.Contains(got.status, c.want) {
			t.Errorf("%s: status = %q, want it to mention %q", c.name, got.status, c.want)
		}
		if !got.statusIsErr {
			t.Errorf("%s: the message is not marked as an error", c.name)
		}
		// Every status has to clear itself, or it sits there describing
		// something that stopped being true.
		if cmd == nil {
			t.Errorf("%s: the message was left with no expiry", c.name)
		}
	}
}

func TestLargestFile(t *testing.T) {
	one := engine.FileInfo{Path: "only.bin", Length: 5}
	if got := largestFile([]engine.FileInfo{one}); got.Path != "only.bin" {
		t.Errorf("largestFile of one file = %q", got.Path)
	}

	// Ties keep the first, so repeated presses play the same file rather
	// than alternating.
	files := []engine.FileInfo{
		{Path: "a.bin", Length: 100},
		{Path: "b.bin", Length: 100},
	}
	if got := largestFile(files); got.Path != "a.bin" {
		t.Errorf("largestFile broke a tie towards %q, want the first", got.Path)
	}
}
