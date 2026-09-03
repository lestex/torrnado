package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// The panes are assembled as JoinHorizontal(sidebar, JoinVertical(list,
// detail)) with the footer beneath, so where each one starts is derived
// from the sizes layout() already computed rather than stored again.
//
// Every pane is a bordered box: its first content row is one below its
// top edge, and its first content column is one in from its left edge
// plus panePadX. Getting that wrong by one is how a click lands on the
// row above the one it appeared to hit.
type mouseTarget int

const (
	targetNone mouseTarget = iota
	targetSidebar
	targetList
	targetDetailTabs
	targetDetailBody
)

// hitTest says which pane a terminal coordinate falls in, and which row
// of that pane's content it is.
func (m Model) hitTest(p panes, x, y int) (mouseTarget, int) {
	switch {
	case x < p.sidebarW:
		row := y - 1 // the top border
		if row < 0 || row >= p.sidebarContentH {
			return targetNone, 0
		}
		return targetSidebar, row

	case y < p.listH:
		row := y - 1
		if row < 0 || row >= p.listContentH {
			return targetNone, 0
		}
		return targetList, row

	case y < p.listH+p.detailH:
		row := y - p.listH - 1
		if row < 0 || row >= p.detailContentH {
			return targetNone, 0
		}
		// The tab strip owns the first content row; everything below it
		// is the body.
		if row == 0 {
			return targetDetailTabs, 0
		}
		return targetDetailBody, row - 1
	}
	return targetNone, 0
}

// detailTabSpans gives the column each tab label starts at and how wide
// it is, in the detail pane's content coordinates.
//
// It mirrors renderDetailTabs: a three-column " ─ " lead, then each label
// wrapped in one column either side - "[Pieces]" when active, " Pieces "
// when not, which are the same width - separated by two spaces. A test
// renders the strip and checks each span lands on its own label, so the
// two cannot drift apart silently.
func detailTabSpans() []struct{ start, width int } {
	out := make([]struct{ start, width int }, len(detailTabNames))
	col := 3
	for i, name := range detailTabNames {
		w := len(name) + 2
		out[i] = struct{ start, width int }{col, w}
		col += w + 2
	}
	return out
}

func detailTabAt(col int) (detailTab, bool) {
	for i, span := range detailTabSpans() {
		if col >= span.start && col < span.start+span.width {
			return detailTab(i), true
		}
	}
	return 0, false
}

// handleMouse routes a mouse event to whatever is under the pointer.
//
// The wheel scrolls the pane it is over rather than the focused one,
// which is what every other program does and what makes a wheel useful
// without clicking first. A click moves focus as well as acting, so the
// keyboard carries on from where the pointer left off.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.width == 0 || m.height == 0 {
		return m, nil // no layout yet
	}
	p := layout(m.width, m.height)
	target, row := m.hitTest(p, msg.X, msg.Y)

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.scrollTarget(target, -1)
	case tea.MouseButtonWheelDown:
		return m.scrollTarget(target, 1)
	}

	// Only a press, and only the left button: a release would act twice,
	// and a right-click is the terminal's own menu in most emulators.
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return m, nil
	}

	switch target {
	case targetSidebar:
		_, rowEntry := m.buildSidebar(p)
		if row >= len(rowEntry) || rowEntry[row] < 0 {
			return m, nil // a heading, a blank, or the daemon block
		}
		m.focus = focusSidebar
		return m.selectSidebar(rowEntry[row])

	case targetList:
		if row == 0 {
			return m, nil // the column header
		}
		visible := m.visibleTorrents()
		// The same window the body was drawn with, so the row clicked is
		// the row that was under the pointer even when the list is
		// scrolled.
		start, _ := scrollWindow(m.cursor, len(visible), (p.listContentH-1)/rowLines)
		i := start + (row-1)/rowLines
		if i < 0 || i >= len(visible) {
			return m, nil
		}
		m.focus = focusList
		m.cursor = i
		return m, m.syncDetail()

	case targetDetailTabs:
		tab, ok := detailTabAt(msg.X - p.sidebarW - 1 - panePadX)
		if !ok {
			return m, nil
		}
		m.focus = focusDetail
		return m.setDetailTab(tab)

	case targetDetailBody:
		m.focus = focusDetail
		return m, nil
	}
	return m, nil
}

// scrollTarget moves the pane under the pointer by one row.
func (m Model) scrollTarget(target mouseTarget, delta int) (tea.Model, tea.Cmd) {
	switch target {
	case targetSidebar:
		return m.selectSidebar(m.currentSidebarIndex() + delta)
	case targetList:
		visible := m.visibleTorrents()
		if len(visible) == 0 {
			return m, nil
		}
		m.cursor += delta
		m.clampCursor(len(visible))
		return m, m.syncDetail()
	case targetDetailTabs, targetDetailBody:
		m.detailScroll = max(0, m.detailScroll+delta)
		return m, nil
	}
	return m, nil
}
