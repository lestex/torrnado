package tui

// Pane geometry constants. Sizes are total (border included) so they can
// be subtracted from the terminal dimensions directly.
const (
	sidebarWidth = 20 // total columns, border included
	detailHeight = 12 // total rows, border included

	// borderWidth/borderHeight are the columns/rows a pane border eats on
	// each axis (one cell per side).
	borderWidth  = 2
	borderHeight = 2

	// panePadX is the breathing room inside a pane's border, in columns
	// per side. Only horizontal: a blank row costs a torrent in the list
	// and an eighth of the detail pane, which is a lot to pay for air.
	panePadX = 1

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
// each pane, the box lipgloss is asked to draw, and the text area inside
// that. Every render function takes its widths from here rather than from
// constants, so the layout is responsive and the panes can't disagree
// about who owns which column.
type panes struct {
	sidebarW, sidebarH int
	listW, listH       int
	detailW, detailH   int
	footerW            int

	// boxW is what lipgloss's Width() is given: the pane minus its
	// border. Padding is *inside* that number -- lipgloss wraps at
	// width-left-right padding -- so a pane keeps its size on screen and
	// only its text area shrinks.
	sidebarBoxW, listBoxW, detailBoxW int

	// contentW/contentH are what a render function may actually draw
	// into: the box less the padding. Anything wider wraps onto a line
	// the layout never allocated, which pushes the frame off screen.
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

	p.sidebarBoxW = p.sidebarW - borderWidth
	p.listBoxW = p.listW - borderWidth
	p.detailBoxW = p.detailW - borderWidth

	// max(0, ...) because a negative width silently becomes an enormous
	// one by the time it reaches strings.Repeat.
	p.sidebarContentW = max(0, p.sidebarBoxW-2*panePadX)
	p.listContentW = max(0, p.listBoxW-2*panePadX)
	p.detailContentW = max(0, p.detailBoxW-2*panePadX)

	// Heights are untouched: the padding is horizontal only.
	p.sidebarContentH = p.sidebarH - borderHeight
	p.listContentH = p.listH - borderHeight
	p.detailContentH = p.detailH - borderHeight
	return p
}
