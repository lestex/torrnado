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

// renderPrompt draws the input line for search and command mode: the
// sigil, what has been typed, and a block where the next character will
// land.
//
// Plain text, the way vim's ex line is. This used to borrow SelectedRow
// -- the list's selection highlight -- which painted a block of
// background around the typed text and stopped dead at the end of it,
// reading as a stray highlight rather than a prompt.
//
// Trimming happens before styling, not after: truncate measures printable
// width but cuts runes, so trimming an already-styled string cuts through
// its escape sequences.
func (m Model) renderPrompt(sigil, buf string, width int) string {
	// The leading space, the sigil and the cursor are a cell each.
	room := width - 3
	if room < 1 {
		return truncate(" "+sigil, width)
	}
	return m.styles.Accent.Render(" "+sigil) +
		m.styles.Base.Render(truncateTail(buf, room)) +
		m.styles.Accent.Render("█")
}

// renderFooter draws the single bottom line: transfer totals on the left,
// any transient status message on the right.
func (m Model) renderFooter(p panes) string {
	// While typing, the footer is the prompt -- there is nowhere else to
	// put it, and the totals are less useful than seeing what you typed.
	switch m.mode {
	case modeSearch:
		return m.renderPrompt("/", m.searchQuery, p.footerW)
	case modeCommand:
		return m.renderPrompt(":", m.commandBuf, p.footerW)
	}
	if m.showHelp {
		return m.styles.StatusBar.Render(" press any key to close help")
	}
	g := m.global
	left := fmt.Sprintf(" ↓ %s  ↑ %s  │  %d torrents",
		formatRate(g.DownloadBPS), formatRate(g.UploadBPS), g.NumTorrents)

	statusStyle := m.styles.StatusBar
	if m.statusIsErr {
		statusStyle = m.styles.StatusErr
	}

	// Both, when both fit.
	if gap := p.footerW - lipgloss.Width(left) - lipgloss.Width(m.status) - 1; m.status == "" || gap >= 1 {
		line := m.styles.StatusBar.Render(left)
		if m.status != "" {
			line += strings.Repeat(" ", gap) + statusStyle.Render(m.status) + " "
		}
		return truncate(line, p.footerW)
	}

	// Otherwise the message takes the line. It answers something the user
	// just did and clears itself after a few seconds, while the totals
	// are always a keystroke away and are back the moment it expires --
	// and this used to drop the message instead, so a long one (every
	// error naming a path, say) simply never appeared.
	//
	// Truncated before styling: truncate measures printable width but
	// cuts runes, so trimming a styled string cuts its escape sequences.
	return statusStyle.Render(truncate(" "+m.status, p.footerW))
}
