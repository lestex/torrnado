package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/engine"
)

// statusFilter narrows the torrent list to one lifecycle category. It is
// orthogonal to the search query -- both apply at once.
type statusFilter int

const (
	filterAll statusFilter = iota
	filterDownloading
	filterSeeding
	filterCompleted
	filterStopped
)

var filterNames = [...]string{
	filterAll:         "All",
	filterDownloading: "Downloading",
	filterSeeding:     "Seeding",
	filterCompleted:   "Completed",
	filterStopped:     "Stopped",
}

// matches reports whether t belongs in this filter.
//
// Completed is deliberately about progress rather than state: a finished
// torrent that has been paused is still completed, and a seeding one is
// too, so the category overlaps Seeding and Stopped instead of
// partitioning against them.
func (f statusFilter) matches(t engine.TorrentSnapshot) bool {
	switch f {
	case filterDownloading:
		return t.State == engine.StateDownloading
	case filterSeeding:
		return t.State == engine.StateSeeding
	case filterCompleted:
		return t.TotalLength > 0 && t.Completed >= t.TotalLength
	case filterStopped:
		// Paused rather than State == StatePaused: a paused torrent that
		// is being rechecked reports State as "checking", and it is still
		// stopped.
		return t.Paused || t.State == engine.StateError
	default:
		return true
	}
}

func (m Model) renderSidebar(p panes) string {
	w := p.sidebarContentW

	var lines []string
	lines = append(lines,
		m.styles.SidebarTitle.Render(truncate("torrnado", w)),
		"",
		m.styles.ColHeader.Render(truncate("Status", w)),
	)

	// Two independent states again, as in the list: a filter can be the
	// applied one, under the sidebar's cursor, or both. Drawing only the
	// applied one hid the cursor exactly when it was on the filter you
	// were already using.
	for i, name := range filterNames {
		active := statusFilter(i) == m.filter
		underCursor := m.focus == focusSidebar && i == m.sidebarCursor

		mark := " "
		if underCursor {
			mark = ">"
		}

		style := m.styles.SidebarItem
		switch {
		case active && underCursor:
			style = m.styles.CursorSelectedRow
		case active:
			style = m.styles.SidebarItemActive
		case underCursor:
			style = m.styles.CursorRow
		}
		lines = append(lines, style.Render(padRight(mark+name, w)))
	}

	// Daemon-wide facts that used to live in the top stats bar; they sit
	// at the bottom of the sidebar so the footer can stay one line.
	g := m.global
	stats := []string{
		m.daemonHeading(w),
		m.styles.Muted.Render(truncate(fmt.Sprintf("port: %d", g.ListenPort), w)),
		m.styles.Muted.Render(truncate(fmt.Sprintf("dht: %d", g.DhtNodes), w)),
		m.styles.Muted.Render(truncate("free: "+formatBytes(g.DiskFreeBytes), w)),
	}

	// Push the daemon block to the bottom when there's room, drop it when
	// there isn't.
	if gap := p.sidebarContentH - len(lines) - len(stats); gap >= 1 {
		for range gap {
			lines = append(lines, "")
		}
		lines = append(lines, stats...)
	}

	return strings.Join(clampLines(lines, p.sidebarContentH), "\n")
}

// daemonStatusDot is the lit indicator beside the Daemon heading.
//
// The numbers under it cannot say whether the daemon is still there: the
// port and the free space keep whatever they last were pushed, so a dead
// daemon looks exactly like a quiet one. The dot is the only thing that
// changes, which is why it is worth a colour of its own.
const daemonStatusDot = "●"

// daemonHeading is "Daemon" followed by that dot, green while the event
// stream is alive and red once it has ended.
//
// The dot trails the word rather than leading it so the heading starts in
// the same column as the values beneath it -- a leading dot indents the
// only line in the block that is not indented.
func (m Model) daemonHeading(w int) string {
	head := m.styles.ColHeader.Render(truncate("Daemon", w))

	// Two cells for the space and the dot. Dropped rather than truncated
	// when the sidebar is too narrow: half a status light says nothing.
	if w < lipgloss.Width("Daemon")+2 {
		return head
	}

	return head + " " + m.daemonDotStyle().Render(daemonStatusDot)
}

// daemonDotStyle is green while the daemon is answering and red once it
// has gone. Split out because the colour is the whole content of the dot,
// and a rendered string cannot be asserted on: lipgloss strips colour
// when it is not writing to a terminal, so under `go test` both branches
// render the same character.
func (m Model) daemonDotStyle() lipgloss.Style {
	if m.daemonDown {
		return m.styles.Error
	}
	return m.styles.Success
}
