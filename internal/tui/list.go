package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/engine"
)

// Fixed widths of the columns to the right of Name. Name itself is
// elastic: it absorbs whatever the pane has left over, which is what
// keeps the table readable at any terminal width.
const (
	// Two marker cells, because the two states are independent: a row can
	// be under the cursor, selected, or both. One cell can only show one
	// of them, and the cursor won - so a selected row under the cursor
	// looked unselected, and there was no way to see which of several
	// selected rows the cursor was on.
	colMark   = 2
	colSize   = 8
	colStatus = 13 // fits "checking 100%"
	colDown   = 12 // wide enough for "↓ 999.9MiB/s"
	colUp     = 12
	colETA    = 7

	// The progress column is a bar, a space, then the percentage. Four
	// cells for the number, so "100%" fits without the column jumping a
	// cell wider at the end of a download.
	colPercent = 4

	// The bar is optional and elastic. Below minBarWidth it is too short
	// to read as a bar at all, so the column drops to the percentage
	// alone and Name gets the room instead - a truncated name beside a
	// stub of a bar serves nobody. Above maxBarWidth it would be
	// measuring nothing anyone needs measured that finely.
	minBarWidth = 10
	maxBarWidth = 48

	// nameTarget is the Name column the bar will not encroach on: names
	// come first, and most torrent names fit in this much. The bar is
	// drawn only from what is left over, so a pane too narrow to afford
	// both shows the percentage and a longer name.
	nameTarget = 48

	// colGap is the single space between columns. Every column except the
	// marker is preceded by one, so the fixed columns - everything Name
	// does NOT get - count seven of them: one per fixed column plus
	// Name's own. Getting this off by one lets a row exceed the pane
	// width, and lipgloss then wraps it onto a second line.
	colGap = 1

	// rowLines is how many terminal rows one torrent occupies. Progress
	// used to be an underline beneath the name, which made it two.
	rowLines = 1

	// minNameWidth is the narrowest Name column worth rendering; below
	// this the trailing columns are dropped instead (see nameWidth). A
	// name cut to a dozen characters tells you nothing, and the speeds
	// are not worth that much.
	minNameWidth = 20
)

// barWidth returns the progress bar's width for a pane of the given
// content width, or 0 when the pane cannot spare the room.
//
// The bar is drawn from what is left once Name has nameTarget columns,
// and only half of that, so it grows with the terminal without ever
// taking a column away from anything else: on a pane that cannot afford
// both, the percentage stands in and Name keeps the width.
func barWidth(contentW int) int {
	surplus := contentW - fixedColumnsWith(0) - nameTarget
	if surplus <= 0 {
		return 0
	}
	bar := surplus / 2
	if bar < minBarWidth {
		return 0
	}
	return min(maxBarWidth, bar)
}

// progressWidthWith is the progress column for a given bar width: the
// bar, a space and the percentage - or just the percentage when there
// is no bar.
func progressWidthWith(bar int) int {
	if bar <= 0 {
		return colPercent
	}
	return bar + colGap + colPercent
}

// progressWidth is the progress column's width for a pane.
func progressWidth(contentW int) int {
	return progressWidthWith(barWidth(contentW))
}

// progressHeading names the column for what it actually shows, since
// "Progress" does not fit above a four-cell percentage.
func progressHeading(bar int) string {
	if bar <= 0 {
		return "%"
	}
	return "Progress"
}

// fixedColumnsWith returns everything the Name column does not get, for a
// given bar width.
func fixedColumnsWith(bar int) int {
	return colMark + 7*colGap +
		progressWidthWith(bar) +
		colSize + colStatus + colDown + colUp + colETA
}

// nameWidth returns the elastic Name column width for a pane of the given
// content width, and whether the full column set fits at all.
//
// When it doesn't, everything is dropped except Name and Progress -
// which of a torrent's numbers survives a narrow terminal is a choice,
// and how far along it is beats how fast it is going.
func nameWidth(contentW int) (int, bool) {
	w := contentW - fixedColumnsWith(barWidth(contentW))
	if w < minNameWidth {
		return max(1, contentW-colMark-colGap-progressWidth(contentW)-colGap), false
	}
	return w, true
}

func (m Model) renderListHeader(p panes) string {
	nameW, wide := nameWidth(p.listContentW)

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", colMark+colGap))
	b.WriteString(padRight("Name", nameW))
	bar := barWidth(p.listContentW)
	b.WriteString(" " + padRight(progressHeading(bar), progressWidthWith(bar)))
	if wide {
		b.WriteString(" " + padRight("Size", colSize))
		b.WriteString(" " + padRight("Status", colStatus))
		b.WriteString(" " + padRight("↓ Speed", colDown))
		b.WriteString(" " + padRight("↑ Speed", colUp))
		b.WriteString(" " + padRight("ETA", colETA))
	}
	return m.styles.ColHeader.Render(b.String())
}

func (m Model) renderListBody(p panes, visible []engine.TorrentSnapshot, paneH int) string {
	height := paneH - 1 // the header owns one row
	if len(m.torrents) == 0 {
		return m.renderEmptyList(p.listContentW, height)
	}
	if len(visible) == 0 {
		return m.styles.Muted.Render(" nothing matches status " + filterNames[m.filter])
	}

	// scrollWindow counts torrents, but each draws rowLines of them.
	start, end := scrollWindow(m.cursor, len(visible), height/rowLines)

	var lines []string
	for i := start; i < end; i++ {
		lines = append(lines, m.renderRow(p, visible[i], i == m.cursor)...)
	}
	return strings.Join(clampLines(lines, height), "\n")
}

// renderEmptyList draws what to do next, for a list with nothing in it.
//
// This is the first thing a new user sees, and "no torrents yet" on its
// own answers the question they do not have. The one they do have is what
// to do about it, so the three ways in are on screen: the palette, the
// shell, and the screen that lists everything else. Naming the keys here
// rather than expecting them to be found is the whole point - a keybind
// reference nobody knows how to open is not a reference.
func (m Model) renderEmptyList(width, height int) string {
	rows := []helpEntry{
		{":add <magnet|file|dir>", "add a torrent without leaving here"},
		{"torrnado add <magnet>", "or from a shell, interface or not"},
		{keyPair(m.keymap.Help, m.keymap.HelpAlt), "every key and command"},
	}

	keyW := 0
	for _, r := range rows {
		if w := lipgloss.Width(r.key); w > keyW {
			keyW = w
		}
	}

	lines := []string{m.styles.Muted.Render(" no torrents yet"), ""}
	for _, r := range rows {
		lines = append(lines, "   "+
			m.styles.Row.Render(padRight(r.key, keyW))+"  "+
			m.styles.Muted.Render(truncate(r.desc, width-keyW-5)))
	}
	return strings.Join(clampLines(lines, height), "\n")
}

// clampLines trims a rendered block to at most height lines.
//
// lipgloss's Height() sets a minimum, not a maximum: content taller than
// its box makes the box grow, pushing the rest of the layout down and the
// top border off the screen. Everything drawn into a pane goes through
// here so a render that miscounts cannot wreck the frame.
func clampLines(lines []string, height int) []string {
	if height > 0 && len(lines) > height {
		return lines[:height]
	}
	return lines
}

// renderRow draws one torrent as a single line of columns.
func (m Model) renderRow(p panes, t engine.TorrentSnapshot, isCursor bool) []string {
	nameW, wide := nameWidth(p.listContentW)

	selected := m.selected[t.ID]

	cursorMark, selectMark := " ", " "
	if isCursor {
		cursorMark = ">"
	}
	if selected {
		selectMark = "*"
	}

	var b strings.Builder
	b.WriteString(cursorMark)
	b.WriteString(selectMark)
	b.WriteString(" ")
	b.WriteString(padRight(truncate(t.Name, nameW), nameW))
	b.WriteString(" " + progressCell(t.Progress, barWidth(p.listContentW)))
	// Every column is left-aligned, headings included, so a heading sits
	// directly above the values it names. Numbers lose the decimal-point
	// alignment that ragged-right columns give them; a table that reads
	// as one grid is worth more here than digits that line up.
	if wide {
		b.WriteString(" " + padRight(formatBytes(t.TotalLength), colSize))
		b.WriteString(" " + padRight(t.StatusText(), colStatus))
		b.WriteString(" " + padRight(rateCell("↓ ", t.DownloadBPS), colDown))
		b.WriteString(" " + padRight(rateCell("↑ ", t.UploadBPS), colUp))
		b.WriteString(" " + padRight(etaCell(t), colETA))
	}

	// The whole line takes one style, so a highlight cannot be broken
	// partway through by a nested style's ANSI reset.
	// The styles carry the same pair of states as the markers, and had
	// the same problem: a selected row under the cursor showed the
	// cursor's color and lost the selection's background entirely.
	style := m.styles.Row
	switch {
	case isCursor && selected:
		style = m.styles.CursorSelectedRow
	case isCursor:
		style = m.styles.CursorRow
	case selected:
		style = m.styles.SelectedRow
	}

	return []string{style.Render(b.String())}
}

// progressCell renders a fraction as a bar of barW cells followed by its
// percentage, or as the percentage alone when barW is 0.
//
// Drawn in one color rather than two, unlike the underline this
// replaced. A row is rendered with a single style so that a cursor or
// selection highlight runs the whole way across it; styling the bar's
// halves separately would end that style mid-row and leave a hole in the
// highlight. The glyphs carry the bar on their own - "━" is filled, "─"
// is track.
func progressCell(frac float64, barW int) string {
	// Clamped once, so the bar and the number cannot disagree: a
	// fraction over 1 (bytes received but not yet verified are counted)
	// would otherwise print a full bar beside "120%", and a percentage
	// wide enough to push the columns out of line.
	if frac > 1 {
		frac = 1
	}
	if frac < 0 {
		frac = 0
	}
	pct := padRight(percentCell(frac)+"%", colPercent)
	if barW <= 0 {
		return pct
	}
	filled := int(frac * float64(barW))
	bar := strings.Repeat("━", filled) + strings.Repeat("─", barW-filled)
	return bar + " " + pct
}

// percentCell renders a completion fraction as a whole number.
//
// It floors rather than rounds: at 99.6% the nearest whole number is 100,
// and a row that reads 100% beside a bar with a gap still in it looks
// like a bug.
func percentCell(frac float64) string {
	return fmt.Sprintf("%d", int(math.Floor(frac*100)))
}

// rateCell renders a transfer rate, blanking it at zero so idle rows stay
// quiet rather than showing a column of "0 B/s".
func rateCell(prefix string, bps float64) string {
	if bps < 1 {
		return ""
	}
	return prefix + formatRate(bps)
}

func etaCell(t engine.TorrentSnapshot) string {
	if t.ETA <= 0 {
		return ""
	}
	return formatETA(t.ETA)
}

// scrollWindow returns the [start,end) bounds of a height-row viewport
// that keeps cursor visible within n rows.
//
// The cursor is kept near the middle rather than at an edge, so moving
// through a long list shows what is coming rather than only what has
// passed.
func scrollWindow(cursor, n, height int) (int, int) {
	if height <= 0 || n <= height {
		return 0, n
	}
	start := cursor - height/2
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > n {
		end = n
		start = end - height
	}
	return start, end
}
