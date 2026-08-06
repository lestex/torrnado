// Package theme provides the color palettes torrnado's TUI renders
// with: a set of built-in themes plus user-supplied TOML overrides.
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
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Theme is a named palette. Every field is required to be non-empty when
// loaded from an override file; built-ins always populate all of them.
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

// missingFields lists the colors a Theme has left unset, by their TOML
// key names. A palette with a hole in it renders as an invisible or
// default-colored element rather than failing, so it is worth being able
// to say exactly which color is absent.
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

// Available returns every theme that can be loaded: the built-ins plus
// any <name>.toml in themesDir.
//
// A user file shadowing a built-in is listed once, because Load already
// prefers the file -- two entries would offer a choice that does not
// exist. A missing or unreadable directory yields the built-ins rather
// than an error: having no themes directory is the ordinary case, not a
// failure.
func Available(themesDir string) []string {
	seen := map[string]bool{}
	for name := range builtins {
		seen[name] = true
	}
	if entries, err := os.ReadDir(themesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if name := strings.TrimSuffix(e.Name(), ".toml"); name != e.Name() {
				seen[name] = true
			}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsUserTheme reports whether name resolves to a file in themesDir
// rather than to a built-in, so a picker can say which is which.
func IsUserTheme(name, themesDir string) bool {
	_, err := os.Stat(filepath.Join(themesDir, name+".toml"))
	return err == nil
}

// Load resolves a theme by name: a user's own themesDir/<name>.toml
// first, then a built-in of that name.
//
// The user's file wins, which is what lets a built-in be customised by
// dropping a file of the same name next to it rather than having to
// invent a new one.
func Load(name, themesDir string) (Theme, error) {
	if name == "" {
		name = "dracula"
	}

	overridePath := filepath.Join(themesDir, name+".toml")
	if data, err := os.ReadFile(overridePath); err == nil {
		var t Theme
		meta, err := toml.Decode(string(data), &t)
		if err != nil {
			return Theme{}, fmt.Errorf("theme %s (%s): %w", name, overridePath, err)
		}
		// An unknown key is a typo, and a silently ignored color is a
		// theme that looks broken for no visible reason.
		if undecoded := meta.Undecoded(); len(undecoded) > 0 {
			return Theme{}, fmt.Errorf("theme %s (%s): unknown key(s): %v", name, overridePath, undecoded)
		}
		t.Name = name
		if missing := t.missingFields(); len(missing) > 0 {
			return Theme{}, fmt.Errorf("theme %s (%s): missing required color(s): %v", name, overridePath, missing)
		}
		return t, nil
	}

	if t, ok := builtins[name]; ok {
		return t, nil
	}

	return Theme{}, fmt.Errorf("unknown theme %q (built-in themes: %v; or place %s.toml in %s)",
		name, Names(), name, themesDir)
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
