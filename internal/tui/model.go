// Package tui is torrnado's terminal interface, built on bubbletea.
//
// bubbletea programs are a loop over three things: a Model holding all
// the state, an Update that takes a message and returns a new Model, and
// a View that renders the Model to a string. Nothing draws to the screen
// directly; the framework diffs the returned string against what is on
// screen already.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
	"github.com/lestex/torrnado/internal/theme"
)

// Model is the root bubbletea model.
//
// It never talks to the torrent library: everything arrives through the
// ipc client, so the same Model works whether the daemon is one it
// spawned or one that was already running.
type Model struct {
	client *ipc.Client
	events <-chan engine.Event

	styles styles

	// torrents is the latest state pushed by the daemon, kept whole so
	// filtering and sorting stay a property of the view rather than
	// something that loses information.
	torrents []engine.TorrentSnapshot
	global   engine.GlobalStats

	// filter narrows the list to one status. The full set is kept above,
	// so changing it is a cheap re-render rather than a refetch.
	filter statusFilter

	status      string
	statusIsErr bool

	width, height int
	quitting      bool
}

// New builds the initial Model. client and its Events() channel must
// already be connected.
func New(client *ipc.Client, th theme.Theme) Model {
	return Model{
		client: client,
		events: client.Events(),
		styles: newStyles(th),
	}
}

func (m Model) Init() tea.Cmd {
	return listenForEvents(m.events)
}

// listenForEvents waits for one event and delivers it as a message.
//
// A bubbletea Cmd is a function run off the main loop whose result comes
// back as a message, which is how anything blocking -- a channel receive,
// an RPC -- stays out of Update. It handles one event and is re-issued,
// because a Cmd runs once.
func listenForEvents(events <-chan engine.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return engineClosedMsg{}
		}
		return engineEventMsg(ev)
	}
}

// visibleTorrents is m.torrents narrowed by the sidebar's filter.
func (m Model) visibleTorrents() []engine.TorrentSnapshot {
	out := make([]engine.TorrentSnapshot, 0, len(m.torrents))
	for _, t := range m.torrents {
		if m.filter.matches(t) {
			out = append(out, t)
		}
	}
	return out
}

func (m *Model) setStatus(msg statusMsg) {
	m.status = msg.text
	m.statusIsErr = msg.isErr
}
