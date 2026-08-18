package tui

import (
	"strings"
	"testing"
)

func footerModel(t *testing.T) Model {
	t.Helper()
	m := testModel("a")
	m.styles = newStyles(loadTestTheme(t))
	m.width, m.height = 100, 30
	return m
}

// A standing pointer at the reference, in the corner of a line that was
// empty anyway. Without it the only way to the keys is a key.
func TestFooterPointsAtTheHelpScreenWhenIdle(t *testing.T) {
	m := footerModel(t)

	got := m.renderFooter(layout(m.width, m.height))

	if !strings.Contains(got, "? help") {
		t.Errorf("the idle footer does not point at the help screen:\n%q", got)
	}
}

// The hint answers a question nobody asked; a status message answers one
// they did. When only one fits, it is not the hint.
func TestFooterGivesTheHintUpForAStatusMessage(t *testing.T) {
	m := footerModel(t)
	m.status = "added 1 torrent(s)"

	got := m.renderFooter(layout(m.width, m.height))

	if !strings.Contains(got, m.status) {
		t.Errorf("the status message is missing:\n%q", got)
	}
	if strings.Contains(got, "? help") {
		t.Errorf("the hint stayed on the line beside a status message:\n%q", got)
	}
}

// The prompt and the help screen own the whole line while they are up.
func TestFooterDropsTheHintWhereTheLineIsSpokenFor(t *testing.T) {
	m := footerModel(t)
	m.showHelp = true
	if got := m.renderFooter(layout(m.width, m.height)); strings.Contains(got, "? help") {
		t.Errorf("the hint was drawn over the help screen's own footer:\n%q", got)
	}

	m = footerModel(t)
	m.mode = modeCommand
	m.commandBuf = "add"
	if got := m.renderFooter(layout(m.width, m.height)); strings.Contains(got, "? help") {
		t.Errorf("the hint was drawn over the command prompt:\n%q", got)
	}
}

// A narrow terminal spends its cells on the numbers, not on the hint.
func TestFooterDropsTheHintWhenItDoesNotFit(t *testing.T) {
	m := footerModel(t)
	m.width = 44

	got := m.renderFooter(layout(m.width, m.height))

	if strings.Contains(got, "? help") {
		t.Errorf("the hint was drawn on a footer too narrow for it:\n%q", got)
	}
}
