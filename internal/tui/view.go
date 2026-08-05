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

	// The help screen replaces the panes rather than floating over them:
	// at these sizes there is nowhere to float it that would not clip the
	// reference it exists to show.
	if m.showHelp {
		helpW, helpH := m.width-borderWidth, m.height-1-borderHeight
		return lipgloss.JoinVertical(lipgloss.Left,
			m.styles.Pane.Width(helpW).Height(helpH).
				Render(m.renderHelp(helpW-2*panePadX, helpH)),
			m.renderFooter(p),
		)
	}

	sidebar := m.styles.pane(m.focus == focusSidebar).
		Width(p.sidebarBoxW).
		Height(p.sidebarContentH).
		Render(clampBlock(m.renderSidebar(p), p.sidebarContentH))

	list := m.styles.pane(m.focus == focusList).
		Width(p.listBoxW).
		Height(p.listContentH).
		Render(clampBlock(lipgloss.JoinVertical(lipgloss.Left,
			m.renderListHeader(p),
			m.renderListBody(p, m.visibleTorrents(), p.listContentH),
		), p.listContentH))

	detail := m.styles.pane(m.focus == focusDetail).
		Width(p.detailBoxW).
		Height(p.detailContentH).
		Render(clampBlock(lipgloss.JoinVertical(lipgloss.Left,
			m.renderDetailTabs(p),
			m.renderDetailBody(p),
		), p.detailContentH))

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		sidebar,
		lipgloss.JoinVertical(lipgloss.Left, list, detail),
	)
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
	// While typing, the footer is the prompt -- there is nowhere else to
	// put it, and the totals are less useful than seeing what you typed.
	switch m.mode {
	case modeSearch:
		return truncate(m.styles.SelectedRow.Render(" /"+m.searchQuery), p.footerW)
	case modeCommand:
		return truncate(m.styles.SelectedRow.Render(" :"+m.commandBuf), p.footerW)
	}
	if m.showHelp {
		return m.styles.StatusBar.Render(" press any key to close help")
	}
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
