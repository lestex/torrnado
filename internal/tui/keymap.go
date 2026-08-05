package tui

// KeyMap holds the single key bound to each action.
//
// Values are compared against tea.KeyMsg.String(), so anything bubbletea
// recognises works: "j", "ctrl+c", "enter", "esc". Keeping the schema to
// one key per action is what lets it be overridden from a config file
// without inventing a syntax for chords.
type KeyMap struct {
	Up, Down, Top, Bottom string
	Select                string
	Quit                  string
}

// DefaultKeyMap is torrnado's out-of-the-box vim-like binding set.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:     "k",
		Down:   "j",
		Top:    "g",
		Bottom: "G",

		Select: " ",
		Quit:   "q",
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
	apply(&k.Quit, "quit")
	return k
}
