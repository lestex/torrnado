package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/engine"
)

// ddChordWindow bounds how long a "d" waits for its partner before it
// stops counting as the start of a chord.
const ddChordWindow = 600 * time.Millisecond

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
		// flowing. The docked pane is refreshed on the same tick.
		return m, tea.Batch(listenForEvents(m.events), m.syncDetail())

	case engineClosedMsg:
		// Set directly, with no expiry: the daemon being gone is a
		// standing condition, not an event that has passed.
		m.status = "lost connection to daemon"
		m.statusIsErr = true
		return m, nil

	case statusMsg:
		cmd := m.setStatus(msg)
		return m, cmd

	case statusExpiredMsg:
		m.clearStatus(msg.seq)
		return m, nil

	case detailLoadedMsg:
		if msg.err != nil {
			cmd := m.setStatus(errStatus(msg.err))
			return m, cmd
		}
		// A reply for a torrent the cursor has already left is stale;
		// dropping it stops a slow fetch overwriting the pane after the
		// user has moved on.
		if msg.detail.Snapshot.ID != m.detailID {
			return m, nil
		}
		m.detail = msg.detail
		m.detailLoaded = true
		if m.detailCursor >= len(m.detail.Files) {
			m.detailCursor = max(0, len(m.detail.Files)-1)
		}
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

	// While typing at a prompt, keys are text rather than commands.
	switch m.mode {
	case modeSearch:
		return m.handleSearchKey(msg)
	case modeCommand:
		return m.handleCommandKey(msg)
	}

	// Focus and tab movement work from any pane, so the detail tabs stay
	// reachable without moving focus into them first.
	switch key {
	case km.FocusNext:
		m.focus = m.focus.next()
		return m, nil
	case km.FocusPrev:
		m.focus = m.focus.prev()
		return m, nil
	case km.TabNext:
		return m.setDetailTab(m.detailTab.next())
	case km.TabPrev:
		return m.setDetailTab(m.detailTab.prev())
	case "1":
		return m.setDetailTab(tabPieces)
	case "2":
		return m.setDetailTab(tabPeers)
	case "3":
		return m.setDetailTab(tabFiles)
	}

	switch m.focus {
	case focusSidebar:
		return m.handleSidebarKey(key)
	case focusDetail:
		return m.handleDetailKey(key)
	}
	return m.handleListKey(key)
}

func (m Model) setDetailTab(t detailTab) (tea.Model, tea.Cmd) {
	m.detailTab = t
	m.detailScroll = 0
	return m, nil
}

// setFilter applies a status filter, wrapping at the ends so the keys
// cycle rather than dead-ending.
func (m Model) setFilter(f statusFilter) (tea.Model, tea.Cmd) {
	n := statusFilter(len(filterNames))
	m.filter = (f%n + n) % n
	m.sidebarCursor = int(m.filter)
	m.clampCursor(len(m.visibleTorrents()))
	return m, m.syncDetail()
}

// handleSidebarKey handles keys while the sidebar holds focus.
//
// The filter applies as the cursor moves rather than on a confirm key:
// the sidebar shows exactly one highlighted entry, and a cursor that
// could sit elsewhere would need a second highlight to explain itself.
func (m Model) handleSidebarKey(key string) (tea.Model, tea.Cmd) {
	km := m.keymap
	switch {
	case key == km.Up, key == "up":
		return m.setFilter(m.filter - 1)
	case key == km.Down, key == "down":
		return m.setFilter(m.filter + 1)
	case key == km.Top:
		return m.setFilter(filterAll)
	case key == km.Bottom:
		return m.setFilter(statusFilter(len(filterNames) - 1))
	}
	return m.handleListKey(key)
}

func (m Model) handleListKey(key string) (tea.Model, tea.Cmd) {
	km := m.keymap
	visible := m.visibleTorrents()

	// Any key other than "d" abandons a half-typed chord.
	if key != "d" {
		m.pendingDD = false
	}

	switch {
	case key == "d":
		// The window matters: without it a "d" pressed minutes ago would
		// silently turn the next one into a deletion.
		if m.pendingDD && time.Since(m.pendingAt) <= ddChordWindow {
			m.pendingDD = false
			return m.removeTargets(visible, false)
		}
		m.pendingDD = true
		m.pendingAt = time.Now()
		return m, nil

	// ctrl+c is not rebindable: it is what every terminal program does,
	// and a config file should not be able to take it away.
	case key == "ctrl+c", key == km.Quit:
		m.quitting = true
		return m, tea.Quit

	case key == km.Up, key == "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, m.syncDetail()

	case key == km.Down, key == "down":
		if m.cursor < len(visible)-1 {
			m.cursor++
		}
		return m, m.syncDetail()

	case key == km.Top:
		m.cursor = 0
		return m, m.syncDetail()

	case key == km.Bottom:
		if len(visible) > 0 {
			m.cursor = len(visible) - 1
		}
		return m, m.syncDetail()

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

	// The detail pane is always docked, so there is no view to open --
	// this moves focus into it instead.
	case key == km.Detail:
		m.focus = focusDetail
		return m, nil

	case key == km.Back:
		// Escape peels one layer at a time rather than clearing
		// everything at once, so it is never a surprise.
		switch {
		case m.focus != focusList:
			m.focus = focusList
			return m, nil
		}
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

	case key == km.Command:
		m.mode = modeCommand
		m.commandBuf = ""
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
		return m, m.syncDetail()
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

// handleCommandKey applies one keystroke to the command line, running it
// on enter.
func (m Model) handleCommandKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		line := m.commandBuf
		m.mode = modeNormal
		m.commandBuf = ""
		return m.execCommand(line)
	case tea.KeyEsc:
		m.mode = modeNormal
		m.commandBuf = ""
	case tea.KeyBackspace:
		r := []rune(m.commandBuf)
		if len(r) > 0 {
			m.commandBuf = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.commandBuf += " "
	case tea.KeyRunes:
		m.commandBuf += string(msg.Runes)
	}
	return m, nil
}
