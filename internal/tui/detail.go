package tui

import (
	"fmt"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/engine"
)

// fileColFixed and the peer widths follow the same rule as the list's
// columns: count one gap per column including the elastic one's, or the
// row overruns its pane by a cell and gets truncated at the edge.
//
// Column widths for the Peers table. Address is elastic; the rest are
// fixed. There is no Choked column: anacrolix/torrent does not export
// peer choke state - see the doc comment on engine.PeerInfo.
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

	fileColSize = 9
	fileColPrio = 8 // fits the "Priority" header, wider than any value
	fileColPct  = 5
	// One marker cell: a file can be under the cursor, and that is all -
	// unlike a torrent row, which can also be selected.
	fileColMark  = 1
	fileColFixed = fileColMark + 4*colGap + fileColSize + fileColPct + fileColPrio
)

// handleDetailKey handles keys while the detail pane holds focus. Keys it
// does not claim fall through to the list, so pausing, quitting and the
// palette keep working from any pane.
func (m Model) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	km := m.keymap

	switch {
	case key == km.Up, key == "up":
		if m.detailTab == tabFiles {
			if m.detailCursor > 0 {
				m.detailCursor--
			}
			return m, nil
		}
		if m.detailScroll > 0 {
			m.detailScroll--
		}
		return m, nil

	case key == km.Down, key == "down":
		if m.detailTab == tabFiles {
			if m.detailCursor < len(m.detail.Files)-1 {
				m.detailCursor++
			}
			return m, nil
		}
		m.detailScroll++
		return m, nil

	case key == km.Select && m.detailTab == tabFiles:
		// A checkbox, not a step through the five-level scale that + and
		// - walk: "do I want this file" is the question a torrent full of
		// episodes and extras actually asks, and answering it should not
		// require knowing where `none` sits relative to `normal`.
		//
		// The cursor advances afterwards, the same as marking a torrent
		// in the list, so a run of files can be turned off by holding one
		// key rather than alternating two.
		return m.toggleFileWanted()

	case key == "+" || key == "=":
		return m.adjustFilePriority(1)

	case key == "-" || key == "_":
		return m.adjustFilePriority(-1)
	}

	return m.handleListKey(key)
}

// renderDetailTabs draws the tab strip.
//
// The active tab is bracketed as well as colored, so the two remain
// distinguishable on a 16-color terminal where accent and muted can end
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
	case tabPieces:
		lines = m.piecesTab(p, height)
	case tabPeers:
		lines = m.peersTab(p, height)
	case tabFiles:
		lines = m.filesTab(p, height)
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

func (m Model) filesTab(p panes, height int) []string {
	d := m.detail
	pathW := max(8, p.detailContentW-fileColFixed)

	lines := []string{
		m.styles.ColHeader.Render(truncate(
			strings.Repeat(" ", fileColMark+colGap)+
				padRight("File", pathW)+
				" "+padLeft("Size", fileColSize)+
				" "+padLeft("%", fileColPct)+
				" "+padRight("Priority", fileColPrio),
			p.detailContentW)),
	}
	if len(d.Files) == 0 {
		return append(lines, m.styles.Muted.Render(" (metadata not yet available)"))
	}

	start, end := scrollWindow(m.detailCursor, len(d.Files), height-1)
	for i := start; i < end; i++ {
		f := d.Files[i]
		mark := " "
		style := m.styles.Row
		// A skipped file is dimmed whole rather than only saying "none"
		// in its last column: in a torrent of fifty files, which ones are
		// off has to be answerable at a glance, not by reading a column.
		if f.Priority == engine.PriorityNone {
			style = m.styles.Muted
		}
		if i == m.detailCursor && m.focus == focusDetail {
			mark, style = ">", m.styles.CursorRow
		}
		row := mark + " " +
			padRight(mediaMarker(f.Path)+f.Path, pathW) +
			" " + padLeft(formatBytes(f.Length), fileColSize) +
			" " + padLeft(percentCell(fileProgress(f)), fileColPct) +
			" " + padRight(f.Priority.String(), fileColPrio)
		lines = append(lines, style.Render(truncate(row, p.detailContentW)))
	}
	return lines
}

// previewFile streams a file to the configured player, from any pane.
//
// Any file is allowed, not just recognized video: players handle audio
// too, and an extension whitelist would only refuse things that work.
// The mediaMarker in the file list is the discoverability hint instead.
//
// When it cannot play anything it says so. It used to return silently,
// which is how pressing v on a torrent in the list - the obvious way to
// play one - looked like a broken feature rather than a key that had not
// been wired up.
func (m Model) previewFile() (tea.Model, tea.Cmd) {
	t, ok := m.cursorTorrent()
	if !ok {
		return m, nil
	}

	// The detail pane is fetched for whatever the cursor is on, so its
	// file list belongs to this torrent - unless the fetch has not
	// landed yet, which is a second or so after the cursor moves.
	if !m.detailLoaded || m.detail.Snapshot.ID != t.ID {
		cmd := m.setStatus(errStatus(fmt.Errorf("still reading %s", t.Name)))
		return m, cmd
	}
	if len(m.detail.Files) == 0 {
		cmd := m.setStatus(errStatus(fmt.Errorf("no files yet - still waiting for this torrent's metadata")))
		return m, cmd
	}

	// The file under the cursor when the Files tab is open and the cursor
	// is on one; otherwise the biggest file in the torrent.
	//
	// Pressing v on a row in the list is how anyone would try to play a
	// torrent, and it used to do nothing at all - the key was claimed
	// only by the detail pane, and only on one of its three tabs. The
	// largest file is what "play this" means: the feature is bigger than
	// the sample, the sample bigger than the subtitles.
	f := largestFile(m.detail.Files)
	if m.detailTab == tabFiles && m.detailCursor < len(m.detail.Files) {
		f = m.detail.Files[m.detailCursor]
	}
	return m, previewCmd(m.client, m.player, m.detail.Snapshot.ID, f)
}

// largestFile returns the biggest file of a torrent. Callers must not
// pass an empty list.
func largestFile(files []engine.FileInfo) engine.FileInfo {
	largest := files[0]
	for _, f := range files[1:] {
		if f.Length > largest.Length {
			largest = f
		}
	}
	return largest
}

// toggleFileWanted turns the file under the cursor off, or back on.
//
// Back on is `normal` rather than whatever it was before: the library has
// nothing between "not wanted" and "wanted normally" anyway (see the
// caveats on `priority low`), so remembering a previous level would be
// remembering a distinction that does not exist. A file deliberately set
// to high keeps that, because toggling it off and on again is not
// something anybody does to a file they raised.
func (m Model) toggleFileWanted() (tea.Model, tea.Cmd) {
	if m.detailCursor >= len(m.detail.Files) {
		return m, nil
	}
	f := m.detail.Files[m.detailCursor]

	want := engine.PriorityNone
	if f.Priority == engine.PriorityNone {
		want = engine.PriorityNormal
	}
	if m.detailCursor < len(m.detail.Files)-1 {
		m.detailCursor++
	}
	return m, setPriorityCmd(m.client, m.detail.Snapshot.ID, f.Index, want)
}

func (m Model) adjustFilePriority(delta int) (tea.Model, tea.Cmd) {
	if m.detailTab != tabFiles || m.detailCursor >= len(m.detail.Files) {
		return m, nil
	}
	f := m.detail.Files[m.detailCursor]
	p := f.Priority + engine.Priority(delta)
	if p < engine.PriorityNone {
		p = engine.PriorityNone
	}
	if p > engine.PriorityNow {
		p = engine.PriorityNow
	}
	return m, setPriorityCmd(m.client, m.detail.Snapshot.ID, f.Index, p)
}

func fileProgress(f engine.FileInfo) float64 {
	if f.Length == 0 {
		return 0
	}
	return float64(f.Completed) / float64(f.Length)
}

func (m Model) piecesTab(p panes, height int) []string {
	d := m.detail
	lines := []string{m.styles.Muted.Render(pieceSummary(d))}
	if bitmap := m.renderPieceMap(d, p.detailContentW, height-1); bitmap != "" {
		lines = append(lines, strings.Split(bitmap, "\n")...)
	}
	return lines
}

// mediaExtensions are the file types worth flagging as playable in the
// file list. This drives the marker only - preview is never refused on
// the strength of an extension.
var mediaExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true, ".m4v": true,
	".webm": true, ".flv": true, ".wmv": true, ".mpg": true, ".mpeg": true,
	".ts": true, ".m2ts": true, ".ogv": true,
	".mp3": true, ".flac": true, ".m4a": true, ".ogg": true, ".opus": true,
	".wav": true, ".aac": true,
}

func mediaMarker(p string) string {
	// path, not filepath: FileInfo.Path is always '/'-separated as it
	// comes off the wire, regardless of the host OS.
	if mediaExtensions[strings.ToLower(path.Ext(p))] {
		return "▸"
	}
	return " "
}
