package tui

// Pane geometry constants. Sizes are total (border included) so they can
// be subtracted from the terminal dimensions directly.
const (
	sidebarWidth = 20 // total columns, border included
	detailHeight = 12 // total rows, border included

	// borderWidth/borderHeight are the columns/rows a rounded border eats
	// on each axis (one cell per side).
	borderWidth  = 2
	borderHeight = 2

	// minWidth/minHeight are the smallest terminal the paneled layout can
	// be drawn in. Below this the panes collapse to nothing and lipgloss
	// starts wrapping content, so a plain message is rendered instead.
	minWidth  = 60
	minHeight = 15

	// compactHeight is the point below which the detail pane gives up its
	// body and keeps only the tab strip, so the torrent list stays usable
	// on a short terminal.
	compactHeight = 24
)

// panes holds the computed geometry for one render pass: the total size of
// each pane plus the usable content area inside its border. Every render
// function takes its widths from here rather than from constants, so the
// layout is responsive and the panes can't disagree about who owns which
// column.
type panes struct {
	sidebarW, sidebarH int
	listW, listH       int
	detailW, detailH   int
	footerW            int

	// contentW/contentH are the inner dimensions, border excluded.
	sidebarContentW, sidebarContentH int
	listContentW, listContentH       int
	detailContentW, detailContentH   int
}

// layout computes pane geometry for a width x height terminal. It assumes
// width/height are at least minWidth/minHeight -- View checks that first.
func layout(width, height int) panes {
	// One row goes to the footer; the panes share the rest.
	bodyH := height - 1

	detailH := detailHeight
	if height < compactHeight {
		detailH = 3 // border + the tab strip alone
	}
	if detailH > bodyH-4 {
		detailH = max(3, bodyH-4)
	}

	sidebarW := sidebarWidth
	if sidebarW > width/3 {
		sidebarW = max(10, width/3)
	}
	rightW := width - sidebarW

	p := panes{
		sidebarW: sidebarW,
		sidebarH: bodyH,
		listW:    rightW,
		listH:    bodyH - detailH,
		detailW:  rightW,
		detailH:  detailH,
		footerW:  width,
	}

	p.sidebarContentW = p.sidebarW - borderWidth
	p.sidebarContentH = p.sidebarH - borderHeight
	p.listContentW = p.listW - borderWidth
	p.listContentH = p.listH - borderHeight
	p.detailContentW = p.detailW - borderWidth
	p.detailContentH = p.detailH - borderHeight
	return p
}
