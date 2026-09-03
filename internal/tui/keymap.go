package tui

// KeyMap holds the single key bound to each action.
//
// Values are compared against tea.KeyMsg.String(), so anything bubbletea
// recognizes works: "j", "ctrl+c", "enter", "esc". Keeping the schema to
// one key per action is what lets it be overridden from a config file
// without inventing a syntax for chords.
type KeyMap struct {
	Up, Down, Top, Bottom      string
	Select, Remove, RemoveData string
	Purge                      string
	Pause, Recheck             string
	Detail, Back, Quit         string
	Search, Command, Help      string
	// HelpAlt is the second key that opens the reference. "?" is what
	// people press when they do not know the keys yet - it is the
	// convention less/man/vim/git set - and "h" is what they press once
	// they do. Neither is worth giving up for the other.
	HelpAlt string
	Preview string
	Open    string

	// Pane focus and detail-pane tab movement.
	FocusNext, FocusPrev   string
	TabNext, TabPrev       string
	FilterNext, FilterPrev string

	// ToggleMouse hands the mouse back to the terminal and takes it
	// again. While torrnado holds it, click-drag no longer selects text,
	// so this is how you copy something off the screen.
	ToggleMouse string

	// The detail tabs by name, as a shortcut past stepping through them.
	// Bindings rather than hardcoded digits: they used to be matched
	// before focus dispatch ever ran, so binding any action to "1" left
	// that action unreachable with nothing to say why.
	TabPieces, TabPeers, TabFiles string
}

// DefaultKeyMap is torrnado's out-of-the-box vim-like binding set.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:     "k",
		Down:   "j",
		Top:    "g",
		Bottom: "G",

		Select:     " ",
		Remove:     "x",
		RemoveData: "D",
		// The other half of x: x drops the torrent and keeps the files,
		// X drops the files and keeps the torrent.
		Purge: "X",

		Pause:   "p",
		Recheck: "r",

		Detail: "enter",
		Back:   "esc",
		Quit:   "q",

		Search:  "/",
		Command: ":",
		Help:    "h",
		HelpAlt: "?",
		Preview: "v",
		// Free, next to nothing destructive, and the letter every file
		// manager and editor already uses for "open".
		Open: "o",

		FocusNext: "tab",
		FocusPrev: "shift+tab",

		// "h"/"l" would be the vim-idiomatic pair for moving between
		// tabs, but "h" is already help; brackets keep tab movement
		// reachable from any pane without stealing a letter.
		TabNext: "]",
		TabPrev: "[",

		FilterNext: "}",
		FilterPrev: "{",

		ToggleMouse: "m",

		TabPieces: "1",
		TabPeers:  "2",
		TabFiles:  "3",
	}
}

// WithOverrides applies config [keybinds] overrides on top of the
// defaults.
//
// Unknown action names are rejected by config validation long before this
// runs, so anything present here is known to be real.
func (k KeyMap) WithOverrides(overrides map[string]string) KeyMap {
	apply := func(dst *string, action string) {
		if v, ok := overrides[action]; ok {
			*dst = v
		}
	}
	apply(&k.Up, "up")
	apply(&k.Down, "down")
	apply(&k.Top, "top")
	apply(&k.Bottom, "bottom")
	apply(&k.Select, "select")
	apply(&k.Remove, "remove")
	apply(&k.RemoveData, "remove_data")
	apply(&k.Purge, "purge")
	apply(&k.Pause, "pause")
	apply(&k.Recheck, "recheck")
	apply(&k.Search, "search")
	apply(&k.Command, "command")
	apply(&k.Help, "help")
	apply(&k.HelpAlt, "help_alt")
	apply(&k.Preview, "preview")
	apply(&k.Open, "open")
	apply(&k.Detail, "detail")
	apply(&k.Back, "back")
	apply(&k.Quit, "quit")
	apply(&k.FocusNext, "focus_next")
	apply(&k.FocusPrev, "focus_prev")
	apply(&k.TabNext, "tab_next")
	apply(&k.TabPrev, "tab_prev")
	apply(&k.FilterNext, "filter_next")
	apply(&k.FilterPrev, "filter_prev")
	apply(&k.ToggleMouse, "toggle_mouse")
	apply(&k.TabPieces, "tab_pieces")
	apply(&k.TabPeers, "tab_peers")
	apply(&k.TabFiles, "tab_files")
	return k
}
