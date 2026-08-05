// Package theme provides the color palettes torrnado's TUI renders with.
//
// Palettes are stored as lipgloss.Color hex strings. Graceful degradation
// for terminals without truecolor is handled by lipgloss/termenv itself --
// lipgloss.Color downsamples a hex value to the nearest ANSI256 or ANSI16
// color automatically based on the terminal's detected profile, so themes
// don't need separate truecolor/256-color variants. The "plain" theme
// exists for the worst case (no color profile detected at all, or a
// terminal a user wants a guaranteed-safe look on) and uses bare ANSI
// color codes instead of hex.
package theme

import (
	"fmt"
	"os"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Theme is a named palette. Built-ins always populate every field.
type Theme struct {
	Name string

	Background lipgloss.Color `toml:"background"`
	Foreground lipgloss.Color `toml:"foreground"`
	Muted      lipgloss.Color `toml:"muted"`
	Accent     lipgloss.Color `toml:"accent"`
	Success    lipgloss.Color `toml:"success"`
	Warning    lipgloss.Color `toml:"warning"`
	Error      lipgloss.Color `toml:"error"`
	Border     lipgloss.Color `toml:"border"`
	SelectedBg lipgloss.Color `toml:"selected_bg"`
	SelectedFg lipgloss.Color `toml:"selected_fg"`
}

// missingFields lists the colours a Theme has left unset, by their TOML
// key names. A palette with a hole in it renders as an invisible or
// default-coloured element rather than failing, so it is worth being able
// to say exactly which colour is absent.
func (t Theme) missingFields() []string {
	var missing []string
	check := func(name string, v lipgloss.Color) {
		if v == "" {
			missing = append(missing, name)
		}
	}
	check("background", t.Background)
	check("foreground", t.Foreground)
	check("muted", t.Muted)
	check("accent", t.Accent)
	check("success", t.Success)
	check("warning", t.Warning)
	check("error", t.Error)
	check("border", t.Border)
	check("selected_bg", t.SelectedBg)
	check("selected_fg", t.SelectedFg)
	return missing
}

// builtins maps theme name -> palette, populated by builtin.go's init.
var builtins = map[string]Theme{}

func register(t Theme) {
	builtins[t.Name] = t
}

// Names returns every built-in theme name, sorted.
func Names() []string {
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Load resolves a theme by name, falling back to the default when the
// name is empty.
func Load(name string) (Theme, error) {
	if name == "" {
		name = "dracula"
	}
	if t, ok := builtins[name]; ok {
		return t, nil
	}
	return Theme{}, fmt.Errorf("unknown theme %q (built-in themes: %v)", name, Names())
}

// TruecolorSupported reports whether the terminal (per COLORTERM and
// termenv's own detection) supports 24-bit color. It's informational --
// lipgloss degrades colors on its own -- but is surfaced so the TUI/config
// can suggest the "plain" theme when it's false.
func TruecolorSupported() bool {
	if v := os.Getenv("COLORTERM"); v == "truecolor" || v == "24bit" {
		return true
	}
	return termenv.EnvColorProfile() == termenv.TrueColor
}
