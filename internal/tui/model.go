// Package tui is torrnado's terminal interface, built on bubbletea.
//
// bubbletea programs are a loop over three things: a Model holding all
// the state, an Update that takes a message and returns a new Model, and
// a View that renders the Model to a string. Nothing draws to the screen
// directly; the framework diffs the returned string against what is on
// screen already.
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
	"github.com/lestex/torrnado/internal/theme"
)

// inputMode is whether keystrokes are being read as commands or typed
// into a prompt.
type inputMode int

const (
	modeNormal inputMode = iota
	modeSearch
	modeCommand
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

	keymap KeyMap

	// cursor indexes visibleTorrents, not torrents: it points at what is
	// on screen, so filtering moves it with the list rather than leaving
	// it aimed at a hidden row.
	cursor   int
	selected map[engine.TorrentID]bool

	// mode is which of the text prompts, if any, is taking keystrokes.
	mode        inputMode
	searchQuery string
	commandBuf  string

	// showHelp overlays the keybind reference on everything else.
	showHelp bool

	status      string
	statusIsErr bool

	width, height int
	quitting      bool
}

// New builds the initial Model. client and its Events() channel must
// already be connected.
func New(client *ipc.Client, km KeyMap, th theme.Theme) Model {
	return Model{
		client:   client,
		events:   client.Events(),
		keymap:   km,
		styles:   newStyles(th),
		selected: map[engine.TorrentID]bool{},
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

// visibleTorrents is m.torrents narrowed by the sidebar's filter and the
// search query. Both apply at once: they intersect rather than one
// replacing the other.
func (m Model) visibleTorrents() []engine.TorrentSnapshot {
	out := make([]engine.TorrentSnapshot, 0, len(m.torrents))
	q := strings.ToLower(strings.TrimSpace(m.searchQuery))
	for _, t := range m.torrents {
		if !m.filter.matches(t) {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(t.Name), q) {
			out = append(out, t)
		}
	}
	return out
}

// cursorTorrent returns the torrent under the cursor, if any.
func (m Model) cursorTorrent() (engine.TorrentSnapshot, bool) {
	visible := m.visibleTorrents()
	if m.cursor < 0 || m.cursor >= len(visible) {
		return engine.TorrentSnapshot{}, false
	}
	return visible[m.cursor], true
}

// clampCursor keeps the cursor inside a list of n rows. Torrents come and
// go while the user is looking at them, so the cursor has to be corrected
// whenever the list changes rather than trusted.
func (m *Model) clampCursor(n int) {
	if n == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) setStatus(msg statusMsg) {
	m.status = msg.text
	m.statusIsErr = msg.isErr
}
