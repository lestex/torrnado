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
		// Quitting is all that is bound for now.
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	}

	return m, nil
}
