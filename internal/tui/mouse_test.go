package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/engine"
)

// ansiRE matches the escape sequences lipgloss emits when the terminal
// supports colour. Tests usually run without one and get plain text, but
// measuring columns against a string that might carry escapes would make
// this pass or fail on where it was run.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// click builds a left-button press at a terminal coordinate.
func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}

func wheel(x, y int, up bool) tea.MouseMsg {
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress}
}

func mouseModel(names ...string) Model {
	m := testModel(names...)
	m.width, m.height = 120, 40
	return m
}

// The panes are laid out by size alone, so a coordinate has to be turned
// back into a pane and a content row. Off by one here puts a click on the
// row above the one it appeared to hit.
func TestHitTestFindsTheRightPane(t *testing.T) {
	m := mouseModel("a")
	p := layout(120, 40)

	for _, tc := range []struct {
		name    string
		x, y    int
		want    mouseTarget
		wantRow int
	}{
		{"sidebar first content row", 3, 1, targetSidebar, 0},
		{"sidebar top border", 3, 0, targetNone, 0},
		{"list header row", 40, 1, targetList, 0},
		{"list first torrent", 40, 2, targetList, 1},
		{"detail tab strip", 40, p.listH + 1, targetDetailTabs, 0},
		{"detail body", 40, p.listH + 2, targetDetailBody, 0},
	} {
		got, row := m.hitTest(p, tc.x, tc.y)
		if got != tc.want || row != tc.wantRow {
			t.Errorf("%s: (%d,%d) = %v row %d, want %v row %d",
				tc.name, tc.x, tc.y, got, row, tc.want, tc.wantRow)
		}
	}
}

// Clicking a torrent puts the cursor on the one that was under the
// pointer, not on whatever was nearby.
func TestClickingARowMovesTheCursorToIt(t *testing.T) {
	m := mouseModel("a", "b", "c", "d")

	// Content row 0 is the header, so the third torrent is row 3.
	next, _ := m.Update(click(40, 1+3))
	got := next.(Model)

	if got.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (the third torrent)", got.cursor)
	}
	if got.focus != focusList {
		t.Errorf("focus = %v, want the list - a click moves focus too", got.focus)
	}
}

// The column header is not a torrent, and clicking it must not select the
// first one by accident.
func TestClickingTheListHeaderSelectsNothing(t *testing.T) {
	m := mouseModel("a", "b")
	m.cursor = 1

	next, _ := m.Update(click(40, 1))
	if got := next.(Model); got.cursor != 1 {
		t.Errorf("cursor = %d, want it left alone", got.cursor)
	}
}

// A click on a sidebar filter applies it; one on a heading or a blank
// does nothing, which is what the row-to-entry mapping is for.
func TestClickingTheSidebarAppliesThatFilter(t *testing.T) {
	m := mouseModel("a")
	p := layout(120, 40)
	_, rowEntry := m.buildSidebar(p)

	var statusRow, chromeRow = -1, -1
	for row, entry := range rowEntry {
		if entry == int(filterSeeding) && statusRow < 0 {
			statusRow = row
		}
		if entry < 0 && chromeRow < 0 {
			chromeRow = row
		}
	}
	if statusRow < 0 || chromeRow < 0 {
		t.Fatalf("could not find rows to click: %v", rowEntry)
	}

	next, _ := m.Update(click(3, statusRow+1))
	if got := next.(Model); got.filter != filterSeeding {
		t.Errorf("filter = %v, want filterSeeding", got.filter)
	}

	m.filter = filterAll
	next, _ = m.Update(click(3, chromeRow+1))
	if got := next.(Model); got.filter != filterAll {
		t.Errorf("clicking chrome changed the filter to %v", got.filter)
	}
}

// The tab spans are computed rather than measured, so this checks each
// one actually lands on its own label in the strip as drawn - the two
// descriptions of that layout must not drift apart.
func TestTabSpansAgreeWithTheRenderedStrip(t *testing.T) {
	m := mouseModel("a")
	p := layout(120, 40)
	// By rune, not by byte: the strip opens with a box-drawing dash that
	// is one column wide and three bytes long, so byte offsets would
	// disagree with the columns a click arrives in.
	strip := []rune(stripANSI(m.renderDetailTabs(p)))

	for i, span := range detailTabSpans() {
		if span.start+span.width > len(strip) {
			t.Fatalf("span %d runs past the strip %q", i, string(strip))
		}
		got := string(strip[span.start : span.start+span.width])
		if !strings.Contains(got, detailTabNames[i]) {
			t.Errorf("span %d covers %q, want it over %q", i, got, detailTabNames[i])
		}
	}
}

func TestClickingATabSwitchesToIt(t *testing.T) {
	m := mouseModel("a")
	p := layout(120, 40)
	spans := detailTabSpans()

	// The Files tab, in terminal coordinates.
	x := p.sidebarW + 1 + panePadX + spans[tabFiles].start
	next, _ := m.Update(click(x, p.listH+1))

	got := next.(Model)
	if got.detailTab != tabFiles {
		t.Errorf("detailTab = %v, want tabFiles", got.detailTab)
	}
	if got.focus != focusDetail {
		t.Errorf("focus = %v, want the detail pane", got.focus)
	}
}

// The wheel acts on the pane it is over, not the focused one - that is
// what makes it useful without clicking first.
func TestTheWheelScrollsThePaneUnderThePointer(t *testing.T) {
	m := mouseModel("a", "b", "c")
	m.focus = focusSidebar

	next, _ := m.Update(wheel(40, 3, false)) // over the list
	got := next.(Model)
	if got.cursor != 1 {
		t.Errorf("cursor = %d, want 1: the wheel should move the list it is over", got.cursor)
	}
	if got.filter != filterAll {
		t.Errorf("the wheel moved the focused sidebar instead of the list under it")
	}
}

func TestTheWheelDoesNotRunOffTheEndsOfTheList(t *testing.T) {
	m := mouseModel("a", "b")

	next, _ := m.Update(wheel(40, 3, true)) // up, already at the top
	if got := next.(Model); got.cursor != 0 {
		t.Errorf("cursor = %d, want 0", got.cursor)
	}

	m.cursor = 1
	next, _ = m.Update(wheel(40, 3, false)) // down, already at the end
	if got := next.(Model); got.cursor != 1 {
		t.Errorf("cursor = %d, want 1", got.cursor)
	}
}

// A release would act a second time on the same gesture.
func TestOnlyAPressActs(t *testing.T) {
	m := mouseModel("a", "b", "c")

	next, _ := m.Update(tea.MouseMsg{
		X: 40, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease,
	})
	if got := next.(Model); got.cursor != 0 {
		t.Errorf("cursor = %d, want 0: a release must not act", got.cursor)
	}
}

// Mouse events can arrive before the first WindowSizeMsg, and laying out
// a zero-sized terminal would divide the panes into nothing.
func TestAMouseEventBeforeTheFirstResizeIsIgnored(t *testing.T) {
	m := testModel("a") // no width or height yet
	m.selected = map[engine.TorrentID]bool{}

	if _, cmd := m.Update(click(10, 10)); cmd != nil {
		t.Error("a click with no layout yet should do nothing")
	}
}
