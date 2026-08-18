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
		{km.Command, "open the command palette"},
		{km.Preview, "stream to your player: the file under the cursor, or the biggest one"},
		{km.Open, "open the torrent's folder in your file manager"},
		{km.Help, "show this screen"},
		{km.Quit, "quit (the daemon keeps running)"},
	}

	// The sections are built first so the header can be chosen against
	// their real height rather than a guessed one: adding a keybind later
	// then costs the mark, not the key it was added for.
	body := m.helpBody(width, height, nav, actions, paletteHelp())

	var b strings.Builder
	b.WriteString(m.helpHeader(height, strings.Count(body, "\n")+1))
	b.WriteString("\n\n")
	b.WriteString(body)

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

// paletteHelp renders the palette's own command list as help entries.
//
// The vocabulary itself lives in palette.go, beside the switch that reads
// it, so this screen cannot list a command that does not exist.
func paletteHelp() []helpEntry {
	entries := make([]helpEntry, len(paletteCommands))
	for i, c := range paletteCommands {
		entries[i] = helpEntry{c.usage, c.desc}
	}
	return entries
}

// helpBody lays the sections out in one column, or two when that is the
// only way they fit.
//
// Stacked, the three sections run past the bottom of an 80x24 terminal,
// and the commands - the section furthest down - are the ones clipped
// away entirely. Side by side they are roughly half as tall. One column
// reads better whenever there is room for it, so the split is what
// happens when there is not, rather than what happens on a wide screen:
// a tall terminal keeps the full descriptions at the same width where a
// short one gives them up to keep the commands visible.
func (m Model) helpBody(width, height int, nav, actions, commands []helpEntry) string {
	var stacked strings.Builder
	writeHelpSection(&stacked, m, "NAVIGATION", nav, width)
	writeHelpSection(&stacked, m, "ACTIONS", actions, width)
	writeHelpSection(&stacked, m, "COMMANDS", commands, width)
	one := strings.TrimRight(stacked.String(), "\n") + "\n"

	// Room for the one-line title, the blank line under it and the note at
	// the foot - the least the rest of the screen can be drawn in. height
	// is 0 when unbounded, which is all the room there is.
	if width < helpTwoColumnWidth || height <= 0 || strings.Count(one, "\n")+3 <= height {
		return one
	}

	// Split by what each side needs rather than down the middle: the
	// commands carry their arguments in the key column where a keybind is
	// one keystroke, and the keybind descriptions are the longer prose. An
	// even split truncates one column while the other keeps whitespace it
	// has no use for.
	leftNeed := helpNaturalWidth(nav, actions)
	rightNeed := helpNaturalWidth(commands)
	leftW := (width - helpGutter) * leftNeed / (leftNeed + rightNeed)
	if leftNeed+rightNeed+helpGutter <= width {
		leftW = leftNeed
	}
	rightW := width - leftW - helpGutter

	var left, right strings.Builder
	writeHelpSection(&left, m, "NAVIGATION", nav, leftW)
	writeHelpSection(&left, m, "ACTIONS", actions, leftW)
	writeHelpSection(&right, m, "COMMANDS", commands, rightW)

	// Width() on the left block puts the gutter at a fixed column rather
	// than after its longest line, so the two columns line up. Nothing
	// wraps: every line was already truncated to leftW.
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftW+helpGutter).Render(strings.TrimRight(left.String(), "\n")),
		strings.TrimRight(right.String(), "\n")) + "\n"
}

// helpNaturalWidth is the width the given sections would take with
// nothing truncated: the widest key, the gap either side of it, and the
// longest description.
func helpNaturalWidth(sections ...[]helpEntry) int {
	keyW, descW := 0, 0
	for _, entries := range sections {
		for _, e := range entries {
			if w := lipgloss.Width(e.key); w > keyW {
				keyW = w
			}
			if w := lipgloss.Width(e.desc); w > descW {
				descW = w
			}
		}
	}
	// Matching writeHelpSection's own two-space indent and separator.
	return keyW + descW + 4
}

// helpTwoColumnWidth is where two columns start being worth more than the
// room they cost: below it the descriptions truncate to nothing and the
// screen is less readable split than stacked.
const (
	helpTwoColumnWidth = 100
	helpGutter         = 4
)

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
