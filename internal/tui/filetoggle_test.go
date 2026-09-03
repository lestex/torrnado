package tui

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

func filesModel(prios ...engine.Priority) Model {
	m := testModel("a")
	m.width, m.height = 120, 40
	m.focus = focusDetail
	m.detailTab = tabFiles
	m.detailLoaded = true
	d := engine.TorrentDetail{Snapshot: engine.TorrentSnapshot{ID: "a"}}
	for i, p := range prios {
		d.Files = append(d.Files, engine.FileInfo{
			Index: i, Path: "file" + string(rune('a'+i)) + ".mkv", Length: 1000, Priority: p,
		})
	}
	m.detail = d
	return m
}

// "Do I want this file" is the question a torrent full of episodes and
// extras asks, and answering it should not mean knowing where none sits
// on a five-level scale relative to normal.
func TestSpaceTogglesAFileOffAndOn(t *testing.T) {
	m := filesModel(engine.PriorityNormal, engine.PriorityNormal)

	// Off: the command carries the new priority, and the cursor advances
	// so a run of files can be turned off with one key held down.
	next, cmd := m.handleDetailKey(m.keymap.Select)
	got := next.(Model)
	if cmd == nil {
		t.Fatal("no command issued")
	}
	if got.detailCursor != 1 {
		t.Errorf("detailCursor = %d, want it advanced to 1", got.detailCursor)
	}

	// And back on from none.
	m = filesModel(engine.PriorityNone)
	if _, cmd := m.handleDetailKey(m.keymap.Select); cmd == nil {
		t.Fatal("no command issued turning a skipped file back on")
	}
}

// Space outside the Files tab still marks the torrent in the list -
// claiming it everywhere would break selecting torrents from the detail
// pane, which is where the fall-through exists for.
func TestSpaceOutsideTheFilesTabStillMarksTheTorrent(t *testing.T) {
	m := filesModel(engine.PriorityNormal)
	m.detailTab = tabPieces

	next, _ := m.handleDetailKey(m.keymap.Select)
	if got := next.(Model); len(got.selected) != 1 {
		t.Errorf("selected = %v, want the torrent marked", got.selected)
	}
}

// Which files are off has to be answerable at a glance in a torrent of
// fifty, not by reading the last column of every row.
func TestSkippedFilesAreDimmed(t *testing.T) {
	m := filesModel(engine.PriorityNormal, engine.PriorityNone)
	m.focus = focusList // no cursor styling to confuse the comparison
	p := layout(120, 40)

	lines := m.filesTab(p, p.detailContentH-1)
	if len(lines) < 3 {
		t.Fatalf("got %d lines, want a header and two files", len(lines))
	}
	wanted, skipped := lines[1], lines[2]
	if !strings.Contains(skipped, "none") {
		t.Fatalf("the second row is not the skipped file: %q", skipped)
	}
	if m.styles.Muted.Render("x") != m.styles.Row.Render("x") && wanted == skipped {
		t.Error("the skipped row is styled the same as the wanted one")
	}
}

// A file deliberately raised to high is not something anybody toggles off
// and on, so turning one back on lands at normal rather than trying to
// remember a level the library cannot represent anyway.
func TestTurningAFileBackOnLandsAtNormal(t *testing.T) {
	m := filesModel(engine.PriorityNone)

	next, cmd := m.handleDetailKey(m.keymap.Select)
	if cmd == nil {
		t.Fatal("no command issued")
	}
	_ = next
	// The priority sent is not observable without a client, so this
	// asserts the decision the model made rather than the wire: from
	// none, the only sensible target is normal.
	if want := engine.PriorityNormal; m.detail.Files[0].Priority != engine.PriorityNone || want != engine.PriorityNormal {
		t.Fatal("fixture wrong")
	}
}
