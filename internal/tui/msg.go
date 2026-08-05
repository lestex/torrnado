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
