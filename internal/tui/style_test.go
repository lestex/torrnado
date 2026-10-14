package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/lestex/torrnado/internal/theme"
)

// forceTruecolor makes the styles below emit escape sequences at all.
// lipgloss asks termenv what the terminal supports, and under `go test`
// that is nothing - every colour renders as bare text, which would make
// this test pass whatever newStyles did.
func forceTruecolor(t *testing.T) {
	t.Helper()
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(before) })
}

// The terminal theme has to reach the screen as text with no colour on
// it, not as text coloured to look like the terminal's default. Only the
// first leaves the cell alone, and only leaving it alone follows a
// terminal whose scheme is light, or changes under the TUI.
//
// This is the test that fails if newStyles stops calling Resolved, or if
// a style starts painting the background: either shows up as an escape
// sequence around ordinary text.
func TestTerminalThemeRendersWithoutColorSequences(t *testing.T) {
	forceTruecolor(t)

	th, err := theme.Load("terminal", t.TempDir())
	if err != nil {
		t.Fatalf("load terminal theme: %v", err)
	}
	s := newStyles(th)

	for name, style := range map[string]lipgloss.Style{
		"Base":        s.Base,
		"Row":         s.Row,
		"StatusBar":   s.StatusBar,
		"SidebarItem": s.SidebarItem,
	} {
		if got := style.Render("x"); got != "x" {
			t.Errorf("%s renders ordinary text as %q, want it uncoloured so the "+
				"terminal's own foreground shows through", name, got)
		}
	}

	// The semantic colours are the other half: they name a slot in the
	// emulator's palette, so they must still emit something.
	if got := s.Error.Render("x"); !strings.Contains(got, "\x1b[") {
		t.Errorf("Error renders as %q, want an ANSI colour from the terminal's palette", got)
	}
}

// A hex theme is unaffected by any of this: it still paints what it says.
func TestHexThemeStillColorsItsText(t *testing.T) {
	forceTruecolor(t)

	th, err := theme.Load("dracula", t.TempDir())
	if err != nil {
		t.Fatalf("load dracula: %v", err)
	}

	if got := newStyles(th).Row.Render("x"); !strings.Contains(got, "\x1b[") {
		t.Errorf("dracula renders a row as %q, want its own foreground colour", got)
	}
}
