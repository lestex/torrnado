package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// runCommand executes a palette line and returns whatever status message
// the resulting command produced, if any.
func runCommand(t *testing.T, m Model, line string) statusMsg {
	t.Helper()
	next, cmd := m.execCommand(line)
	_ = next
	if cmd == nil {
		return statusMsg{}
	}
	msg := cmd()
	s, _ := msg.(statusMsg)
	return s
}

func TestUnknownCommandIsReported(t *testing.T) {
	got := runCommand(t, testModel("a"), "frobnicate")

	if !got.isErr {
		t.Fatalf("unknown command produced %+v, want an error", got)
	}
	if got.text == "" {
		t.Error("the error should say something")
	}
}

// A command needing an argument has to say so rather than doing nothing.
func TestCommandsRequiringArgumentsComplain(t *testing.T) {
	for _, line := range []string{"add", "limit-up", "limit-down", "move"} {
		if got := runCommand(t, testModel("a"), line); !got.isErr {
			t.Errorf("%q with no argument produced %+v, want an error", line, got)
		}
	}
}

func TestQuitCommand(t *testing.T) {
	for _, line := range []string{"q", "quit"} {
		next, _ := testModel("a").execCommand(line)
		if !next.(Model).quitting {
			t.Errorf("%q did not quit", line)
		}
	}
}

// An empty line is a no-op, not an error: pressing : then enter is a
// mistake worth forgiving silently.
func TestEmptyCommandDoesNothing(t *testing.T) {
	m := testModel("a")
	next, cmd := m.execCommand("   ")
	if cmd != nil {
		t.Error("an empty command should produce no work")
	}
	if next.(Model).quitting {
		t.Error("an empty command should not quit")
	}
}

// The prompt collects text and runs it on enter; escape abandons it.
func TestCommandPromptCollectsAndCancels(t *testing.T) {
	m := testModel("a")
	m = press(m, ":")
	if m.mode != modeCommand {
		t.Fatal(": did not open the command prompt")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("quit")})
	m = next.(Model)
	if m.commandBuf != "quit" {
		t.Errorf("buffer = %q, want the typed text", m.commandBuf)
	}

	m = press(m, "esc")
	if m.mode != modeNormal || m.commandBuf != "" {
		t.Errorf("escape left mode=%v buffer=%q", m.mode, m.commandBuf)
	}
	if m.quitting {
		t.Error("escaping the prompt should not have run the command")
	}
}
