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
	colPct    = 4

	// colGap is the single space between columns. Every column except the
	// marker is preceded by one, so fixedColumns -- everything the Name
	// column does NOT get -- counts seven of them: one per fixed column
	// plus Name's own. Getting this off by one lets a row exceed the pane
	// width, and lipgloss then wraps it onto a second line.
	colGap       = 1
	fixedColumns = colMark + 7*colGap +
		colSize + colStatus + colDown + colUp + colETA + colPct

	// rowLines is how many terminal rows one torrent occupies: the data
	// line plus the progress underline beneath it.
	rowLines = 2

	// minNameWidth is the narrowest Name column worth rendering; below
	// this the trailing columns are dropped instead (see nameWidth).
	minNameWidth = 12
)

// nameWidth returns the elastic Name column width for a pane of the given
// content width, and whether the full column set fits at all.
func nameWidth(contentW int) (int, bool) {
	w := contentW - fixedColumns
	if w < minNameWidth {
		return max(1, contentW-colMark-colGap-colPct-colGap), false
	}
	return w, true
}

func (m Model) renderListHeader(p panes) string {
	nameW, wide := nameWidth(p.listContentW)

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", colMark+colGap))
	b.WriteString(padRight("Name", nameW))
	if wide {
		b.WriteString(" " + padLeft("Size", colSize))
		b.WriteString(" " + padRight("Status", colStatus))
		b.WriteString(" " + padLeft("↓ Speed", colDown))
		b.WriteString(" " + padLeft("↑ Speed", colUp))
		b.WriteString(" " + padLeft("ETA", colETA))
	}
	b.WriteString(" " + padLeft("%", colPct))
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

// renderRow draws one torrent as rowLines lines: the data columns, then a
// progress underline spanning the Name column.
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
	b.WriteString(padRight(t.Name, nameW))
	if wide {
		b.WriteString(" " + padLeft(formatBytes(t.TotalLength), colSize))
		b.WriteString(" " + padRight(t.StatusText(), colStatus))
		b.WriteString(" " + padLeft(rateCell("↓ ", t.DownloadBPS), colDown))
		b.WriteString(" " + padLeft(rateCell("↑ ", t.UploadBPS), colUp))
		b.WriteString(" " + padLeft(etaCell(t), colETA))
	}
	b.WriteString(" " + padLeft(percentCell(t.Progress), colPct))

	// The whole line takes one style, so a highlight cannot be broken
	// partway through by a nested style's ANSI reset.
	style := m.styles.Row
	switch {
	case isCursor:
		style = m.styles.CursorRow
	case m.selected[t.ID]:
		style = m.styles.SelectedRow
	}

	return []string{
		style.Render(b.String()),
		m.progressUnderline(t, nameW),
	}
}

// progressUnderline draws the thin bar beneath a torrent's name. A
// complete torrent gets a blank line instead: an underline that is always
// full carries no information and just adds visual noise to a list of
// finished torrents.
func (m Model) progressUnderline(t engine.TorrentSnapshot, nameW int) string {
	indent := strings.Repeat(" ", colMark+colGap)
	if t.Progress >= 1 || nameW <= 0 {
		return ""
	}
	filled := int(t.Progress * float64(nameW))
	if filled > nameW {
		filled = nameW
	}
	return indent +
		m.styles.ProgressFill.Render(strings.Repeat("━", filled)) +
		m.styles.ProgressTrack.Render(strings.Repeat("─", nameW-filled))
}

// percentCell renders a completion fraction as a whole number.
//
// It floors rather than rounds: at 99.6% the nearest whole number is 100,
// and a row that claims to be finished while the progress bar beside it
// still shows a gap reads as a bug.
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
