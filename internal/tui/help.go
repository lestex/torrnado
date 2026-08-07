package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
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
// so it cannot drift from what is actually bound -- including any
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
		{km.Preview, "stream the selected file to your player, even while downloading"},
		{km.Open, "open the torrent's folder in your file manager"},
		{km.Help, "show this screen"},
		{km.Quit, "quit (the daemon keeps running)"},
	}

	var b strings.Builder
	b.WriteString(m.styles.Title.Render("torrnado -- keys"))
	b.WriteString("\n\n")
	writeHelpSection(&b, m, "NAVIGATION", nav, width)
	writeHelpSection(&b, m, "ACTIONS", actions, width)

	lines := strings.Split(b.String(), "\n")
	note := "Keys reflect any [keybinds] overrides in config.toml."
	if height > 0 && len(lines) >= height {
		// Overflowing would push the pane's own border off the screen.
		lines = lines[:height-1]
		note = "(clipped -- resize the terminal)"
	}
	lines = append(lines, m.styles.Muted.Render(truncate(note, width)))
	return strings.Join(lines, "\n")
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
