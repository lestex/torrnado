package tui

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

// Completed is about progress, not state, so it overlaps Seeding and
// Stopped rather than partitioning against them: a finished torrent that
// has been paused is still completed.
func TestStatusFilterMatches(t *testing.T) {
	downloading := engine.TorrentSnapshot{State: engine.StateDownloading, TotalLength: 100, Completed: 10}
	seeding := engine.TorrentSnapshot{State: engine.StateSeeding, TotalLength: 100, Completed: 100}
	pausedPart := engine.TorrentSnapshot{State: engine.StatePaused, Paused: true, TotalLength: 100, Completed: 10}
	pausedDone := engine.TorrentSnapshot{State: engine.StatePaused, Paused: true, TotalLength: 100, Completed: 100}
	failed := engine.TorrentSnapshot{State: engine.StateError}

	cases := []struct {
		filter statusFilter
		snap   engine.TorrentSnapshot
		want   bool
	}{
		{filterAll, downloading, true},
		{filterAll, failed, true},

		{filterDownloading, downloading, true},
		{filterDownloading, seeding, false},

		{filterSeeding, seeding, true},
		{filterSeeding, downloading, false},

		{filterCompleted, seeding, true},
		{filterCompleted, pausedDone, true}, // finished, just not running
		{filterCompleted, pausedPart, false},

		{filterStopped, pausedPart, true},
		{filterStopped, failed, true}, // an errored torrent is stopped too
		{filterStopped, downloading, false},
	}
	for _, c := range cases {
		if got := c.filter.matches(c.snap); got != c.want {
			t.Errorf("%s.matches(state=%v paused=%v done=%d/%d) = %v, want %v",
				filterNames[c.filter], c.snap.State, c.snap.Paused,
				c.snap.Completed, c.snap.TotalLength, got, c.want)
		}
	}
}

// A torrent with no metadata yet reports a zero length, and must not be
// counted as completed on the strength of 0 >= 0.
func TestCompletedIgnoresTorrentsWithoutMetadata(t *testing.T) {
	noMetadata := engine.TorrentSnapshot{State: engine.StateChecking}
	if filterCompleted.matches(noMetadata) {
		t.Error("a torrent of unknown size should not count as completed")
	}
}

func TestVisibleTorrentsAppliesTheFilter(t *testing.T) {
	m := Model{torrents: []engine.TorrentSnapshot{
		{Name: "a", State: engine.StateDownloading},
		{Name: "b", State: engine.StateSeeding, TotalLength: 1, Completed: 1},
	}}

	if got := len(m.visibleTorrents()); got != 2 {
		t.Errorf("filterAll returned %d torrents, want 2", got)
	}

	m.filter = filterSeeding
	got := m.visibleTorrents()
	if len(got) != 1 || got[0].Name != "b" {
		t.Errorf("filterSeeding returned %v", got)
	}
}

// The same pair of independent states as a torrent row: a filter can be
// the applied one, under the sidebar's cursor, or both. Drawing only the
// applied one hid the cursor exactly when it sat on the filter already
// in use.
func TestSidebarShowsTheCursorOnTheActiveFilter(t *testing.T) {
	m := testModel("a")
	m.focus = focusSidebar
	m.filter = filterAll
	m.sidebarCursor = int(filterAll)
	p := layout(120, 30)

	onActive := m.renderSidebar(p)
	if !strings.Contains(onActive, ">"+filterNames[filterAll]) {
		t.Errorf("no cursor marker on the active filter:\n%s", onActive)
	}

	// And it still marks a filter that is not the applied one.
	m.sidebarCursor = int(filterSeeding)
	onOther := m.renderSidebar(p)
	if !strings.Contains(onOther, ">"+filterNames[filterSeeding]) {
		t.Errorf("no cursor marker on an inactive filter:\n%s", onOther)
	}
	if strings.Contains(onOther, ">"+filterNames[filterAll]) {
		t.Errorf("the cursor is drawn on two filters at once:\n%s", onOther)
	}
}

// With the sidebar unfocused its cursor is not a thing the user is
// moving, so it is not drawn at all.
func TestSidebarHidesItsCursorWhenUnfocused(t *testing.T) {
	m := testModel("a")
	m.focus = focusList
	m.sidebarCursor = int(filterSeeding)

	if got := m.renderSidebar(layout(120, 30)); strings.Contains(got, ">") {
		t.Errorf("an unfocused sidebar drew a cursor:\n%s", got)
	}
}
