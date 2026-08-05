package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Column widths for the Peers table. Address is elastic; the rest are
// fixed. There is no Choked column: anacrolix/torrent does not export
// peer choke state -- see the doc comment on engine.PeerInfo.
//
// As in list.go, the "fixed" totals must count one gap per column
// including the elastic one's, or the row overruns the pane by a cell and
// gets truncated at the right edge.
const (
	peerColClient = 20
	peerColPieces = 13
	peerColSource = 9
	peerColSpeed  = 22
	peerColFixed  = colGap + 4*colGap +
		peerColClient + peerColPieces + peerColSource + peerColSpeed

	fileColSize  = 9
	fileColPrio  = 8 // fits the "Priority" header, wider than any value
	fileColPct   = 5
	fileColFixed = colMark + 4*colGap + fileColSize + fileColPct + fileColPrio
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
	var lines []string
	switch m.detailTab {
	case tabPeers:
		lines = m.peersTab(p, height)
	default:
		lines = []string{m.styles.Muted.Render(" (nothing to show yet)")}
	}
	return strings.Join(clampLines(lines, height), "\n")
}

func (m Model) peersTab(p panes, height int) []string {
	d := m.detail
	addrW := max(8, p.detailContentW-peerColFixed)

	header := " " + padRight("Address", addrW) +
		" " + padRight("Client", peerColClient) +
		" " + padLeft("Pieces", peerColPieces) +
		" " + padRight("Source", peerColSource) +
		" " + padRight("Speed", peerColSpeed)

	lines := []string{
		m.styles.Muted.Render(fmt.Sprintf(" Peers: %d", len(d.Peers))),
		m.styles.ColHeader.Render(truncate(header, p.detailContentW)),
	}
	if len(d.Peers) == 0 {
		return append(lines, m.styles.Muted.Render(" (no connected peers)"))
	}

	// Two lines are already spent on the summary and header.
	start, end := scrollWindow(m.detailScroll, len(d.Peers), height-2)
	for _, peer := range d.Peers[start:end] {
		name := peer.Client
		if peer.Encrypted {
			name += " 🔒"
		}
		row := " " + padRight(peer.Addr, addrW) +
			" " + padRight(name, peerColClient) +
			" " + padLeft(fmt.Sprintf("%d/%d", peer.PiecesHave, peer.PiecesTotal), peerColPieces) +
			" " + padRight(peer.Source, peerColSource) +
			" " + padRight(fmt.Sprintf("↓ %s ↑ %s", formatRate(peer.DownloadBPS), formatRate(peer.UploadBPS)), peerColSpeed)
		lines = append(lines, m.styles.Row.Render(truncate(row, p.detailContentW)))
	}
	return lines
}
