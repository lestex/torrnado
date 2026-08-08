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

	// daemonDown records that the event stream has ended. One-way: the
	// client does not redial, so a daemon that comes back needs the TUI
	// restarted, and a dot that went green again would be lying.
	daemonDown bool

	// filter narrows the list to one status. The full set is kept above,
	// so changing it is a cheap re-render rather than a refetch.
	filter   statusFilter
	sortBy   sortMode
	sortDesc bool

	keymap KeyMap
	player string // command used to preview a file, from config
	opener string // command used to show a torrent's folder, from config

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

	// The theme picker: the palette it is choosing from, where the
	// cursor is in it, what is applied, and what to put back on escape.
	// themesDir is kept because a user's own themes live on disk and are
	// re-read as the cursor moves.
	themePicker bool
	themeNames  []string
	themeCursor int
	theme       theme.Theme
	themeSaved  theme.Theme
	themesDir   string

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
	// statusSeq counts status messages, so the timer that clears one can
	// tell whether it is still the message on screen.
	statusSeq int

	width, height int
	quitting      bool
}

// Options is what a Model needs from the outside world.
//
// A struct rather than positional parameters: the list had grown to the
// point of two adjacent strings, and a caller that swapped ThemesDir and
// Player would compile and then behave strangely at runtime.
type Options struct {
	Client *ipc.Client
	Keys   KeyMap
	Theme  theme.Theme
	// ThemesDir is where a user's own themes live, re-read when the
	// theme picker changes selection.
	ThemesDir string
	// Player is the command used to preview a file.
	Player string
	// Opener is the command used to show a torrent's folder.
	Opener string
}

// New builds the initial Model. The client and its Events() channel must
// already be connected.
func New(o Options) Model {
	return Model{
		client:    o.Client,
		events:    o.Client.Events(),
		keymap:    o.Keys,
		styles:    newStyles(o.Theme),
		theme:     o.Theme,
		themesDir: o.ThemesDir,
		player:    o.Player,
		opener:    o.Opener,
		selected:  map[engine.TorrentID]bool{},
	}
}

func (m Model) Init() tea.Cmd {
	return listenForEvents(m.events)
}

// listenForEvents waits for one event and delivers it as a message.
//
// A bubbletea Cmd is a function run off the main loop whose result comes
// back as a message, which is how anything blocking - a channel receive,
// an RPC - stays out of Update. It handles one event and is re-issued,
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
// Events carry snapshots only - files, peers and pieces need their own
// call - so this runs on every tick as well as on cursor movement, which
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

// How long a status message stays on screen. Errors linger, because they
// are usually the answer to "why did nothing happen" and are worth
// reading twice; a confirmation has done its job as soon as it is seen.
const (
	statusTTL      = 5 * time.Second
	statusErrorTTL = 12 * time.Second
)

// setStatus shows a message and returns the command that will clear it.
//
// It mutates the receiver, so it must be called on its own line:
//
//	cmd := m.setStatus(okStatus("done"))
//	return m, cmd
//
// `return m, m.setStatus(...)` is two operands of one return statement, and
// the spec orders the function calls among themselves but not against the
// plain operand beside them - so whether the returned copy carries the
// message is left to the compiler.
//
// A status bar reporting "rechecking 1 torrent(s)" is describing
// something that happened, not something that is still true - and the
// message outlived the recheck by however long the TUI stayed open,
// which reads as a job that never finished.
//
// A caller that drops the returned command gets the old behavior: a
// message that stays until something replaces it. That is deliberate for
// a lost connection, which is a condition rather than an event.
func (m *Model) setStatus(msg statusMsg) tea.Cmd {
	m.status = msg.text
	m.statusIsErr = msg.isErr
	m.statusSeq++

	ttl := statusTTL
	if msg.isErr {
		ttl = statusErrorTTL
	}
	return expireStatusCmd(m.statusSeq, ttl)
}

// expireStatusCmd asks for a status to be cleared after a delay. Split
// out so a test can exercise it with a delay it does not have to sit
// through - tea.Tick's command really does wait.
func expireStatusCmd(seq int, after time.Duration) tea.Cmd {
	return tea.Tick(after, func(time.Time) tea.Msg {
		return statusExpiredMsg{seq: seq}
	})
}

// clearStatus removes the message with the given sequence number, and
// does nothing if a newer one has replaced it.
func (m *Model) clearStatus(seq int) {
	if seq != m.statusSeq {
		return
	}
	m.status = ""
	m.statusIsErr = false
}
