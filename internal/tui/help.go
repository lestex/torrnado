package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/branding"
)

type helpEntry struct {
	key  string
	desc string
}

// displayKey substitutes a readable label for keys that render as
// nothing, such as a literal space.
func displayKey(key string) string {
	if key == " " {
		return "space"
	}
	return key
}

// renderHelp draws the keybind reference.
//
// It is generated from the live keymap rather than written out by hand,
// so it cannot drift from what is actually bound - including any
// rebinding from a config file, which a hardcoded list would misreport.
func (m Model) renderHelp(width, height int) string {
	km := m.keymap

	nav := []helpEntry{
		{km.Up + " / " + km.Down, "move up / down"},
		{km.Top + " / " + km.Bottom, "jump to top / bottom"},
		{displayKey(km.FocusNext) + " / " + displayKey(km.FocusPrev), "move focus between the panes"},
		{displayKey(km.TabNext) + " / " + displayKey(km.TabPrev) + ", 1-3", "switch the detail pane's tab"},
		{km.Search, "search torrents by name"},
		{displayKey(km.Select), "mark the row under the cursor"},
		{km.Detail, "move focus into the detail pane"},
		{km.Back, "focus back to the list, then clear selection, search, filter"},
	}

	actions := []helpEntry{
		{km.Remove + ", dd", "remove the marked torrents, keeping the data"},
		{km.RemoveData, "remove them and delete the data too"},
		{km.Purge, "delete their data, keeping the torrents in the list"},
		{km.Pause, "pause or resume"},
		{km.Recheck, "re-verify the data on disk"},
		{km.Command, "command palette: :add, :theme, :limit-up, :move, :q"},
		{km.Preview, "stream to your player: the file under the cursor, or the biggest one"},
		{km.Open, "open the torrent's folder in your file manager"},
		{km.Help, "show this screen"},
		{km.Quit, "quit (the daemon keeps running)"},
	}

	// The sections are built first so the header can be chosen against
	// their real height rather than a guessed one: adding a keybind later
	// then costs the mark, not the key it was added for.
	var body strings.Builder
	writeHelpSection(&body, m, "NAVIGATION", nav, width)
	writeHelpSection(&body, m, "ACTIONS", actions, width)

	var b strings.Builder
	b.WriteString(m.helpHeader(height, strings.Count(body.String(), "\n")+1))
	b.WriteString("\n\n")
	b.WriteString(body.String())

	lines := strings.Split(b.String(), "\n")
	note := "Keys reflect any [keybinds] overrides in config.toml."
	if height > 0 && len(lines) >= height {
		// Overflowing would push the pane's own border off the screen.
		lines = lines[:height-1]
		note = "(clipped - resize the terminal)"
	}
	lines = append(lines, m.styles.Muted.Render(truncate(note, width)))
	return strings.Join(lines, "\n")
}

// helpHeader is the mark beside the wordmark, dropped for a one-line
// title when the keys would not otherwise fit: this screen exists to show
// the reference, not the logo.
func (m Model) helpHeader(height, bodyLines int) string {
	// The mark's rows, the blank line under it, and the note at the foot.
	mark := branding.LogoLines(branding.Logo)
	if height > 0 && bodyLines+len(mark)+2 > height {
		return m.styles.Title.Render("torrnado - keys")
	}
	words := m.styles.Title.Render("torrnado") + "\n" +
		m.styles.Muted.Render("keys & commands")
	return lipgloss.JoinHorizontal(lipgloss.Center,
		m.styles.Title.Render(branding.Logo), "   ", words)
}

func writeHelpSection(b *strings.Builder, m Model, title string, entries []helpEntry, width int) {
	b.WriteString(m.styles.Accent.Render(title))
	b.WriteString("\n")

	keyWidth := 0
	for _, e := range entries {
		if w := lipgloss.Width(e.key); w > keyWidth {
			keyWidth = w
		}
	}
	for _, e := range entries {
		b.WriteString("  ")
		b.WriteString(m.styles.Row.Render(padRight(e.key, keyWidth)))
		b.WriteString("  ")
		// Cut rather than wrapped: a wrapped line would push the section
		// below it off the bottom of the pane.
		b.WriteString(m.styles.Muted.Render(truncate(e.desc, width-keyWidth-4)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}
