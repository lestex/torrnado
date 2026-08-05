package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handleDetailKey handles keys while the detail pane holds focus. Keys it
// does not claim fall through to the list, so pausing, quitting and the
// palette keep working from any pane.
func (m Model) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	km := m.keymap

	switch {
	case key == km.Up, key == "up":
		if m.detailScroll > 0 {
			m.detailScroll--
		}
		return m, nil

	case key == km.Down, key == "down":
		m.detailScroll++
		return m, nil
	}

	return m.handleListKey(key)
}

// renderDetailTabs draws the tab strip.
//
// The active tab is bracketed as well as coloured, so the two remain
// distinguishable on a 16-colour terminal where accent and muted can end
// up looking alike.
func (m Model) renderDetailTabs(p panes) string {
	var b strings.Builder
	b.WriteString(m.styles.Muted.Render(" ─ "))
	for i, name := range detailTabNames {
		if i > 0 {
			b.WriteString("  ")
		}
		if detailTab(i) == m.detailTab {
			b.WriteString(m.styles.TabActive.Render("[" + name + "]"))
			continue
		}
		b.WriteString(m.styles.TabInactive.Render(" " + name + " "))
	}
	return truncate(b.String(), p.detailContentW)
}

func (m Model) renderDetailBody(p panes) string {
	height := p.detailContentH - 1 // the tab strip owns one row
	if height <= 0 {
		return ""
	}
	if _, ok := m.cursorTorrent(); !ok {
		return ""
	}
	if !m.detailLoaded {
		return m.styles.Muted.Render(" loading...")
	}
	return m.styles.Muted.Render(" (nothing to show yet)")
}
