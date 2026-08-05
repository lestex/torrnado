package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/lestex/torrnado/internal/engine"
)

// Fixed widths of the columns to the right of Name. Name itself is
// elastic: it absorbs whatever the pane has left over, which is what
// keeps the table readable at any terminal width.
const (
	colMark   = 1 // cursor ">" / selection "*" marker
	colSize   = 8
	colStatus = 13 // fits "checking 100%"
	colDown   = 12 // wide enough for "↓ 999.9MiB/s"
	colUp     = 12
	colETA    = 7

	// The progress column: a bar, a space, then the percentage. Sized so
	// "100%" fits without the column jumping a cell wider at the end of a
	// download.
	barWidth    = 16
	colPercent  = 4
	colProgress = barWidth + colGap + colPercent

	// colGap is the single space between columns. Every column except the
	// marker is preceded by one, so fixedColumns -- everything the Name
	// column does NOT get -- counts seven of them: one per fixed column
	// plus Name's own. Getting this off by one lets a row exceed the pane
	// width, and lipgloss then wraps it onto a second line.
	colGap       = 1
	fixedColumns = colMark + 7*colGap +
		colProgress + colSize + colStatus + colDown + colUp + colETA

	// rowLines is how many terminal rows one torrent occupies. Progress
	// used to be an underline beneath the name, which made it two.
	rowLines = 1

	// minNameWidth is the narrowest Name column worth rendering; below
	// this the trailing columns are dropped instead (see nameWidth).
	minNameWidth = 12
)

// nameWidth returns the elastic Name column width for a pane of the given
// content width, and whether the full column set fits at all.
//
// When it doesn't, everything is dropped except Name and Progress --
// which of a torrent's numbers survives a narrow terminal is a choice,
// and how far along it is beats how fast it is going.
func nameWidth(contentW int) (int, bool) {
	w := contentW - fixedColumns
	if w < minNameWidth {
		return max(1, contentW-colMark-colGap-colProgress-colGap), false
	}
	return w, true
}

func (m Model) renderListHeader(p panes) string {
	nameW, wide := nameWidth(p.listContentW)

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", colMark+colGap))
	b.WriteString(padRight("Name", nameW))
	b.WriteString(" " + padRight("Progress", colProgress))
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
		return m.styles.Muted.Render(" no torrents yet -- add one with `torrnado add`")
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

	mark := " "
	switch {
	case isCursor:
		mark = ">"
	case m.selected[t.ID]:
		mark = "*"
	}

	var b strings.Builder
	b.WriteString(mark)
	b.WriteString(" ")
	b.WriteString(padRight(truncate(t.Name, nameW), nameW))
	b.WriteString(" " + progressCell(t.Progress))
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
	style := m.styles.Row
	switch {
	case isCursor:
		style = m.styles.CursorRow
	case m.selected[t.ID]:
		style = m.styles.SelectedRow
	}

	return []string{style.Render(b.String())}
}

// progressCell renders a fraction as a bar followed by its percentage,
// exactly colProgress cells wide.
//
// Drawn in one colour rather than two, unlike the underline this
// replaced. A row is rendered with a single style so that a cursor or
// selection highlight runs the whole way across it; styling the bar's
// halves separately would end that style mid-row and leave a hole in the
// highlight. The glyphs carry the bar on their own -- "━" is filled, "─"
// is track.
func progressCell(frac float64) string {
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
	filled := int(frac * barWidth)
	bar := strings.Repeat("━", filled) + strings.Repeat("─", barWidth-filled)
	return bar + " " + padRight(percentCell(frac)+"%", colPercent)
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
