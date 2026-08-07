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
// need an argument and so cannot be a single keystroke -- adding a
// torrent, setting a rate, moving files.
//
// It deliberately mirrors the CLI subcommands rather than inventing its
// own vocabulary, so knowing one teaches the other.

func (m Model) execCommand(line string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return m, nil
	}
	name, args := fields[0], fields[1:]

	switch name {
	case "add":
		if len(args) == 0 {
			return m, func() tea.Msg { return errStatus(fmt.Errorf("add: needs a magnet, file or directory")) }
		}
		return m, addBatchCmd(m.client, args)

	case "rm", "remove":
		return m.removeTargets(m.visibleTorrents(), false)

	case "rm!", "remove!":
		return m.removeTargets(m.visibleTorrents(), true)

	case "pause":
		return m.setPausedTargets(true)

	case "resume":
		return m.setPausedTargets(false)

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
		return m, moveCmd(m.client, t.ID, args[0])

	case "q", "quit":
		m.quitting = true
		return m, tea.Quit
	}

	return m, func() tea.Msg { return errStatus(fmt.Errorf("unknown command %q", name)) }
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
