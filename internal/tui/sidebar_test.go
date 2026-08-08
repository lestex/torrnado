package tui

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/theme"
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
	blocked := engine.TorrentSnapshot{State: engine.StateBlocked, TotalLength: 100, Completed: 10}

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
		{filterStopped, failed, true},  // an errored torrent is stopped too
		{filterStopped, blocked, true}, // so is one held by the VPN guard
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

// The daemon block is a label-and-value list, so it reads as one when
// every line starts in the same column and every label ends the same way.
func TestDaemonStatsAreLabelledAndFlushLeft(t *testing.T) {
	m := testModel("a")
	m.global = engine.GlobalStats{ListenPort: 51413, DhtNodes: 12, DiskFreeBytes: 1 << 30}

	got := m.renderSidebar(layout(120, 30))
	for _, want := range []string{"port: 51413", "dht: 12", "free: 1.0GiB"} {
		if !strings.Contains(got, "\n"+want) {
			t.Errorf("no line beginning %q:\n%s", want, got)
		}
	}
}

// The guard is why every torrent is sitting still, so the sidebar has to
// say so - and say nothing at all when there is no guard, since a daemon
// that was never asked to check has nothing to report.
func TestTheSidebarReportsTheVPNGuard(t *testing.T) {
	m := testModel("a")
	p := layout(120, 30)

	if got := m.renderSidebar(p); strings.Contains(got, "vpn") {
		t.Errorf("a daemon with no VPN guard drew a vpn line:\n%s", got)
	}

	m.global = engine.GlobalStats{VPNRequired: true, VPNActive: true, VPNInterface: "utun4"}
	if got := m.renderSidebar(p); !strings.Contains(got, "vpn: utun4") {
		t.Errorf("an active guard does not name the interface:\n%s", got)
	}

	m.global = engine.GlobalStats{VPNRequired: true}
	if got := m.renderSidebar(p); !strings.Contains(got, "vpn: blocked") {
		t.Errorf("a guard holding transfers says nothing:\n%s", got)
	}
}

// The dot is the only part of the block that can tell the truth about a
// dead daemon - the port and the free space keep their last value - so
// it has to change when the stream ends.
func TestTheDaemonDotReportsALostConnection(t *testing.T) {
	m := testModel("a")
	th, err := theme.Load("dracula", t.TempDir())
	if err != nil {
		t.Fatalf("loading a built-in theme: %v", err)
	}
	m.styles = newStyles(th)

	if !strings.Contains(m.renderSidebar(layout(120, 30)), daemonStatusDot) {
		t.Error("the daemon heading carries no status dot")
	}

	// The color, not the character, is what the dot says - and it has
	// to come from the theme, so a picked theme recolors it too.
	if got := m.daemonDotStyle().GetForeground(); got != th.Success {
		t.Errorf("a live daemon's dot is %v, want the theme's success color %v", got, th.Success)
	}

	m.daemonDown = true
	if got := m.daemonDotStyle().GetForeground(); got != th.Error {
		t.Errorf("a lost daemon's dot is %v, want the theme's error color %v", got, th.Error)
	}
}

// Half a status light says nothing, so a sidebar with no room for it
// drops the dot rather than truncating the heading to fit.
func TestTheDaemonDotIsDroppedWhenThereIsNoRoom(t *testing.T) {
	m := testModel("a")

	if got := m.daemonHeading(7); strings.Contains(got, daemonStatusDot) {
		t.Errorf("the dot was drawn in 7 columns: %q", got)
	}
	if got := m.daemonHeading(8); !strings.Contains(got, daemonStatusDot) {
		t.Errorf("the dot was dropped in 8 columns: %q", got)
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
