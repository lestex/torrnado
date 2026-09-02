package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/engine"
)

// statusFilter narrows the torrent list to one lifecycle category. It is
// orthogonal to the search query - both apply at once.
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
		// stopped. The guards belong here too - a torrent held off-VPN or
		// by a full disk is not running, and this is the filter someone
		// reaches for to find out what is not.
		return t.Paused || t.State == engine.StateError ||
			t.State == engine.StateBlocked || t.State == engine.StateLowDisk
	default:
		return true
	}
}

func (m Model) renderSidebar(p panes) string {
	w := p.sidebarContentW

	var lines []string

	// No mark here: the sidebar is 20 columns that torrent names and
	// filters compete for, and the help screen already carries it.
	lines = append(lines,
		m.styles.SidebarTitle.Render(truncate("torrnado", w)),
		"",
		m.styles.ColHeader.Render(truncate("Status", w)),
	)

	// Two independent states again, as in the list: a filter can be the
	// applied one, under the sidebar's cursor, or both. Drawing only the
	// applied one hid the cursor exactly when it was on the filter you
	// were already using.
	entries := m.sidebarEntries()
	for i := range filterNames {
		lines = append(lines, m.renderSidebarRow(entries[i], i, filterNames[i], w))
	}

	// Labels, when anything is filed under one. The heading is drawn only
	// then: an empty "Labels" on every daemon would be three rows of the
	// sidebar spent saying that a feature exists.
	if labels := entries[len(filterNames):]; len(labels) > 0 {
		// What is left after the status block above, the daemon block
		// below, and this section's own blank line and heading. Labels
		// are elided to fit rather than capped at some fixed number:
		// how many fit is a property of the terminal, not of the user.
		// The +1 is the blank row that separates the daemon block from
		// what is above it; without reserving it, the block is dropped
		// entirely rather than moving down.
		room := p.sidebarContentH - len(lines) - (m.daemonBlockRows() + 1) - 2
		shown := len(labels)
		if room < shown {
			// One row goes to saying how many are not being shown, so a
			// filter that exists cannot be invisible with nothing to
			// suggest looking for it.
			shown = max(room-1, 0)
		}
		if shown > 0 {
			lines = append(lines, "", m.styles.ColHeader.Render(truncate("Labels", w)))
			for j, l := range labels[:shown] {
				lines = append(lines, m.renderSidebarRow(l, len(filterNames)+j, l.label, w))
			}
			if hidden := len(labels) - shown; hidden > 0 {
				lines = append(lines,
					m.styles.Muted.Render(truncate(fmt.Sprintf(" +%d more", hidden), w)))
			}
		}
	}

	// Daemon-wide facts that used to live in the top stats bar; they sit
	// at the bottom of the sidebar so the footer can stay one line.
	g := m.global
	stats := []string{m.daemonHeading(w)}
	if line, ok := m.vpnLine(w); ok {
		stats = append(stats, line)
	}
	stats = append(stats,
		m.styles.Muted.Render(truncate(fmt.Sprintf("port: %d", g.ListenPort), w)),
		m.styles.Muted.Render(truncate(fmt.Sprintf("dht: %d", g.DhtNodes), w)),
		m.diskLine(w),
	)

	// Push the daemon block to the bottom when there's room, drop it when
	// there isn't.
	//
	// The label section above has already reserved these rows, so the
	// two cannot both claim the same space.
	if gap := p.sidebarContentH - len(lines) - len(stats); gap >= 1 {
		for range gap {
			lines = append(lines, "")
		}
		lines = append(lines, stats...)
	}

	return strings.Join(clampLines(lines, p.sidebarContentH), "\n")
}

// renderSidebarRow draws one selectable row - a status filter or a label
// - in whichever of the four states it is in.
//
// Two independent states again, as in the list: a row can be the applied
// one, under the sidebar's cursor, or both. Drawing only the applied one
// hid the cursor exactly when it was on the filter you were already
// using.
func (m Model) renderSidebarRow(e sidebarEntry, i int, name string, w int) string {
	active := m.entryIsActive(e)
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
	return style.Render(padRight(mark+name, w))
}

// entryIsActive reports whether this row is the filter currently applied.
func (m Model) entryIsActive(e sidebarEntry) bool {
	if e.isLabel() {
		return m.labelFilter == e.label
	}
	return m.labelFilter == "" && e.status == m.filter
}

// daemonBlockRows is how many rows the daemon block at the bottom wants,
// so the label section can leave them alone rather than pushing the
// port/dht/disk readings off the bottom of the pane.
func (m Model) daemonBlockRows() int {
	rows := 4 // heading, port, dht, disk
	if _, ok := m.vpnLine(0); ok {
		rows++
	}
	return rows
}

// diskLine reports free space, in the error style when the free-space
// guard is what is holding every transfer.
//
// Always drawn, unlike vpnLine: free space is worth knowing whether or
// not a floor is configured, and a number on its own says nothing
// alarming. What changes when the guard bites is the colour, so the
// answer to "why is nothing moving" is in the same place as the reading
// that explains it.
func (m Model) diskLine(w int) string {
	free := formatBytes(m.global.DiskFreeBytes)
	if m.global.DiskLow {
		return m.styles.Error.Render(truncate("free: "+free+" - held", w))
	}
	return m.styles.Muted.Render(truncate("free: "+free, w))
}

// vpnLine reports the VPN guard, and whether there is anything to report.
//
// Drawn only when the guard is switched on. A "vpn: none" on every daemon
// would read as a warning to the many people who never asked for one, and
// the daemon does not even run the check unless it was asked to - so
// there would be nothing behind the word.
func (m Model) vpnLine(w int) (string, bool) {
	if !m.global.VPNRequired {
		return "", false
	}
	if !m.global.VPNActive {
		// The one line in this block that is not muted: it is the reason
		// every torrent is sitting still.
		return m.styles.Error.Render(truncate("vpn: blocked", w)), true
	}
	// The interface name, because "which VPN am I on" is the question
	// after "am I on one" - and a daemon that thinks it is protected by
	// the wrong interface is worth being able to see.
	iface := m.global.VPNInterface
	if iface == "" {
		iface = "on"
	}
	return m.styles.Success.Render(truncate("vpn: "+iface, w)), true
}

// daemonStatusDot is the lit indicator beside the Daemon heading.
//
// The numbers under it cannot say whether the daemon is still there: the
// port and the free space keep whatever they last were pushed, so a dead
// daemon looks exactly like a quiet one. The dot is the only thing that
// changes, which is why it is worth a color of its own.
const daemonStatusDot = "●"

// daemonHeading is "Daemon" followed by that dot, green while the event
// stream is alive and red once it has ended.
//
// The dot trails the word rather than leading it so the heading starts in
// the same column as the values beneath it - a leading dot indents the
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
// has gone. Split out because the color is the whole content of the dot,
// and a rendered string cannot be asserted on: lipgloss strips color
// when it is not writing to a terminal, so under `go test` both branches
// render the same character.
func (m Model) daemonDotStyle() lipgloss.Style {
	if m.daemonDown {
		return m.styles.Error
	}
	return m.styles.Success
}
