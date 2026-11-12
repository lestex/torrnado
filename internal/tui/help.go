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

// keyPair labels an action bound to two keys, and collapses to one when
// a config file has bound both to the same thing - "h / h" would read as
// a bug in the screen rather than a choice in the file.
func keyPair(primary, alt string) string {
	if alt == "" || alt == primary {
		return displayKey(primary)
	}
	return displayKey(primary) + " / " + displayKey(alt)
}

// helpHint is the footer's standing pointer at the reference screen.
//
// It names the alternate key rather than the primary one: "?" is what
// someone who has never seen this program tries, and they are exactly who
// the hint is for.
func (m Model) helpHint() string {
	key := m.keymap.HelpAlt
	if key == "" {
		key = m.keymap.Help
	}
	if key == "" {
		return ""
	}
	return displayKey(key) + " help"
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
		{displayKey(km.TabNext) + " / " + displayKey(km.TabPrev) + ", " +
			displayKey(km.TabPieces) + "/" + displayKey(km.TabPeers) + "/" + displayKey(km.TabFiles),
			"switch the detail pane's tab"},
		{km.Search, "search torrents by name"},
		{displayKey(km.Select), "mark the torrent under the cursor; on the Files tab, skip that file"},
		{km.Detail, "move focus into the detail pane"},
		{km.Back, "focus back to the list, then clear selection, search, filter"},
	}

	actions := []helpEntry{
		{km.Remove + ", dd", "remove the marked torrents, keeping the data"},
		{km.RemoveData, "remove them and delete the data too"},
		{km.Purge, "delete their data, keeping the torrents in the list"},
		{km.Pause, "pause or resume"},
		{km.Recheck, "re-verify the data on disk"},
		{"+ / -", "raise or lower the priority of the file under the cursor"},
		{km.Command, "open the command palette"},
		{km.ToggleMouse, "hand the mouse back to the terminal, to select and copy text"},
		{km.Preview, "stream to your player: the file under the cursor, or the biggest one"},
		{km.Open, "open the torrent's folder in your file manager"},
		{km.Find, "search for new torrents to add (needs search.jackett enabled)"},
		{keyPair(km.Help, km.HelpAlt), "show this screen"},
		{km.Quit, "quit (the daemon keeps running)"},
		{"ctrl+c", "the same, from any prompt or overlay"},
	}

	// The sections are built first so the header can be chosen against
	// their real height rather than a guessed one: adding a keybind later
	// then costs the mark, not the key it was added for.
	body := m.helpBody(width, height, nav, actions, paletteHelp())

	var b strings.Builder
	b.WriteString(m.helpHeader(width, height, strings.Count(body, "\n")+1))
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
//
// It also says which build this is, and which the daemon is. The help
// screen is where someone goes to find out what they are running, and it
// replaces the panes, so the sidebar's daemon line is not on screen while
// it is open - this is the one place a daemon that is not this build can
// still be said.
//
// All of that is one line, "v0.10.1 | daemon v0.10.1", under the
// wordmark: as many lines as the mark is tall, so it costs the keys
// nothing, and a mismatch is the daemon half changing colour rather than
// a further line to make room for.
func (m Model) helpHeader(width, height, bodyLines int) string {
	mark := branding.LogoLines(branding.Logo)
	// Beside the mark, with its width and the gap taken off, so a long
	// version is cut here rather than wrapped by the pane - a wrapped
	// header line would push a row of the reference off the bottom.
	fit := width - branding.Width(branding.Logo) - 3

	words := []string{
		m.styles.Title.Render("torrnado"),
		m.styles.Muted.Render("keys & commands"),
	}
	if v := m.helpVersions(fit); v != "" {
		words = append(words, v)
	}

	rows := max(len(mark), len(words))
	if height > 0 && bodyLines+rows+2 > height {
		return m.helpTitleLine(width)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		m.styles.Title.Render(branding.Logo), "   ", strings.Join(words, "\n"))
}

// helpTitleLine is the one-line header: the title, the versions, "keys".
//
// Falls back to the bare title when the whole line would not fit the
// pane, because a wrapped title costs a row and this form exists for the
// moments when there is none to spare.
func (m Model) helpTitleLine(width int) string {
	client, daemon, skewed := m.versionParts()
	plain := "torrnado"
	if client != "" {
		plain += " " + client
		if daemon != "" {
			plain += " | " + daemon
		}
	}
	plain += " - keys"
	if width > 0 && lipgloss.Width(plain) > width {
		return m.styles.Title.Render("torrnado - keys")
	}

	line := m.styles.Title.Render("torrnado")
	if client != "" {
		line += m.styles.Muted.Render(" " + client)
		if daemon != "" {
			line += m.styles.Muted.Render(" | ") + m.daemonStyle(skewed).Render(daemon)
		}
	}
	return line + m.styles.Title.Render(" - keys")
}

// helpVersions is the line under the wordmark, or "" when there is no
// version to show. Cut to fit rather than wrapped; a line too long for
// its room loses its colours, since a truncated half-styled string is the
// worse outcome of the two.
func (m Model) helpVersions(fit int) string {
	client, daemon, skewed := m.versionParts()
	if client == "" {
		return ""
	}
	plain := client
	if daemon != "" {
		plain += " | " + daemon
	}
	if lipgloss.Width(plain) > fit {
		return m.styles.Muted.Render(truncate(plain, fit))
	}
	if daemon == "" {
		return m.styles.Muted.Render(client)
	}
	return m.styles.Muted.Render(client+" | ") + m.daemonStyle(skewed).Render(daemon)
}

// daemonStyle: the daemon's half is the one that changes colour, because a
// daemon that is not this build is a warning and not an error - it works,
// it is just not what this interface was built against.
func (m Model) daemonStyle(skewed bool) lipgloss.Style {
	if skewed {
		return m.styles.Warning
	}
	return m.styles.Muted
}

// versionParts is what the header says about the two builds: this one,
// and the daemon's as "daemon v0.10.1", each as short as it can be while
// still telling them apart. The daemon part is empty until the daemon has
// been heard from - before the first event its version is simply not known,
// and showing nothing is truer than showing a blank.
func (m Model) versionParts() (client, daemon string, skewed bool) {
	client = shortVersion(m.clientVersion)
	if client == "" {
		return "", "", false
	}
	skewed = m.versionNote != ""

	switch {
	case m.global.Version != "":
		d := shortVersion(m.global.Version)
		// The same release can be two different builds - a rebuilt client
		// over a daemon left running - and then the short forms read as
		// equal beside a warning that says they are not. Say the commits.
		if skewed && d == client {
			return m.clientVersion, "daemon " + m.global.Version, true
		}
		return client, "daemon " + d, skewed
	case skewed:
		// A daemon older than the version field says nothing at all, which
		// is not the same as not having spoken yet.
		return client, "daemon too old", true
	}
	return client, "", false
}

// shortVersion is a version string as people say it: the release, without
// the commit the build string appends - "0.10.1 (da4134de07be)" is
// "v0.10.1". Release builds are stamped without the leading v and a build
// from a working tree has one, so it is added to anything that starts with
// a digit and left off what is not a version at all, such as "dev".
func shortVersion(full string) string {
	v, _, _ := strings.Cut(full, " (")
	v = strings.TrimSpace(v)
	if v != "" && v[0] >= '0' && v[0] <= '9' {
		v = "v" + v
	}
	return v
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
