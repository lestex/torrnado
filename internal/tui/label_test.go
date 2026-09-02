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
