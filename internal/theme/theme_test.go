package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every built-in has to set every colour. A palette with a hole in it
// renders as an invisible or default-coloured element, which is far
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

// The "plain" theme exists for terminals with no real colour support, so
// it must not use hex values -- those are what it is avoiding.
func TestPlainThemeAvoidsHexColours(t *testing.T) {
	th, err := Load("plain", t.TempDir())
	if err != nil {
		t.Fatalf("Load(plain): %v", err)
	}
	for _, c := range []string{
		string(th.Foreground), string(th.Accent), string(th.Error),
	} {
		if strings.HasPrefix(c, "#") {
			t.Errorf("plain theme uses a hex colour %q", c)
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
		t.Errorf("Accent = %q, want the override's colour", th.Accent)
	}
}

// A half-written theme is rejected, naming what is absent. Falling back
// silently would leave elements rendering in the terminal's default with
// nothing to explain why.
func TestOverrideMissingColoursIsAnError(t *testing.T) {
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
