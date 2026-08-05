package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the whole interface as one string.
//
// bubbletea diffs this against what is already on screen, so View must be
// a pure function of the Model -- no side effects, no I/O. Anything that
// needs either belongs in Update.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	// Nothing has told us how big the terminal is yet.
	if m.width == 0 {
		return "starting torrnado...\n"
	}
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("terminal too small: need at least %dx%d, have %dx%d\n",
			minWidth, minHeight, m.width, m.height)
	}

	p := layout(m.width, m.height)

	sidebar := m.styles.Pane.
		Width(p.sidebarContentW).
		Height(p.sidebarContentH).
		Render(clampBlock(m.renderSidebar(p), p.sidebarContentH))

	list := m.styles.Pane.
		Width(p.listContentW).
		Height(p.listContentH).
		Render(clampBlock(lipgloss.JoinVertical(lipgloss.Left,
			m.renderListHeader(p),
			m.renderListBody(p, m.visibleTorrents()),
		), p.listContentH))

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, list)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.renderFooter(p))
}

// clampBlock trims a rendered block to at most height lines, for the same
// reason clampLines exists: lipgloss grows a box to fit its contents, so
// overflowing one pushes the frame off the screen.
func clampBlock(s string, height int) string {
	if height <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		return s
	}
	return strings.Join(lines[:height], "\n")
}

// renderFooter draws the single bottom line: transfer totals on the left,
// any transient status message on the right.
func (m Model) renderFooter(p panes) string {
	g := m.global
	left := fmt.Sprintf(" ↓ %s  ↑ %s  │  %d torrents",
		formatRate(g.DownloadBPS), formatRate(g.UploadBPS), g.NumTorrents)

	right := ""
	if m.status != "" {
		style := m.styles.StatusBar
		if m.statusIsErr {
			style = m.styles.StatusErr
		}
		right = style.Render(m.status)
	}

	gap := p.footerW - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		// Not enough room for both. The totals are the line's reason for
		// existing, so the status goes rather than wrapping onto a row
		// the layout has not allocated.
		return truncate(m.styles.StatusBar.Render(left), p.footerW)
	}
	return m.styles.StatusBar.Render(left) + strings.Repeat(" ", gap) + right + " "
}
