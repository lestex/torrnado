package theme

import (
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
