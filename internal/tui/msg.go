package tui

import "github.com/lestex/torrnado/internal/engine"

// engineEventMsg carries a fresh state snapshot pushed by the daemon.
type engineEventMsg engine.Event

// engineClosedMsg is sent when the event stream ends (daemon connection
// lost).
type engineClosedMsg struct{}

// statusMsg sets the transient message shown in the status bar.
type statusMsg struct {
	text  string
	isErr bool
}

// torrentsAddedMsg reports a successful add, so the model can make sure
// what was just added is actually going to be visible.
//
// It carries the ids rather than only a count because which filters are
// hiding them cannot be answered until the torrents themselves are in
// hand: a name decides the search, a label decides the label filter, and
// a state decides the status filter. They arrive on the next snapshot.
type torrentsAddedMsg struct {
	text string
	ids  []engine.TorrentID
}

// statusExpiredMsg asks for the status bar to be cleared. It carries the
// sequence number of the message it was scheduled for, so a stale timer
// cannot wipe a newer message.
type statusExpiredMsg struct{ seq int }

// detailLoadedMsg carries the result of an async Detail call.
type detailLoadedMsg struct {
	detail engine.TorrentDetail
	err    error
}

func errStatus(err error) statusMsg {
	return statusMsg{text: err.Error(), isErr: true}
}

func okStatus(text string) statusMsg {
	return statusMsg{text: text}
}
