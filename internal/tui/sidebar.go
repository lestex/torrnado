package tui

import (
	"fmt"
	"strings"

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

	for i, name := range filterNames {
		label := " " + name
		if statusFilter(i) == m.filter {
			lines = append(lines, m.styles.SidebarItemActive.Render(padRight(label, w)))
			continue
		}
		lines = append(lines, m.styles.SidebarItem.Render(padRight(label, w)))
	}

	// Daemon-wide facts that used to live in the top stats bar; they sit
	// at the bottom of the sidebar so the footer can stay one line.
	g := m.global
	stats := []string{
		m.styles.ColHeader.Render(truncate("Daemon", w)),
		m.styles.Muted.Render(truncate(fmt.Sprintf(" port %d", g.ListenPort), w)),
		m.styles.Muted.Render(truncate(fmt.Sprintf(" dht %d", g.DhtNodes), w)),
		m.styles.Muted.Render(truncate(" free "+formatBytes(g.DiskFreeBytes), w)),
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
