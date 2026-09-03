package theme

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Every built-in has to set every color. A palette with a hole in it
// renders as an invisible or default-colored element, which is far
// harder to notice than a build failure.
func TestBuiltinsAreComplete(t *testing.T) {
	for _, name := range Names() {
		th, err := Load(name, t.TempDir())
		if err != nil {
			t.Fatalf("Load(%q): %v", name, err)
		}
		if missing := th.missingFields(); len(missing) > 0 {
			t.Errorf("built-in %q is missing: %v", name, missing)
		}
		if th.Name != name {
			t.Errorf("built-in %q reports Name = %q", name, th.Name)
		}
	}
}

func TestLoadDefaultsWhenUnnamed(t *testing.T) {
	th, err := Load("", t.TempDir())
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if th.Name != "dracula" {
		t.Errorf("default theme is %q, want dracula", th.Name)
	}
}

// An unknown name lists what is available, so the fix is in the error.
func TestLoadUnknownNameListsTheChoices(t *testing.T) {
	_, err := Load("no-such-theme", t.TempDir())
	if err == nil {
		t.Fatal("an unknown theme should be an error")
	}
	if !strings.Contains(err.Error(), "dracula") {
		t.Errorf("error should list the built-ins, got: %v", err)
	}
}

func TestNamesAreSorted(t *testing.T) {
	names := Names()
	if len(names) < 2 {
		t.Fatalf("expected several built-in themes, got %v", names)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("Names() is not sorted: %v", names)
			break
		}
	}
}

// The "plain" theme exists for terminals with no real color support, so
// it must not use hex values - those are what it is avoiding.
func TestPlainThemeAvoidsHexColors(t *testing.T) {
	th, err := Load("plain", t.TempDir())
	if err != nil {
		t.Fatalf("Load(plain): %v", err)
	}
	for _, c := range []string{
		string(th.Foreground), string(th.Accent), string(th.Error),
	} {
		if strings.HasPrefix(c, "#") {
			t.Errorf("plain theme uses a hex color %q", c)
		}
	}
}

// xtermPalette is the classic 16-colour palette, used to give the ANSI
// codes a concrete value so they can be measured.
//
// A terminal may render these however it likes, and most render the
// bright half lighter than this. That makes these numbers the pessimistic
// case, which is the useful one: a colour that reads here reads anywhere.
var xtermPalette = [16]string{
	"#000000", "#800000", "#008000", "#808000",
	"#000080", "#800080", "#008080", "#c0c0c0",
	"#808080", "#ff0000", "#00ff00", "#ffff00",
	"#0000ff", "#ff00ff", "#00ffff", "#ffffff",
}

// relativeLuminance is WCAG's, so contrastRatio below means what it means
// everywhere else.
func relativeLuminance(hex string) float64 {
	h := strings.TrimPrefix(hex, "#")
	if len(h) != 6 {
		return -1
	}
	channel := func(i int) float64 {
		v, err := strconv.ParseInt(h[i:i+2], 16, 0)
		if err != nil {
			return 0
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(0) + 0.7152*channel(2) + 0.0722*channel(4)
}

func contrastRatio(a, b string) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	hi, lo := math.Max(la, lb), math.Min(la, lb)
	return (hi + 0.05) / (lo + 0.05)
}

// resolveColor turns a theme colour into a hex value, looking an ANSI
// code up in the palette above.
func resolveColor(c string) string {
	if strings.HasPrefix(c, "#") {
		return c
	}
	n, err := strconv.Atoi(c)
	if err != nil || n < 0 || n > 15 {
		return ""
	}
	return xtermPalette[n]
}

// Accent marks the row under the cursor, the focused pane's border and
// the active tab, and draws all of them as a foreground colour with
// nothing behind it - unlike SelectedBg, which brings its own contrast.
// So it has to read against the background the theme is written for.
//
// 3:1 is WCAG's floor for a interface element you are meant to pick out.
// The plain theme sat at 1.3:1 with ANSI 4 and the cursor row came out
// darker than the ordinary rows around it, which is the bug this guards.
//
// Measured against the theme's declared background, which is the author's
// stated assumption about the terminal: nothing paints it (see the note
// on Background), so the real backdrop is whatever the terminal is set
// to, and that is the only stated intent there is.
func TestEveryThemeAccentReadsAgainstItsBackground(t *testing.T) {
	const floor = 3.0

	for _, name := range Names() {
		th, err := Load(name, t.TempDir())
		if err != nil {
			t.Fatalf("Load(%s): %v", name, err)
		}
		bg, accent := resolveColor(string(th.Background)), resolveColor(string(th.Accent))
		if bg == "" || accent == "" {
			t.Errorf("%s: cannot resolve background %q or accent %q", name, th.Background, th.Accent)
			continue
		}
		if got := contrastRatio(accent, bg); got < floor {
			t.Errorf("%s: accent %s on background %s is %.2f:1, want at least %.1f:1 - "+
				"the row under the cursor would be harder to see than the rest",
				name, th.Accent, th.Background, got, floor)
		}
	}
}

// The ordinary text has to be readable too, and by a wider margin: it is
// every row, not the one being pointed at.
//
// The floor is 4.0 rather than WCAG AA's 4.5, and solarized-light is why:
// it reproduces a published palette that is deliberately low contrast -
// #657b83 on #fdf6e3 is 4.13:1, and those are Solarized's own values.
// Holding it to 4.5 would mean shipping something that is not Solarized,
// which is a worse answer than a theme that is faint on purpose. This
// still catches a theme whose text has become genuinely hard to read.
func TestEveryThemeForegroundReadsAgainstItsBackground(t *testing.T) {
	const floor = 4.0

	for _, name := range Names() {
		th, err := Load(name, t.TempDir())
		if err != nil {
			t.Fatalf("Load(%s): %v", name, err)
		}
		fg, bg := resolveColor(string(th.Foreground)), resolveColor(string(th.Background))
		if got := contrastRatio(fg, bg); got < floor {
			t.Errorf("%s: foreground %s on background %s is %.2f:1, want at least %.1f:1",
				name, th.Foreground, th.Background, got, floor)
		}
	}
}

// A marked row is drawn as selected_fg on selected_bg, which is the one
// pair that brings its own background - so it stands alone.
//
// Same floor and the same reason: solarized-light's selection sits at
// 4.39:1 out of its own palette.
func TestEveryThemeSelectionIsReadable(t *testing.T) {
	const floor = 4.0

	for _, name := range Names() {
		th, err := Load(name, t.TempDir())
		if err != nil {
			t.Fatalf("Load(%s): %v", name, err)
		}
		fg, bg := resolveColor(string(th.SelectedFg)), resolveColor(string(th.SelectedBg))
		if got := contrastRatio(fg, bg); got < floor {
			t.Errorf("%s: selected_fg %s on selected_bg %s is %.2f:1, want at least %.1f:1",
				name, th.SelectedFg, th.SelectedBg, got, floor)
		}
	}
}

// writeTheme puts a theme file in dir and returns dir.
func writeTheme(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

const completeTheme = `
background  = "#000000"
foreground  = "#ffffff"
muted       = "#888888"
accent      = "#ff00ff"
success     = "#00ff00"
warning     = "#ffff00"
error       = "#ff0000"
border      = "#333333"
selected_bg = "#222222"
selected_fg = "#ffffff"
`

func TestLoadUsesAnOverrideFile(t *testing.T) {
	dir := writeTheme(t, "mine", completeTheme)

	th, err := Load("mine", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if th.Accent != "#ff00ff" || th.Name != "mine" {
		t.Errorf("got %+v", th)
	}
}

// A file shadows a built-in of the same name, which is how a built-in
// gets customised without having to invent a new name for it.
func TestOverrideBeatsBuiltinOfTheSameName(t *testing.T) {
	dir := writeTheme(t, "dracula", completeTheme)

	th, err := Load("dracula", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if th.Accent != "#ff00ff" {
		t.Errorf("Accent = %q, want the override's color", th.Accent)
	}
}

// A half-written theme is rejected, naming what is absent. Falling back
// silently would leave elements rendering in the terminal's default with
// nothing to explain why.
func TestOverrideMissingColorsIsAnError(t *testing.T) {
	dir := writeTheme(t, "partial", `foreground = "#ffffff"`)

	_, err := Load("partial", dir)
	if err == nil {
		t.Fatal("an incomplete theme should be an error")
	}
	for _, want := range []string{"background", "accent", "selected_fg"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}
}

func TestOverrideUnknownKeyIsAnError(t *testing.T) {
	dir := writeTheme(t, "typo", completeTheme+"\nacent = \"#123456\"\n")

	_, err := Load("typo", dir)
	if err == nil {
		t.Fatal("an unknown key should be an error")
	}
	if !strings.Contains(err.Error(), "acent") {
		t.Errorf("error should name the unknown key, got: %v", err)
	}
}

func TestAvailableListsBuiltinsWithNoThemesDir(t *testing.T) {
	got := Available(filepath.Join(t.TempDir(), "absent"))

	if len(got) != len(Names()) {
		t.Errorf("got %d themes, want the %d built-ins", len(got), len(Names()))
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("themes are not sorted: %v", got)
	}
}

func TestAvailableIncludesUserThemes(t *testing.T) {
	dir := t.TempDir()
	writeThemeFile(t, dir, "midnight")
	// Not a theme; must not be listed.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Available(dir)

	if !slices.Contains(got, "midnight") {
		t.Errorf("a user theme is missing from %v", got)
	}
	if slices.Contains(got, "notes") || slices.Contains(got, "notes.txt") {
		t.Errorf("a non-theme file was listed: %v", got)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("themes are not sorted: %v", got)
	}
}

// Load prefers a user file over the built-in of the same name, so
// listing both would offer a choice that does not exist.
func TestAvailableListsAShadowedBuiltinOnce(t *testing.T) {
	dir := t.TempDir()
	writeThemeFile(t, dir, "nord")

	got := Available(dir)

	var n int
	for _, name := range got {
		if name == "nord" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("nord appears %d times in %v, want once", n, got)
	}
}

func TestIsUserTheme(t *testing.T) {
	dir := t.TempDir()
	writeThemeFile(t, dir, "midnight")

	if !IsUserTheme("midnight", dir) {
		t.Error("a theme with a file in the themes dir is not reported as a user theme")
	}
	if IsUserTheme("nord", dir) {
		t.Error("a built-in with no file is reported as a user theme")
	}
}

// writeThemeFile drops a complete, valid theme into dir.
func writeThemeFile(t *testing.T, dir, name string) {
	t.Helper()
	body := `background = "#000000"
foreground = "#ffffff"
muted = "#888888"
accent = "#ff00ff"
success = "#00ff00"
warning = "#ffff00"
error = "#ff0000"
border = "#444444"
selected_bg = "#222222"
selected_fg = "#ffffff"
`
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
