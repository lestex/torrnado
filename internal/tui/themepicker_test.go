package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/theme"
)

// pickerModel is a model with the picker open over a couple of torrents,
// themed from an empty directory so only the built-ins are listed.
func pickerModel(t *testing.T, themesDir string) Model {
	t.Helper()

	th, err := theme.Load("dracula", themesDir)
	if err != nil {
		t.Fatalf("load dracula: %v", err)
	}
	m := testModel("a", "b")
	m.themesDir = themesDir
	m.width, m.height = 120, 40
	m = m.applyTheme(th)

	next, _ := m.openThemePicker()
	return next.(Model)
}

func TestOpeningThePickerStartsOnTheCurrentTheme(t *testing.T) {
	m := pickerModel(t, t.TempDir())

	if !m.themePicker {
		t.Fatal("the picker did not open")
	}
	if got := m.themeNames[m.themeCursor]; got != "dracula" {
		t.Errorf("cursor starts on %q, want the applied theme", got)
	}
	if len(m.themeNames) < 2 {
		t.Fatalf("only %d themes listed", len(m.themeNames))
	}
}

// Moving the cursor applies the theme immediately - that is the reason
// the picker floats over the panes instead of replacing them.
func TestMovingThePickerCursorAppliesTheTheme(t *testing.T) {
	m := pickerModel(t, t.TempDir())
	before := m.styles.theme.Name

	next, _ := m.handleThemeKey(m.keymap.Down)
	m = next.(Model)

	if m.themeNames[m.themeCursor] == before {
		t.Fatal("the cursor did not move")
	}
	if m.styles.theme.Name != m.themeNames[m.themeCursor] {
		t.Errorf("styles are %q with the cursor on %q",
			m.styles.theme.Name, m.themeNames[m.themeCursor])
	}
}

// Escape puts back exactly what was on screen when the picker opened.
func TestEscapeRestoresTheThemeThePickerOpenedWith(t *testing.T) {
	m := pickerModel(t, t.TempDir())
	original := m.theme.Name

	next, _ := m.handleThemeKey(m.keymap.Down)
	next, _ = next.(Model).handleThemeKey(m.keymap.Down)
	moved := next.(Model)
	if moved.theme.Name == original {
		t.Fatal("the theme did not change while moving")
	}

	next, _ = moved.handleThemeKey("esc")
	m = next.(Model)

	if m.themePicker {
		t.Error("escape left the picker open")
	}
	if m.theme.Name != original {
		t.Errorf("theme is %q after escape, want %q", m.theme.Name, original)
	}
	if m.styles.theme.Name != original {
		t.Errorf("styles are %q after escape, want %q", m.styles.theme.Name, original)
	}
}

func TestEnterKeepsTheHighlightedTheme(t *testing.T) {
	m := pickerModel(t, t.TempDir())

	next, _ := m.handleThemeKey(m.keymap.Down)
	m = next.(Model)
	chosen := m.themeNames[m.themeCursor]

	next, cmd := m.handleThemeKey("enter")
	m = next.(Model)

	if m.themePicker {
		t.Error("enter left the picker open")
	}
	if m.theme.Name != chosen {
		t.Errorf("theme is %q, want the chosen %q", m.theme.Name, chosen)
	}
	// And it says how to make the choice outlast the session, since the
	// picker deliberately does not write to config.toml.
	//
	// Read off the model rather than by running the command: what comes
	// back from setStatus is the timer that clears the message, and
	// running a tea.Tick blocks for its whole duration.
	if cmd == nil {
		t.Error("applying a theme scheduled no expiry for its message")
	}
	if !strings.Contains(m.status, "config.toml") {
		t.Errorf("status %q does not say how to keep the theme", m.status)
	}
}

func TestPickerCursorStopsAtBothEnds(t *testing.T) {
	m := pickerModel(t, t.TempDir())
	km := m.keymap

	next, _ := m.handleThemeKey(km.Top)
	m = next.(Model)
	next, _ = m.handleThemeKey(km.Up)
	if got := next.(Model).themeCursor; got != 0 {
		t.Errorf("cursor went to %d past the top", got)
	}

	next, _ = m.handleThemeKey(km.Bottom)
	m = next.(Model)
	last := len(m.themeNames) - 1
	next, _ = m.handleThemeKey(km.Down)
	if got := next.(Model).themeCursor; got != last {
		t.Errorf("cursor went to %d past the end (%d)", got, last)
	}
}

// A user theme with a bad key must not repaint the screen unreadable on
// the way past it.
func TestABrokenUserThemeIsReportedAndSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "aaa-broken.toml"),
		[]byte("nonsense = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := pickerModel(t, dir)
	before := m.theme.Name

	// aaa-broken sorts first, so Top lands on it.
	next, _ := m.handleThemeKey(m.keymap.Top)
	m = next.(Model)

	if m.theme.Name != before {
		t.Errorf("a broken theme was applied: now %q", m.theme.Name)
	}
	if !m.statusIsErr {
		t.Errorf("a broken theme was passed over without an error: %q", m.status)
	}
	if !strings.Contains(m.status, "aaa-broken") {
		t.Errorf("the error does not name the theme that failed: %q", m.status)
	}
}

func TestSetThemeByName(t *testing.T) {
	m := pickerModel(t, t.TempDir())
	next, _ := m.handleThemeKey("esc")
	m = next.(Model)

	next, _ = m.setThemeByName("nord")
	m = next.(Model)
	if m.theme.Name != "nord" {
		t.Errorf("theme is %q, want nord", m.theme.Name)
	}

	if !strings.Contains(m.status, "config.toml") {
		t.Errorf("status %q does not say how to keep the theme", m.status)
	}

	// An unknown name changes nothing and says so.
	next, _ = m.setThemeByName("no-such-theme")
	m = next.(Model)
	if m.theme.Name != "nord" {
		t.Errorf("an unknown theme changed the applied one to %q", m.theme.Name)
	}
	if !m.statusIsErr {
		t.Errorf("an unknown theme did not report an error: %q", m.status)
	}
	// theme.Load's message lists what is available, which is the useful
	// half of the answer.
	if !strings.Contains(m.status, "dracula") {
		t.Errorf("the error does not list the built-ins: %q", m.status)
	}
}

// The float must not change the frame's shape, at any terminal size the
// TUI will draw at.
func TestThePickerFitsTheFrame(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {minWidth, minHeight}, {200, 60}, {80, 24}} {
		m := pickerModel(t, t.TempDir())
		m.width, m.height = size[0], size[1]

		frame := m.View()
		lines := strings.Split(frame, "\n")

		if len(lines) > m.height {
			t.Errorf("%dx%d: frame is %d lines", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > m.width {
				t.Errorf("%dx%d: line %d is %d columns", size[0], size[1], i, w)
			}
		}
		if !strings.Contains(frame, "theme") {
			t.Errorf("%dx%d: the picker is not on screen", size[0], size[1])
		}
	}
}

// Every key belongs to the picker while it is open, or a keystroke aimed
// at the float moves a cursor nobody can see behind it.
func TestThePickerSwallowsOtherKeys(t *testing.T) {
	m := pickerModel(t, t.TempDir())
	cursorBefore := m.cursor

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(m.keymap.Search)})
	m = next.(Model)

	if m.mode != modeNormal {
		t.Errorf("a key reached the list and opened mode %v", m.mode)
	}
	if m.cursor != cursorBefore {
		t.Errorf("the list cursor moved to %d behind the picker", m.cursor)
	}
	if !m.themePicker {
		t.Error("the picker closed on an unrelated key")
	}
}
