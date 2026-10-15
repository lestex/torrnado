package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// quitsOnCtrlC presses ctrl+c and reports whether the model asked to
// quit. Both halves matter: tea.Quit is what ends the program, and
// m.quitting is what makes View render nothing, so a quit without it
// leaves the last frame painted over the shell prompt.
func quitsOnCtrlC(t *testing.T, m Model) {
	t.Helper()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c returned no command; the keystroke was swallowed")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c returned %T, want tea.QuitMsg", cmd())
	}
	if got := next.(Model); !got.quitting {
		t.Error("quitting is not set, so the last frame stays on screen")
	}
}

// Every one of these states swallows keys by design - the overlays take
// all of them, help dismisses itself on any of them, and a prompt treats
// one as text - which is exactly why ctrl+c has to be answered before
// the routing reaches them. A floating window with no visible way out
// that ignores ctrl+c reads as a hung program.
func TestCtrlCQuitsFromEveryModeThatSwallowsKeys(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		quitsOnCtrlC(t, testModel("a"))
	})

	t.Run("help", func(t *testing.T) {
		m := testModel("a")
		m.showHelp = true
		quitsOnCtrlC(t, m)
	})

	t.Run("theme picker", func(t *testing.T) {
		quitsOnCtrlC(t, pickerModel(t, t.TempDir()))
	})

	t.Run("find overlay, loaded", func(t *testing.T) {
		m := findModel()
		m.findState = findLoaded
		quitsOnCtrlC(t, m)
	})

	// A search in flight is the state most likely to be mistaken for a
	// hang, so it is the one that most needs the key to work.
	t.Run("find overlay, still searching", func(t *testing.T) {
		m := findModel()
		m.findState = findLoading
		quitsOnCtrlC(t, m)
	})

	t.Run("find prompt", func(t *testing.T) {
		m := testModel("a")
		m.mode = modeFind
		quitsOnCtrlC(t, m)
	})

	t.Run("search prompt", func(t *testing.T) {
		m := testModel("a")
		m.mode = modeSearch
		quitsOnCtrlC(t, m)
	})

	t.Run("command prompt", func(t *testing.T) {
		m := testModel("a")
		m.mode = modeCommand
		quitsOnCtrlC(t, m)
	})
}

// The configured quit key is still the configured quit key: answering
// ctrl+c early must not have moved the rest of the routing.
func TestTheQuitKeyStillQuits(t *testing.T) {
	m := testModel("a")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(m.keymap.Quit)})
	if cmd == nil {
		t.Fatal("the quit key returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("the quit key returned %T, want tea.QuitMsg", cmd())
	}
	if !next.(Model).quitting {
		t.Error("quitting is not set")
	}
}

// ctrl+c is deliberately not rebindable, so a config that renames the
// quit key cannot take it away.
func TestCtrlCSurvivesARebind(t *testing.T) {
	m := testModel("a")
	m.keymap.Quit = "Z"
	quitsOnCtrlC(t, m)
}
