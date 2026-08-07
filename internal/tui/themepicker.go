package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/theme"
)

// The theme picker is a floating list drawn over the panes rather than
// in place of them, which is the point: moving through it recolours the
// interface underneath, so a theme is judged on your own torrents rather
// than on a swatch.
//
// The choice lasts for the session. Writing it back to config.toml would
// mean re-encoding the file from the parsed struct, losing the comments
// and ordering of something the user wrote by hand -- so the status
// message says what to add instead.

// openThemePicker starts the picker with the cursor on the current theme.
func (m Model) openThemePicker() (tea.Model, tea.Cmd) {
	m.themeNames = theme.Available(m.themesDir)
	if len(m.themeNames) == 0 {
		cmd := m.setStatus(errStatus(fmt.Errorf("no themes found")))
		return m, cmd
	}

	// Remembered so escape can put back exactly what was on screen,
	// including a user theme that has since been edited on disk.
	m.themeSaved = m.theme
	m.themeCursor = 0
	for i, name := range m.themeNames {
		if name == m.theme.Name {
			m.themeCursor = i
			break
		}
	}
	m.themePicker = true
	return m, nil
}

// handleThemeKey routes a keystroke while the picker is open. It takes
// every key: a floating window that let keys through to the list behind
// it would move a cursor nobody can see.
func (m Model) handleThemeKey(key string) (tea.Model, tea.Cmd) {
	km := m.keymap

	switch key {
	case "esc", km.Back:
		m.themePicker = false
		return m.applyTheme(m.themeSaved), nil

	case "enter", km.Select:
		name := m.themeNames[m.themeCursor]
		m.themePicker = false
		cmd := m.setStatus(okStatus(keepThemeHint(name)))
		return m, cmd

	case km.Down, "down", km.Up, "up", km.Top, km.Bottom:
		return m.moveThemeCursor(key)
	}
	return m, nil
}

func (m Model) moveThemeCursor(key string) (tea.Model, tea.Cmd) {
	km := m.keymap
	last := len(m.themeNames) - 1

	switch key {
	case km.Down, "down":
		if m.themeCursor < last {
			m.themeCursor++
		}
	case km.Up, "up":
		if m.themeCursor > 0 {
			m.themeCursor--
		}
	case km.Top:
		m.themeCursor = 0
	case km.Bottom:
		m.themeCursor = last
	}

	// Applied as the cursor moves, which is the whole reason the picker
	// floats instead of replacing the panes.
	th, err := theme.Load(m.themeNames[m.themeCursor], m.themesDir)
	if err != nil {
		// A broken user theme leaves the current one alone: a picker
		// that repaints the screen unreadable on the way past a bad file
		// is worse than one that says so.
		cmd := m.setStatus(errStatus(err))
		return m, cmd
	}
	return m.applyTheme(th), nil
}

// applyTheme swaps in a theme's styles. Cheap enough to do on every
// keystroke -- it is a few dozen lipgloss values, built once at startup
// for exactly this reason.
func (m Model) applyTheme(th theme.Theme) Model {
	m.theme = th
	m.styles = newStyles(th)
	return m
}

// setThemeByName applies a named theme, for `:theme <name>`.
func (m Model) setThemeByName(name string) (tea.Model, tea.Cmd) {
	th, err := theme.Load(name, m.themesDir)
	if err != nil {
		cmd := m.setStatus(errStatus(err))
		return m, cmd
	}
	m = m.applyTheme(th)
	cmd := m.setStatus(okStatus(keepThemeHint(name)))
	return m, cmd
}

// keepThemeHint says how to make a pick outlast the session, since the
// picker deliberately does not rewrite config.toml.
func keepThemeHint(name string) string {
	return fmt.Sprintf("theme %q (add theme = %q to config.toml to keep it)", name, name)
}

// renderThemePicker draws the floating box. Returns the block and the
// top-left corner to splice it at.
func (m Model) renderThemePicker() (box string, x, y int) {
	const (
		title = "theme"
		hint  = "enter apply · esc cancel · j/k move"
	)

	// Wide enough for the longest name and its annotation, the title and
	// the hint, and never wider than the terminal.
	inner := lipgloss.Width(hint)
	for _, name := range m.themeNames {
		if w := lipgloss.Width(name) + len(" (current) (user)"); w > inner {
			inner = w
		}
	}
	if maxInner := m.width - 2*borderWidth - 2*panePadX; inner > maxInner {
		inner = max(1, maxInner)
	}

	// The list scrolls rather than growing past the terminal: more
	// themes than rows is an ordinary case once a user has their own.
	rows := len(m.themeNames)
	if maxRows := m.height - 1 - borderHeight - 3; rows > maxRows {
		rows = max(1, maxRows)
	}
	start, end := scrollWindow(m.themeCursor, len(m.themeNames), rows)

	var b strings.Builder
	b.WriteString(m.styles.Title.Render(title))
	b.WriteString("\n\n")
	for i := start; i < end; i++ {
		name := m.themeNames[i]

		var note string
		switch {
		case name == m.themeSaved.Name:
			note = " (current)"
		case theme.IsUserTheme(name, m.themesDir):
			note = " (user)"
		}

		line := "  " + name + note
		if i == m.themeCursor {
			line = "> " + name + note
			b.WriteString(m.styles.CursorRow.Render(padRight(line, inner)))
		} else {
			b.WriteString(m.styles.Row.Render(padRight(line, inner)))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(m.styles.Muted.Render(truncate(hint, inner)))

	box = m.styles.pane(true).Width(inner + 2*panePadX).Render(b.String())
	x, y = centre(m.width, m.height-1, lipgloss.Width(box), lipgloss.Height(box))
	return box, x, y
}
