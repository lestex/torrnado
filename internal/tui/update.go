package tui

import tea "github.com/charmbracelet/bubbletea"

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
