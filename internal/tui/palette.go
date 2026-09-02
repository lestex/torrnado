package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/batch"
	"github.com/lestex/torrnado/internal/config"
	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
)

// The command palette is vim's ex-mode: a ":" prompt for the things that
// need an argument and so cannot be a single keystroke - adding a
// torrent, setting a rate, moving files.
//
// It deliberately mirrors the CLI subcommands rather than inventing its
// own vocabulary, so knowing one teaches the other.

// paletteCommand is one line of the help screen's COMMANDS section,
// together with every word execCommand answers to for it.
//
// The list lives here rather than in help.go because a vocabulary and the
// documentation of it drift the moment they sit in different files -
// adding a case below is meant to be the same edit as adding the line
// people read. A test parses execCommand's switch and fails if the two
// ever disagree, in either direction, so a command cannot be added
// without being documented or documented without existing.
type paletteCommand struct {
	names []string // what execCommand accepts, aliases included
	usage string   // how the line reads on the help screen
	desc  string
	// completesPaths marks a command whose last argument names something
	// on disk, so Tab can complete it. Kept here rather than as a list
	// somewhere else, so a new path-taking command cannot quietly miss
	// out on completion.
	completesPaths bool
}

var paletteCommands = []paletteCommand{
	{[]string{"add"}, ":add <magnet|file|dir>", "add torrents - anything `torrnado add` takes", true},
	{[]string{"pause", "resume"}, ":pause / :resume", "pause or resume the marked torrents", false},
	{[]string{"rm", "remove", "rm!", "remove!"}, ":rm / :rm!", "remove them; ! deletes the data too", false},
	{[]string{"purge"}, ":purge", "delete their data, keeping them in the list", false},
	{[]string{"recheck"}, ":recheck", "re-verify the data on disk", false},
	{[]string{"limit-up"}, ":limit-up <rate>", "global upload cap: 500k, 2M, unlimited", false},
	{[]string{"limit-down"}, ":limit-down <rate>", "global download cap", false},
	{[]string{"move"}, ":move <dir>", "move the torrent under the cursor", true},
	{[]string{"sort"}, ":sort <column> [desc]", "name, size, progress, ratio, eta, added, down, up", false},
	{[]string{"theme"}, ":theme [name]", "open the theme picker, or apply one by name", false},
	{[]string{"help"}, ":help", "show this screen", false},
	{[]string{"q", "quit"}, ":q", "quit (the daemon keeps running)", false},
}

func (m Model) execCommand(line string) (tea.Model, tea.Cmd) {
	fields := splitArgs(line)
	if len(fields) == 0 {
		return m, nil
	}
	name, args := fields[0], fields[1:]

	switch name {
	case "add":
		if len(args) == 0 {
			return m, func() tea.Msg { return errStatus(fmt.Errorf("add: needs a magnet, file or directory")) }
		}
		return m, addBatchCmd(m.client, expandPathArgs(args))

	case "rm", "remove":
		return m.removeTargets(m.visibleTorrents(), false)

	case "rm!", "remove!":
		return m.removeTargets(m.visibleTorrents(), true)

	case "pause":
		return m.setPausedTargets(true)

	case "resume":
		return m.setPausedTargets(false)

	case "purge":
		return m.purgeTargets(m.visibleTorrents())

	case "recheck":
		return m.recheckTargets(m.visibleTorrents())

	case "limit-up", "limit-down":
		if len(args) != 1 {
			return m, func() tea.Msg { return errStatus(fmt.Errorf("%s: needs a rate, e.g. 500k or unlimited", name)) }
		}
		return m, limitCmd(m.client, name == "limit-up", args[0])

	case "move":
		if len(args) != 1 {
			return m, func() tea.Msg { return errStatus(fmt.Errorf("move: needs a directory")) }
		}
		t, ok := m.cursorTorrent()
		if !ok {
			return m, nil
		}
		return m, moveCmd(m.client, t.ID, expandPathArgs(args)[0])

	case "theme":
		// With no argument the picker opens, which is how you find out
		// what the names are; with one it applies directly, for when
		// you already know.
		if len(args) == 0 {
			return m.openThemePicker()
		}
		return m.setThemeByName(args[0])

	case "sort":
		if len(args) == 0 {
			return m, func() tea.Msg {
				return errStatus(fmt.Errorf("sort: needs a column (name, size, progress, ratio, eta, added, down, up)"))
			}
		}
		mode, ok := ParseSortMode(args[0])
		if !ok {
			return m, func() tea.Msg { return errStatus(fmt.Errorf("sort: unknown column %q", args[0])) }
		}
		m.sortBy = mode
		m.sortDesc = len(args) > 1 && args[1] == "desc"
		m.clampCursor(len(m.visibleTorrents()))
		return m, nil

	case "help":
		// The same screen the help key opens, reachable from the palette
		// for someone who came looking for the command list and found the
		// prompt first - which is where ":" habits lead.
		m.showHelp = true
		return m, nil

	case "q", "quit":
		m.quitting = true
		return m, tea.Quit
	}

	return m, func() tea.Msg { return errStatus(fmt.Errorf("unknown command %q", name)) }
}

// splitArgs splits a palette line into a command and its arguments,
// treating quoted runs as one argument and dropping the quotes.
//
// The palette is not a shell - nothing here expands anything - but the
// habits that get people to it are shell habits. A magnet uri has to be
// quoted in zsh, because the `?` and `&` in it are glob and job-control
// characters, so `:add 'magnet:?xt=...'` is what a person types. Passed
// through with its quotes still attached, that argument does not look
// like a magnet to the batch expander, which falls through to treating it
// as a file glob and reports matching no files - a confusing answer to a
// command that was very nearly right.
//
// Quotes also make an argument with a space in it expressible at all:
// `:move '/media/big disk'` was impossible when this split on whitespace.
//
// Only ' and " are special, and only as a matched pair. No escapes: a
// path containing a quote character is rare enough to leave, and an
// escape syntax nobody asked for is another thing to get wrong. An
// unclosed quote takes the rest of the line rather than being an error,
// since the intent is never in doubt.
func splitArgs(line string) []string {
	var (
		args  []string
		cur   strings.Builder
		quote rune // the quote character we are inside, or 0
		open  bool // whether cur holds an argument, even an empty one
	)
	for _, r := range strings.TrimSpace(line) {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			// An empty pair of quotes is still an argument, so remember
			// that one was started.
			quote, open = r, true
		case r == ' ' || r == '\t':
			if open || cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
				open = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if open || cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args
}

func (m Model) setPausedTargets(paused bool) (tea.Model, tea.Cmd) {
	targets := m.targets(m.visibleTorrents())
	if len(targets) == 0 {
		return m, nil
	}
	ids := make([]engine.TorrentID, len(targets))
	for i, t := range targets {
		ids[i] = t.ID
	}
	return m, setPausedCmd(m.client, ids, paused)
}

// addBatchCmd expands the arguments the same way `torrnado add` does, so
// the two accept exactly the same things.
func addBatchCmd(c *ipc.Client, args []string) tea.Cmd {
	return func() tea.Msg {
		sources, err := batch.Expand(args)
		if err != nil {
			return errStatus(err)
		}
		ids, failures, err := c.AddBatch(sources, engine.AddOpts{})
		if err != nil {
			return errStatus(err)
		}
		if len(failures) > 0 {
			return errStatus(fmt.Errorf("added %d, %d failed: %s",
				len(ids), len(failures), failures[0]))
		}
		return okStatus(fmt.Sprintf("added %d torrent(s)", len(ids)))
	}
}

func limitCmd(c *ipc.Client, up bool, rate string) tea.Cmd {
	return func() tea.Msg {
		bps, err := config.ParseRate(rate)
		if err != nil {
			return errStatus(err)
		}
		if up {
			err = c.SetGlobalUploadLimit(bps)
		} else {
			err = c.SetGlobalDownloadLimit(bps)
		}
		if err != nil {
			return errStatus(err)
		}
		dir := "download"
		if up {
			dir = "upload"
		}
		return okStatus(fmt.Sprintf("global %s limit: %s", dir, config.Rate(bps)))
	}
}

func moveCmd(c *ipc.Client, id engine.TorrentID, dir string) tea.Cmd {
	return func() tea.Msg {
		if err := c.MoveStorage(id, dir); err != nil {
			return errStatus(err)
		}
		return okStatus("moved to " + dir)
	}
}
