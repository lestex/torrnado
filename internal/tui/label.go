package tui

import (
	"sort"
	"strings"

	"github.com/lestex/torrnado/internal/engine"
)

// labelUse is one label and how many torrents carry it.
type labelUse struct {
	name  string
	count int
}

// labelsInUse returns the labels some torrent is filed under, most-used
// first and alphabetical within a count.
//
// Derived from the torrent list on every call rather than held as state,
// because that is what makes a label need no lifecycle: there is nothing
// to create, nothing to delete, and no way for the sidebar to list a
// label that nothing is filed under. It also means the counts cannot go
// stale against the list they describe.
//
// Most-used first because that is the order a sidebar with more labels
// than rows should truncate in: the ones you use least are the ones you
// can most afford to have elided.
func (m Model) labelsInUse() []labelUse {
	counts := map[string]int{}
	for _, t := range m.torrents {
		if t.Label != "" {
			counts[t.Label]++
		}
	}

	out := make([]labelUse, 0, len(counts))
	for name, n := range counts {
		out = append(out, labelUse{name: name, count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].name < out[j].name
	})
	return out
}

// sidebarEntry is one selectable row: a status filter, or a label.
type sidebarEntry struct {
	label  string // empty for a status row
	status statusFilter
	count  int // torrents carrying the label; unused for a status row
}

func (e sidebarEntry) isLabel() bool { return e.label != "" }

// sidebarEntries is every row the sidebar cursor can land on, in the
// order they are drawn: the status filters, then the labels in use.
//
// One flat list because the sidebar highlights exactly one entry. Two
// cursors - one for statuses, one for labels - would need a second
// highlight to say which of them was live.
func (m Model) sidebarEntries() []sidebarEntry {
	out := make([]sidebarEntry, 0, len(filterNames)+4)
	for i := range filterNames {
		out = append(out, sidebarEntry{status: statusFilter(i)})
	}
	for _, l := range m.labelsInUse() {
		out = append(out, sidebarEntry{label: l.name, count: l.count})
	}
	return out
}

// applyEntry selects one sidebar row.
//
// Selecting a label widens the status filter back to All, and selecting
// a status clears the label. The two could compose, but the sidebar can
// only show one selection, and a filter that is on while nothing says so
// is how "where did my torrents go" happens.
func (m Model) applyEntry(e sidebarEntry) Model {
	if e.isLabel() {
		m.labelFilter = e.label
		m.filter = filterAll
		return m
	}
	m.labelFilter = ""
	m.filter = e.status
	return m
}

// currentSidebarIndex is which entry the current filter corresponds to,
// used to put the cursor back where the view says it is after the label
// list has changed underneath it.
func (m Model) currentSidebarIndex() int {
	entries := m.sidebarEntries()
	for i, e := range entries {
		if m.labelFilter != "" {
			if e.isLabel() && e.label == m.labelFilter {
				return i
			}
			continue
		}
		if !e.isLabel() && e.status == m.filter {
			return i
		}
	}
	return 0
}

// dropVanishedLabelFilter falls back to All when the label being filtered
// on stops existing.
//
// A label exists only while a torrent carries it, so relabelling or
// removing the last one takes the filter with it - and leaving it applied
// strands the user in front of an empty list they did not ask for, with
// the only way out being a filter for something that is gone. Falling
// back is the one case where the view changes without a keypress, which
// is right: the thing it was showing no longer exists.
func (m Model) dropVanishedLabelFilter() Model {
	if m.labelFilter == "" {
		return m
	}
	for _, l := range m.labelsInUse() {
		if l.name == m.labelFilter {
			return m
		}
	}
	m.labelFilter = ""
	m.filter = filterAll
	m.sidebarCursor = 0
	return m
}

// revealJustAdded drops whichever filters are hiding a torrent that was
// just added, and names the ones it dropped.
//
// Adding is the one action whose whole purpose is to put something in the
// list, so reporting success while the list does not change is the wrong
// answer however correct the filter was. The label case was the one that
// could never resolve itself - a new torrent has no label and nothing was
// going to give it one - but none of the three resolve *promptly*: a
// torrent added under the Completed filter is hidden for as long as it
// takes to download, and one that does not match the search is hidden
// until the search is cleared by hand.
//
// This runs on a snapshot rather than on the add's reply because the
// question needs the torrents themselves: the name decides the search,
// the label decides the label filter, and the state decides the status
// filter. Ids not in the snapshot yet are left pending for the next one.
func (m Model) revealJustAdded() (Model, []string) {
	if len(m.pendingReveal) == 0 {
		return m, nil
	}

	byID := make(map[engine.TorrentID]engine.TorrentSnapshot, len(m.torrents))
	for _, t := range m.torrents {
		byID[t.ID] = t
	}

	var seen bool
	var hiddenByStatus, hiddenByLabel, hiddenBySearch bool
	query := strings.ToLower(strings.TrimSpace(m.searchQuery))
	for _, id := range m.pendingReveal {
		t, ok := byID[id]
		if !ok {
			continue
		}
		seen = true
		if !m.filter.matches(t) {
			hiddenByStatus = true
		}
		if m.labelFilter != "" && t.Label != m.labelFilter {
			hiddenByLabel = true
		}
		if query != "" && !strings.Contains(strings.ToLower(t.Name), query) {
			hiddenBySearch = true
		}
	}
	if !seen {
		return m, nil // not in this snapshot; ask again on the next one
	}
	m.pendingReveal = nil

	var cleared []string
	if hiddenByLabel {
		cleared = append(cleared, "the "+m.labelFilter+" label")
		m.labelFilter = ""
	}
	if hiddenByStatus {
		cleared = append(cleared, "the "+strings.ToLower(filterNames[m.filter])+" filter")
		m.filter = filterAll
	}
	if hiddenBySearch {
		cleared = append(cleared, "the search")
		m.searchQuery = ""
	}
	if len(cleared) > 0 {
		m.sidebarCursor = m.currentSidebarIndex()
	}
	return m, cleared
}

// joinClauses renders a list as "a", "a and b", "a, b and c".
func joinClauses(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}
