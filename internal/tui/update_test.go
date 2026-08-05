package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/engine"
)

// press feeds one key to the model, the way bubbletea would.
func press(m Model, key string) Model {
	var msg tea.KeyMsg
	switch key {
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func testModel(names ...string) Model {
	m := Model{
		keymap:   DefaultKeyMap(),
		selected: map[engine.TorrentID]bool{},
	}
	for _, n := range names {
		m.torrents = append(m.torrents, engine.TorrentSnapshot{
			ID: engine.TorrentID(n), Name: n, State: engine.StateDownloading,
		})
	}
	return m
}

func TestCursorMovesWithinBounds(t *testing.T) {
	m := testModel("a", "b", "c")

	m = press(m, "j")
	if m.cursor != 1 {
		t.Errorf("after j, cursor = %d, want 1", m.cursor)
	}

	// Already at the top: k must not run off the start.
	m.cursor = 0
	m = press(m, "k")
	if m.cursor != 0 {
		t.Errorf("k at the top moved the cursor to %d", m.cursor)
	}

	// Nor off the end.
	m.cursor = 2
	m = press(m, "j")
	if m.cursor != 2 {
		t.Errorf("j at the bottom moved the cursor to %d", m.cursor)
	}
}

func TestTopAndBottom(t *testing.T) {
	m := testModel("a", "b", "c")
	m.cursor = 1

	if m = press(m, "G"); m.cursor != 2 {
		t.Errorf("G put the cursor at %d, want 2", m.cursor)
	}
	if m = press(m, "g"); m.cursor != 0 {
		t.Errorf("g put the cursor at %d, want 0", m.cursor)
	}
}

// Marking a row advances, so a run can be selected by holding one key.
func TestSelectMarksAndAdvances(t *testing.T) {
	m := testModel("a", "b", "c")

	m = press(m, " ")
	if !m.selected["a"] {
		t.Error("space did not mark the cursor row")
	}
	if m.cursor != 1 {
		t.Errorf("space left the cursor at %d, want it advanced", m.cursor)
	}

	// And is a toggle.
	m.cursor = 0
	m = press(m, " ")
	if m.selected["a"] {
		t.Error("space did not unmark an already-marked row")
	}
}

// Torrents disappear while being looked at, so the cursor has to be
// corrected when the list shrinks -- otherwise it points past the end and
// every action reads the wrong row, or none.
func TestCursorIsClampedWhenTorrentsVanish(t *testing.T) {
	m := testModel("a", "b", "c")
	m.cursor = 2

	next, _ := m.Update(engineEventMsg{Torrents: []engine.TorrentSnapshot{{ID: "a", Name: "a"}}})
	m = next.(Model)

	if m.cursor != 0 {
		t.Errorf("cursor = %d after the list shrank to one row", m.cursor)
	}
	if _, ok := m.cursorTorrent(); !ok {
		t.Error("cursor does not point at a torrent")
	}
}

// The cursor indexes the visible list, so a filter that hides the row it
// was on must not leave it dangling.
func TestCursorIsClampedByTheFilter(t *testing.T) {
	m := testModel("a", "b", "c")
	m.cursor = 2
	m.filter = filterSeeding // nothing here is seeding

	m.clampCursor(len(m.visibleTorrents()))

	if _, ok := m.cursorTorrent(); ok {
		t.Error("cursor points at a torrent when none is visible")
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d with an empty list, want 0", m.cursor)
	}
}

// An action applies to the marked torrents if any are marked, and to the
// row under the cursor otherwise. That rule is what lets the same keys
// work on one torrent or fifty with no separate mode.
func TestTargetsPrefersSelectionOverCursor(t *testing.T) {
	m := testModel("a", "b", "c")
	visible := m.visibleTorrents()

	m.cursor = 2
	got := m.targets(visible)
	if len(got) != 1 || got[0].Name != "c" {
		t.Errorf("with nothing marked, targets = %v, want just the cursor row", got)
	}

	m.selected["a"] = true
	m.selected["b"] = true
	got = m.targets(visible)
	if len(got) != 2 {
		t.Fatalf("with two marked, targets = %v", got)
	}
	// List order, not map order: a status message counting them, or a
	// partial failure, has to be reproducible.
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("targets came back in %v, want list order", got)
	}
}

func TestTargetsIsEmptyWhenNothingIsVisible(t *testing.T) {
	m := testModel()
	if got := m.targets(m.visibleTorrents()); len(got) != 0 {
		t.Errorf("targets = %v on an empty list", got)
	}
}

// Escape peels one layer at a time rather than clearing everything, so it
// is never a surprise.
func TestBackClearsSelectionThenFilter(t *testing.T) {
	m := testModel("a", "b")
	m.filter = filterDownloading
	m.selected["a"] = true

	m = press(m, "esc")
	if len(m.selected) != 0 {
		t.Error("first escape should clear the selection")
	}
	if m.filter != filterDownloading {
		t.Error("first escape should leave the filter alone")
	}

	m = press(m, "esc")
	if m.filter != filterAll {
		t.Error("second escape should clear the filter")
	}
}
