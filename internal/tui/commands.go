package tui

import (
	"fmt"
	"path"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
	"github.com/lestex/torrnado/internal/player"
)

// Each of these returns a tea.Cmd: a function bubbletea runs off the main
// loop, whose result comes back as a message. That is how an RPC that
// might block for a moment does not freeze the interface -- Update stays
// a pure, fast state transition and the waiting happens elsewhere.

func removeCmd(c *ipc.Client, ids []engine.TorrentID, deleteData bool) tea.Cmd {
	return func() tea.Msg {
		var failed int
		for _, id := range ids {
			if err := c.Remove(id, deleteData); err != nil {
				failed++
			}
		}
		if failed > 0 {
			return errStatus(fmt.Errorf("removed %d/%d torrents (%d failed)",
				len(ids)-failed, len(ids), failed))
		}
		verb := "removed"
		if deleteData {
			verb = "removed (with data)"
		}
		return okStatus(fmt.Sprintf("%s %d torrent(s)", verb, len(ids)))
	}
}

// pauseCmd toggles each target's pause state.
//
// It reads Snapshot.Paused rather than deriving it from State: State
// reports "checking" while a recheck runs, so anything inferring
// pausedness from it would decide a paused torrent was running and only
// ever pause it again.
func pauseCmd(c *ipc.Client, targets []engine.TorrentSnapshot) tea.Cmd {
	return func() tea.Msg {
		var failed int
		for _, t := range targets {
			if err := c.SetPaused(t.ID, !t.Paused); err != nil {
				failed++
			}
		}
		if failed > 0 {
			return errStatus(fmt.Errorf("toggled %d/%d torrents (%d failed)",
				len(targets)-failed, len(targets), failed))
		}
		return okStatus(fmt.Sprintf("toggled pause on %d torrent(s)", len(targets)))
	}
}

func recheckCmd(c *ipc.Client, ids []engine.TorrentID) tea.Cmd {
	return func() tea.Msg {
		var failed int
		for _, id := range ids {
			if err := c.ForceRecheck(id); err != nil {
				failed++
			}
		}
		if failed > 0 {
			return errStatus(fmt.Errorf("rechecked %d/%d torrents (%d failed)",
				len(ids)-failed, len(ids), failed))
		}
		return okStatus(fmt.Sprintf("rechecking %d torrent(s)", len(ids)))
	}
}

// setPausedCmd sets an absolute state, unlike pauseCmd which toggles.
// The palette's :pause has to mean pause whatever the current state is.
func setPausedCmd(c *ipc.Client, ids []engine.TorrentID, paused bool) tea.Cmd {
	return func() tea.Msg {
		var failed int
		for _, id := range ids {
			if err := c.SetPaused(id, paused); err != nil {
				failed++
			}
		}
		if failed > 0 {
			return errStatus(fmt.Errorf("updated %d/%d torrents (%d failed)",
				len(ids)-failed, len(ids), failed))
		}
		verb := "resumed"
		if paused {
			verb = "paused"
		}
		return okStatus(fmt.Sprintf("%s %d torrent(s)", verb, len(ids)))
	}
}

func loadDetail(c *ipc.Client, id engine.TorrentID) tea.Cmd {
	return func() tea.Msg {
		d, err := c.Detail(id)
		return detailLoadedMsg{detail: d, err: err}
	}
}

// setPriorityCmd changes one file's priority, then refetches the detail
// so the pane shows what the daemon actually stored rather than what was
// asked for -- the two differ, since the library has no "low".
func setPriorityCmd(c *ipc.Client, id engine.TorrentID, fileIndex int, prio engine.Priority) tea.Cmd {
	return func() tea.Msg {
		if err := c.SetFilePriority(id, fileIndex, prio); err != nil {
			return errStatus(err)
		}
		d, err := c.Detail(id)
		return detailLoadedMsg{detail: d, err: err}
	}
}

// previewCmd asks the daemon for a stream URL and opens it in the
// configured player. The daemon resumes the torrent and raises the file's
// priority as part of handing the URL over.
func previewCmd(c *ipc.Client, playerCmd string, id engine.TorrentID, f engine.FileInfo) tea.Cmd {
	return func() tea.Msg {
		url, err := c.PreviewURL(id, f.Index)
		if err != nil {
			return errStatus(err)
		}
		if err := player.Launch(playerCmd, url); err != nil {
			return errStatus(err)
		}
		return okStatus(fmt.Sprintf("playing %s", path.Base(f.Path)))
	}
}
