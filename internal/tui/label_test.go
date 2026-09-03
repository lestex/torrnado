package tui

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

func labelled(pairs ...string) Model {
	m := testModel()
	for i := 0; i < len(pairs); i += 2 {
		m.torrents = append(m.torrents, engine.TorrentSnapshot{
			ID:    engine.TorrentID(pairs[i]),
			Name:  pairs[i],
			Label: pairs[i+1],
			State: engine.StateDownloading,
		})
	}
	return m
}

// The sidebar can only list labels something is filed under, which is
// what makes a label need no lifecycle - and the order matters, because
// it is the order the list is truncated in when it does not fit.
func TestLabelsInUseAreCountedAndOrdered(t *testing.T) {
	m := labelled("a", "tv", "b", "tv", "c", "films", "d", "", "e", "audio")

	got := m.labelsInUse()
	if len(got) != 3 {
		t.Fatalf("got %d labels, want 3 (the unlabelled one must not count)", len(got))
	}
	if got[0].name != "tv" || got[0].count != 2 {
		t.Errorf("first = %+v, want tv with 2 - most used comes first", got[0])
	}
	// audio and films both have one, so they order alphabetically.
	if got[1].name != "audio" || got[2].name != "films" {
		t.Errorf("ties = %q, %q; want audio then films", got[1].name, got[2].name)
	}
}

func TestSelectingALabelFiltersTheList(t *testing.T) {
	m := labelled("a", "tv", "b", "films", "c", "")
	m.focus = focusSidebar

	// Past the five status filters is the first label.
	next, _ := m.selectSidebar(len(filterNames))
	got := next.(Model)

	if got.labelFilter != "films" && got.labelFilter != "tv" {
		t.Fatalf("labelFilter = %q, want a label", got.labelFilter)
	}
	vis := got.visibleTorrents()
	if len(vis) != 1 || vis[0].Label != got.labelFilter {
		t.Errorf("visible = %d torrents, want only the one labelled %q", len(vis), got.labelFilter)
	}
}

// The two filters are shown by one highlight, so selecting either has to
// clear the other or the sidebar would be lying about what is applied.
func TestAStatusAndALabelFilterAreExclusive(t *testing.T) {
	m := labelled("a", "tv", "b", "")
	next, _ := m.selectSidebar(len(filterNames)) // a label
	m = next.(Model)
	if m.labelFilter == "" {
		t.Fatal("no label was selected")
	}

	next, _ = m.selectSidebar(int(filterDownloading))
	m = next.(Model)
	if m.labelFilter != "" {
		t.Errorf("labelFilter = %q, want it cleared by picking a status", m.labelFilter)
	}
	if m.filter != filterDownloading {
		t.Errorf("filter = %v, want filterDownloading", m.filter)
	}
}

// Nothing filed under a label means no heading: an empty "Labels" would
// spend three sidebar rows saying a feature exists.
func TestTheLabelsHeadingIsOnlyDrawnWhenThereAreLabels(t *testing.T) {
	p := layout(120, 40)

	plain := testModel("a")
	if strings.Contains(plain.renderSidebar(p), "Labels") {
		t.Error("the Labels heading was drawn with no labels in use")
	}

	m := labelled("a", "tv")
	out := m.renderSidebar(p)
	if !strings.Contains(out, "Labels") || !strings.Contains(out, "tv") {
		t.Errorf("the label is missing from the sidebar:\n%s", out)
	}
}

// More labels than rows are elided rather than clipped, because clipping
// hides a filter with nothing to suggest looking for it.
func TestTooManyLabelsAreElidedWithACount(t *testing.T) {
	var pairs []string
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		pairs = append(pairs, n, "label-"+n)
	}
	m := labelled(pairs...)

	out := m.renderSidebar(layout(120, minHeight+6))

	if !strings.Contains(out, "more") {
		t.Errorf("no overflow marker, so labels were silently clipped:\n%s", out)
	}
	// The daemon block must survive: labels must not push it off.
	if !strings.Contains(out, "dht:") {
		t.Errorf("the daemon block was pushed out by the labels:\n%s", out)
	}
}

// A label exists only while a torrent carries it, so relabelling the last
// one takes the filter with it. Leaving the filter applied strands the
// user in front of an empty list, with the only way out being a filter
// for something that no longer exists.
func TestTheFilterFallsBackWhenItsLabelStopsExisting(t *testing.T) {
	m := labelled("a", "tv", "b", "films")
	next, _ := m.selectSidebar(m.currentSidebarIndex()) // start on a status
	m = next.(Model)
	m.labelFilter = "tv"

	// The next snapshot has nothing filed under "tv" any more.
	m.torrents[0].Label = "films"
	m = m.dropVanishedLabelFilter()

	if m.labelFilter != "" {
		t.Errorf("labelFilter = %q, want it dropped", m.labelFilter)
	}
	if m.filter != filterAll {
		t.Errorf("filter = %v, want filterAll", m.filter)
	}
	if len(m.visibleTorrents()) != 2 {
		t.Errorf("visible = %d, want both torrents back", len(m.visibleTorrents()))
	}
}

// But a label that is still in use must survive a snapshot, or the filter
// would reset itself every tick.
func TestALiveLabelFilterSurvivesASnapshot(t *testing.T) {
	m := labelled("a", "tv", "b", "films")
	m.labelFilter = "tv"

	if got := m.dropVanishedLabelFilter(); got.labelFilter != "tv" {
		t.Errorf("labelFilter = %q, want it kept", got.labelFilter)
	}
}

// addThenSnapshot performs the two halves of an add: the reply, then the
// snapshot that carries the new torrent. The filters cannot be judged on
// the reply alone - it is the torrent itself that decides which of them
// hide it.
func addThenSnapshot(m Model, added engine.TorrentSnapshot) Model {
	next, _ := m.Update(torrentsAddedMsg{
		text: "added 1 torrent(s)",
		ids:  []engine.TorrentID{added.ID},
	})
	m = next.(Model)
	next, _ = m.Update(engineEventMsg{Torrents: append(m.torrents, added)})
	return next.(Model)
}

// Adding a torrent used to report success and change nothing on screen
// whenever a filter was hiding it. Restarting the interface was the only
// way to see what had been added.
//
// The label case could never resolve itself - a new torrent has no label
// and nothing was going to give it one - but none of the three resolve
// promptly: a torrent added under Completed is hidden until it finishes,
// and one that does not match the search is hidden until the search is
// cleared by hand.
func TestAddingClearsWhicheverFilterHidesTheResult(t *testing.T) {
	fresh := engine.TorrentSnapshot{
		ID: "new", Name: "Something New", State: engine.StateChecking,
	}

	t.Run("label", func(t *testing.T) {
		m := labelled("a", "tv")
		m.labelFilter = "tv"

		got := addThenSnapshot(m, fresh)

		if got.labelFilter != "" {
			t.Errorf("labelFilter = %q, want it cleared", got.labelFilter)
		}
		if !strings.Contains(got.status, "tv") {
			t.Errorf("status = %q, want it to name the label it cleared", got.status)
		}
	})

	t.Run("status", func(t *testing.T) {
		m := labelled("a", "")
		m.filter = filterCompleted

		got := addThenSnapshot(m, fresh)

		if got.filter != filterAll {
			t.Errorf("filter = %v, want filterAll", got.filter)
		}
		if !strings.Contains(got.status, "completed") {
			t.Errorf("status = %q, want it to name the filter it cleared", got.status)
		}
	})

	t.Run("search", func(t *testing.T) {
		m := labelled("a", "")
		m.searchQuery = "nothing like it"

		got := addThenSnapshot(m, fresh)

		if got.searchQuery != "" {
			t.Errorf("searchQuery = %q, want it cleared", got.searchQuery)
		}
		if !strings.Contains(got.status, "search") {
			t.Errorf("status = %q, want it to say the search was cleared", got.status)
		}
	})

	t.Run("all three at once", func(t *testing.T) {
		m := labelled("a", "tv")
		m.labelFilter = "tv"
		m.filter = filterCompleted
		m.searchQuery = "nothing like it"

		got := addThenSnapshot(m, fresh)

		if got.labelFilter != "" || got.filter != filterAll || got.searchQuery != "" {
			t.Errorf("not everything was cleared: label=%q filter=%v search=%q",
				got.labelFilter, got.filter, got.searchQuery)
		}
		if len(got.visibleTorrents()) == 0 {
			t.Error("the torrent that was just added is still not visible")
		}
	})
}

// A filter that is not hiding the new torrent must be left alone: an add
// is not a reason to throw away a search that is working.
func TestAddingKeepsFiltersThatAlreadyShowTheResult(t *testing.T) {
	m := labelled("a", "")
	m.searchQuery = "ubuntu"
	m.filter = filterAll

	got := addThenSnapshot(m, engine.TorrentSnapshot{
		ID: "new", Name: "Ubuntu 26.04", State: engine.StateChecking,
	})

	if got.searchQuery != "ubuntu" {
		t.Errorf("searchQuery = %q, want it kept - it already matched", got.searchQuery)
	}
	if got.status != "added 1 torrent(s)" {
		t.Errorf("status = %q, want it unchanged", got.status)
	}
}

// The ids are held until a snapshot actually carries them, so an add is
// not forgotten because the first tick arrived too early.
func TestTheRevealWaitsForTheSnapshotThatCarriesTheTorrent(t *testing.T) {
	m := labelled("a", "tv")
	m.labelFilter = "tv"

	next, _ := m.Update(torrentsAddedMsg{text: "added 1 torrent(s)", ids: []engine.TorrentID{"new"}})
	m = next.(Model)

	// A snapshot without it changes nothing and keeps the id pending.
	next, _ = m.Update(engineEventMsg{Torrents: m.torrents})
	m = next.(Model)
	if m.labelFilter != "tv" {
		t.Error("the filter was cleared before the torrent had even arrived")
	}
	if len(m.pendingReveal) != 1 {
		t.Fatalf("pendingReveal = %v, want the id still waiting", m.pendingReveal)
	}

	// And the one that carries it does the work.
	next, _ = m.Update(engineEventMsg{Torrents: append(m.torrents,
		engine.TorrentSnapshot{ID: "new", Name: "New", State: engine.StateChecking})})
	if got := next.(Model); got.labelFilter != "" {
		t.Errorf("labelFilter = %q, want it cleared once the torrent arrived", got.labelFilter)
	}
}

// Escape peels one layer at a time, and a label filter is a layer. It was
// not in the chain, so with one applied and nothing else left to clear,
// escape did nothing at all: the list stayed showing one label's worth of
// torrents and there was no way back to All except the sidebar.
func TestEscapeClearsALabelFilter(t *testing.T) {
	m := labelled("a", "tv", "b", "films", "c", "")
	m.labelFilter = "tv"
	m.focus = focusList

	if len(m.visibleTorrents()) != 1 {
		t.Fatalf("setup: %d visible, want 1", len(m.visibleTorrents()))
	}

	m = press(m, "esc")

	if m.labelFilter != "" {
		t.Errorf("labelFilter = %q, want it cleared", m.labelFilter)
	}
	if n := len(m.visibleTorrents()); n != 3 {
		t.Errorf("visible = %d, want all 3 back", n)
	}
}

// And it stays one layer at a time: a selection is cleared first, so a
// single escape must not throw away both.
func TestEscapePeelsTheSelectionBeforeTheLabelFilter(t *testing.T) {
	m := labelled("a", "tv", "b", "")
	m.labelFilter = "tv"
	m.focus = focusList
	m.selected = map[engine.TorrentID]bool{"a": true}

	m = press(m, "esc")
	if m.labelFilter != "tv" {
		t.Error("the label filter went with the selection; escape peels one layer")
	}
	if len(m.selected) != 0 {
		t.Error("the selection was not cleared first")
	}

	m = press(m, "esc")
	if m.labelFilter != "" {
		t.Errorf("labelFilter = %q, want the second escape to clear it", m.labelFilter)
	}
}

// Marks belong to the view they were made in. The batch operations act on
// the selection rather than on what is drawn, so a mark left armed under
// a filter that hides it means x or D reaching torrents that are not on
// screen.
func TestChangingTheFilterClearsTheSelection(t *testing.T) {
	for _, tc := range []struct {
		name string
		to   int
	}{
		{"to a status", int(filterDownloading)},
		{"to a label", len(filterNames)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := labelled("a", "tv", "b", "films", "c", "")
			m.selected = map[engine.TorrentID]bool{"a": true, "b": true}

			next, _ := m.selectSidebar(tc.to)
			got := next.(Model)

			if len(got.selected) != 0 {
				t.Errorf("selected = %v, want it cleared with the filter change", got.selected)
			}
		})
	}
}

// Re-picking the filter already applied is not a reason to throw a
// selection away - moving the cursor back onto All and pressing nothing
// else should not undo the marks you just made.
func TestReselectingTheSameFilterKeepsTheSelection(t *testing.T) {
	m := labelled("a", "tv", "b", "")
	m.selected = map[engine.TorrentID]bool{"a": true}
	m.filter = filterAll
	m.labelFilter = ""

	next, _ := m.selectSidebar(int(filterAll))
	got := next.(Model)

	if len(got.selected) != 1 {
		t.Errorf("selected = %v, want it kept - the filter did not change", got.selected)
	}
}

// Widening on its own is not a filter change the user made, and it cannot
// hide anything: the reveal after an add and the fallback when a label
// stops existing both leave a selection alone.
func TestAWideningFallbackKeepsTheSelection(t *testing.T) {
	m := labelled("a", "tv", "b", "films")
	m.labelFilter = "tv"
	m.selected = map[engine.TorrentID]bool{"a": true}

	m.torrents[0].Label = "films" // nothing is filed under tv any more
	m = m.dropVanishedLabelFilter()

	if len(m.selected) != 1 {
		t.Errorf("selected = %v, want it kept: widening hides nothing", m.selected)
	}
}
