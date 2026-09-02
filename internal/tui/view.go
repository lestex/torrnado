package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the whole interface as one string.
//
// bubbletea diffs this against what is already on screen, so View must be
// a pure function of the Model - no side effects, no I/O. Anything that
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
	frame := lipgloss.JoinVertical(lipgloss.Left, body, m.renderFooter(p))

	// Spliced over the finished frame rather than joined to it, so the
	// panes stay on screen underneath and a theme is previewed on the
	// real interface.
	if m.themePicker {
		box, x, y := m.renderThemePicker()
		return overlay(frame, box, x, y)
	}
	return frame
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
// - the list's selection highlight - which painted a block of
// background around the typed text and stopped dead at the end of it,
// reading as a stray highlight rather than a prompt.
//
// Trimming happens before styling, not after: truncate measures printable
// width but cuts runes, so trimming an already-styled string cuts through
// its escape sequences.
// renderPrompt draws the input line, optionally followed by Tab
// candidates.
//
// The candidates go after the cursor, muted and behind a gap, so they
// read as a hint rather than as text that has been typed. They are only
// drawn when what is typed leaves room: the input is the thing that must
// never be squeezed off its own line.
func (m Model) renderPrompt(sigil, buf string, width int, hints ...string) string {
	// The leading space, the sigil and the cursor are a cell each.
	room := width - 3
	if room < 1 {
		return truncate(" "+sigil, width)
	}
	shown := truncateTail(buf, room)
	line := m.styles.Accent.Render(" "+sigil) +
		m.styles.Base.Render(shown) +
		m.styles.Accent.Render("█")

	const gap = 3
	spare := room - lipgloss.Width(shown) - gap
	if len(hints) == 0 || spare < minHintW {
		return line
	}
	return line + m.styles.Muted.Render(
		strings.Repeat(" ", gap)+truncate(candidateList(hints), spare))
}

// minHintW is the narrowest a candidate list can be and still say
// anything; below it the prompt keeps the whole line.
const minHintW = 12

// footerRateW is the cell each transfer total is drawn in: the arrow, a
// space, the widest rate format.Rate can produce ("1023.9KiB/s"), and one
// space before whatever comes next.
//
// Fixed, because everything to the right of these two numbers moves when
// they do otherwise - the separator and the torrent count slid a cell
// left and right every second as a speed crossed 10, 100 or a unit
// boundary, which is the sort of movement the eye follows and the mind
// then has to dismiss.
// 2 cells for "↓ ", 11 for the rate, 1 to keep it off the next thing.
const footerRateW = 14

// renderFooter draws the single bottom line: transfer totals on the left,
// any transient status message on the right.
func (m Model) renderFooter(p panes) string {
	// While typing, the footer is the prompt - there is nowhere else to
	// put it, and the totals are less useful than seeing what you typed.
	switch m.mode {
	case modeSearch:
		return m.renderPrompt("/", m.searchQuery, p.footerW)
	case modeCommand:
		return m.renderPrompt(":", m.commandBuf, p.footerW, m.completions...)
	}
	if m.showHelp {
		return m.styles.StatusBar.Render(" press any key to close help")
	}
	g := m.global
	left := " " +
		padRight("↓ "+formatRate(g.DownloadBPS), footerRateW) +
		padRight("↑ "+formatRate(g.UploadBPS), footerRateW) +
		fmt.Sprintf("│  %d torrents", g.NumTorrents)

	statusStyle := m.styles.StatusBar
	if m.statusIsErr {
		statusStyle = m.styles.StatusErr
	}

	// With nothing to report, that end of the line carries the way in to
	// everything else. It costs a corner of a row that was empty anyway,
	// and it is the difference between a reference someone finds and one
	// they have to already know the key for. Any real message takes the
	// space back.
	right, rightStyle := m.status, statusStyle
	if right == "" {
		right, rightStyle = m.helpHint(), m.styles.Muted
	}

	// Both, when both fit.
	if gap := p.footerW - lipgloss.Width(left) - lipgloss.Width(right) - 1; gap >= 1 {
		line := m.styles.StatusBar.Render(left)
		if right != "" {
			line += strings.Repeat(" ", gap) + rightStyle.Render(right) + " "
		}
		return truncate(line, p.footerW)
	}
	// The hint yields rather than taking the line: it answers a question
	// nobody asked, where a status message answers one they did.
	if m.status == "" {
		return truncate(m.styles.StatusBar.Render(left), p.footerW)
	}

	// Otherwise the message takes the line. It answers something the user
	// just did and clears itself after a few seconds, while the totals
	// are always a keystroke away and are back the moment it expires -
	// and this used to drop the message instead, so a long one (every
	// error naming a path, say) simply never appeared.
	//
	// Truncated before styling: truncate measures printable width but
	// cuts runes, so trimming a styled string cuts its escape sequences.
	return statusStyle.Render(truncate(" "+m.status, p.footerW))
}
