// Package tui is torrnado's terminal interface, built on bubbletea.
//
// bubbletea programs are a loop over three things: a Model holding all
// the state, an Update that takes a message and returns a new Model, and
// a View that renders the Model to a string. Nothing draws to the screen
// directly; the framework diffs the returned string against what is on
// screen already.
package tui

import (
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
	"github.com/lestex/torrnado/internal/theme"
)

// paneFocus is which pane keystrokes are routed to.
type paneFocus int

const (
	focusList paneFocus = iota
	focusDetail
	focusSidebar
)

func (f paneFocus) next() paneFocus { return (f + 1) % 3 }
func (f paneFocus) prev() paneFocus { return (f + 2) % 3 }

// detailTab is which page of the docked detail pane is showing.
type detailTab int

const (
	tabPieces detailTab = iota
	tabPeers
	tabFiles
)

var detailTabNames = [...]string{tabPieces: "Pieces", tabPeers: "Peers", tabFiles: "Files"}

func (t detailTab) next() detailTab { return (t + 1) % detailTab(len(detailTabNames)) }
func (t detailTab) prev() detailTab {
	return (t + detailTab(len(detailTabNames)) - 1) % detailTab(len(detailTabNames))
}

// sortMode is the column the list is ordered by.
//
// Some ordering is not optional: the daemon reports torrents from a map,
// and Go randomises map iteration, so an unsorted list reshuffles itself
// every tick with the cursor pointing at whatever landed under it.
type sortMode int

const (
	sortName sortMode = iota
	sortSize
	sortProgress
	sortRatio
	sortETA
	sortAdded
	sortDown
	sortUp
)

// ParseSortMode maps a column name to a sort mode.
func ParseSortMode(s string) (sortMode, bool) {
	switch s {
	case "name":
		return sortName, true
	case "size":
		return sortSize, true
	case "progress":
		return sortProgress, true
	case "ratio":
		return sortRatio, true
	case "eta":
		return sortETA, true
	case "added":
		return sortAdded, true
	case "down":
		return sortDown, true
	case "up":
		return sortUp, true
	}
	return 0, false
}

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
	filter   statusFilter
	sortBy   sortMode
	sortDesc bool

	keymap KeyMap
	player string // command used to preview a file, from config

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

	// pendingDD is a "d" waiting for its partner, for vim's dd chord.
	pendingDD bool
	pendingAt time.Time

	focus         paneFocus
	sidebarCursor int

	// detail always describes the torrent under the list cursor. The
	// pane is docked rather than a separate view, so it is refetched
	// whenever the cursor moves or fresh state arrives.
	detail       engine.TorrentDetail
	detailTab    detailTab
	detailCursor int // file cursor, used by the Files tab
	detailScroll int // scroll offset within the active tab
	detailLoaded bool
	detailID     engine.TorrentID

	status      string
	statusIsErr bool

	width, height int
	quitting      bool
}

// New builds the initial Model. client and its Events() channel must
// already be connected.
func New(client *ipc.Client, km KeyMap, th theme.Theme, player string) Model {
	return Model{
		client:   client,
		events:   client.Events(),
		keymap:   km,
		styles:   newStyles(th),
		player:   player,
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

	// SliceStable so torrents comparing equal keep a fixed relative
	// order; an unstable sort would let them swap between ticks, which is
	// the jitter this exists to remove.
	sort.SliceStable(out, func(i, j int) bool {
		var less bool
		switch m.sortBy {
		case sortSize:
			less = out[i].TotalLength < out[j].TotalLength
		case sortProgress:
			less = out[i].Progress < out[j].Progress
		case sortRatio:
			less = out[i].Ratio < out[j].Ratio
		case sortETA:
			less = out[i].ETA < out[j].ETA
		case sortAdded:
			less = out[i].AddedAt.Before(out[j].AddedAt)
		case sortDown:
			less = out[i].DownloadBPS < out[j].DownloadBPS
		case sortUp:
			less = out[i].UploadBPS < out[j].UploadBPS
		default:
			less = strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		if m.sortDesc {
			return !less
		}
		return less
	})
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

// syncDetail fetches detail for the torrent under the cursor.
//
// Events carry snapshots only -- files, peers and pieces need their own
// call -- so this runs on every tick as well as on cursor movement, which
// keeps the pane no staler than the list beside it.
func (m *Model) syncDetail() tea.Cmd {
	t, ok := m.cursorTorrent()
	if !ok {
		m.detailLoaded = false
		m.detail = engine.TorrentDetail{}
		m.detailID = ""
		return nil
	}
	if t.ID != m.detailID {
		m.detailID = t.ID
		m.detailLoaded = false
		m.detailCursor = 0
		m.detailScroll = 0
		return loadDetail(m.client, t.ID)
	}
	// Same torrent: patch the summary from state we already have, and
	// refresh the rest.
	m.detail.Snapshot = t
	return loadDetail(m.client, t.ID)
}

func (m *Model) setStatus(msg statusMsg) {
	m.status = msg.text
	m.statusIsErr = msg.isErr
}
