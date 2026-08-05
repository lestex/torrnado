package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/engine"
)

// Update handles one message and returns the next Model.
//
// Every change to the interface passes through here: a keystroke, a
// terminal resize, a state push from the daemon. That is the whole point
// of the pattern -- there is one place where the model changes.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case engineEventMsg:
		m.torrents = msg.Torrents
		m.global = msg.Global
		m.clampCursor(len(m.visibleTorrents()))
		// Cmds run once, so listening again is what keeps the stream
		// flowing.
		return m, listenForEvents(m.events)

	case engineClosedMsg:
		m.status = "lost connection to daemon"
		m.statusIsErr = true
		return m, nil

	case statusMsg:
		m.setStatus(msg)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	km := m.keymap

	// The help screen swallows the next key to dismiss itself, so there
	// is no key to learn for closing it.
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	// While typing a search, keys are text rather than commands.
	if m.mode == modeSearch {
		return m.handleSearchKey(msg)
	}

	visible := m.visibleTorrents()

	switch {
	// ctrl+c is not rebindable: it is what every terminal program does,
	// and a config file should not be able to take it away.
	case key == "ctrl+c", key == km.Quit:
		m.quitting = true
		return m, tea.Quit

	case key == km.Up, key == "up":
		if m.cursor > 0 {
			m.cursor--
		}

	case key == km.Down, key == "down":
		if m.cursor < len(visible)-1 {
			m.cursor++
		}

	case key == km.Top:
		m.cursor = 0

	case key == km.Bottom:
		if len(visible) > 0 {
			m.cursor = len(visible) - 1
		}

	case key == km.Remove:
		return m.removeTargets(visible, false)

	case key == km.RemoveData:
		return m.removeTargets(visible, true)

	case key == km.Pause:
		targets := m.targets(visible)
		if len(targets) == 0 {
			return m, nil
		}
		return m, pauseCmd(m.client, targets)

	case key == km.Recheck:
		return m.recheckTargets(visible)

	case key == km.Back:
		// Escape peels one layer at a time rather than clearing
		// everything at once, so it is never a surprise.
		if len(m.selected) > 0 {
			m.selected = map[engine.TorrentID]bool{}
		} else if m.searchQuery != "" {
			m.searchQuery = ""
			m.clampCursor(len(m.visibleTorrents()))
		} else if m.filter != filterAll {
			m.filter = filterAll
			m.clampCursor(len(m.visibleTorrents()))
		}

	case key == km.Search:
		m.mode = modeSearch
		return m, nil

	case key == km.Help:
		m.showHelp = true
		return m, nil

	case key == km.Select:
		// Marking a row advances the cursor, so a run of torrents can be
		// selected by holding one key rather than alternating two.
		if len(visible) > 0 {
			id := visible[m.cursor].ID
			if m.selected[id] {
				delete(m.selected, id)
			} else {
				m.selected[id] = true
			}
			if m.cursor < len(visible)-1 {
				m.cursor++
			}
		}
	}

	return m, nil
}

// targets is what an action applies to: every marked torrent if any are
// marked, otherwise just the row under the cursor.
//
// That rule is what lets the same keys work on one torrent or fifty
// without a separate "apply to selection" mode.
func (m Model) targets(visible []engine.TorrentSnapshot) []engine.TorrentSnapshot {
	if len(m.selected) > 0 {
		out := make([]engine.TorrentSnapshot, 0, len(m.selected))
		// Walked in list order, not map order, so the status message
		// counts and any partial failure are reproducible.
		for _, t := range m.torrents {
			if m.selected[t.ID] {
				out = append(out, t)
			}
		}
		return out
	}
	if len(visible) == 0 || m.cursor >= len(visible) {
		return nil
	}
	return visible[m.cursor : m.cursor+1]
}

func (m Model) removeTargets(visible []engine.TorrentSnapshot, deleteData bool) (tea.Model, tea.Cmd) {
	targets := m.targets(visible)
	if len(targets) == 0 {
		return m, nil
	}
	ids := make([]engine.TorrentID, len(targets))
	for i, t := range targets {
		ids[i] = t.ID
	}
	// Cleared up front: the rows are about to stop existing, and a
	// selection pointing at them would apply the next action to nothing.
	m.selected = map[engine.TorrentID]bool{}
	return m, removeCmd(m.client, ids, deleteData)
}

func (m Model) recheckTargets(visible []engine.TorrentSnapshot) (tea.Model, tea.Cmd) {
	targets := m.targets(visible)
	if len(targets) == 0 {
		return m, nil
	}
	ids := make([]engine.TorrentID, len(targets))
	for i, t := range targets {
		ids[i] = t.ID
	}
	return m, recheckCmd(m.client, ids)
}

// handleSearchKey applies one keystroke to the search query.
//
// It switches on the message type rather than its string form, which is
// the only reliable way to tell typed text from a named key: an input
// method or a paste can deliver several runes in one message, and
// anything filtering on "exactly one rune" silently drops the rest.
func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.mode = modeNormal
	case tea.KeyEsc:
		// Cancelling puts the list back, rather than leaving a filter
		// behind that was never confirmed.
		m.mode = modeNormal
		m.searchQuery = ""
	case tea.KeyBackspace:
		r := []rune(m.searchQuery)
		if len(r) > 0 {
			m.searchQuery = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.searchQuery += " "
	case tea.KeyRunes:
		m.searchQuery += string(msg.Runes)
	}

	// The list narrows on every keystroke, so the cursor has to follow.
	m.clampCursor(len(m.visibleTorrents()))
	return m, nil
}
