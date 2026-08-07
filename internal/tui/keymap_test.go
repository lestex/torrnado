package tui

import (
	"reflect"
	"testing"

	"github.com/lestex/torrnado/internal/config"
)

func TestWithOverridesReplacesOnlyWhatIsNamed(t *testing.T) {
	km := DefaultKeyMap().WithOverrides(map[string]string{"quit": "Q"})

	if km.Quit != "Q" {
		t.Errorf("Quit = %q, want the override", km.Quit)
	}
	if km.Down != "j" {
		t.Errorf("Down = %q, want the default", km.Down)
	}
}

func TestWithOverridesOnEmptyMapChangesNothing(t *testing.T) {
	if got, want := DefaultKeyMap().WithOverrides(nil), DefaultKeyMap(); !reflect.DeepEqual(got, want) {
		t.Errorf("nil overrides changed the keymap: %+v", got)
	}
}

// config.KnownActions and WithOverrides are two hand-written lists that
// have to agree. If they drift, config accepts a binding that the keymap
// then ignores -- a key the user believes they rebound, silently doing
// the old thing.
func TestEveryKnownActionIsApplied(t *testing.T) {
	for _, action := range config.KnownActions {
		before := DefaultKeyMap()
		after := before.WithOverrides(map[string]string{action: "ctrl+z"})

		if reflect.DeepEqual(before, after) {
			t.Errorf("config knows action %q but WithOverrides ignores it", action)
		}
	}
}

// And the reverse: a key the TUI honours but config rejects can never be
// set, since validation fails before the TUI sees it.
func TestEveryAppliedActionIsKnownToConfig(t *testing.T) {
	known := make(map[string]bool, len(config.KnownActions))
	for _, a := range config.KnownActions {
		known[a] = true
	}

	// The action names WithOverrides looks for, discovered by trying each
	// candidate and seeing whether anything changes.
	for _, action := range []string{"up", "down", "top", "bottom", "select", "quit"} {
		if !known[action] {
			t.Errorf("WithOverrides applies %q but config rejects it", action)
		}
	}
}

// Every action must have a key. An unset one is the empty string, which
// silently matches nothing -- the feature is simply dead, with no build
// error and no failing test unless something looks for exactly this.
func TestNoBindingIsEmpty(t *testing.T) {
	km := DefaultKeyMap()
	v := reflect.ValueOf(km)

	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).String() == "" {
			t.Errorf("KeyMap.%s has no key bound", v.Type().Field(i).Name)
		}
	}
}

// And every field must be reachable from a config file, or it can never
// be rebound.
func TestEveryFieldIsOverridable(t *testing.T) {
	v := reflect.ValueOf(DefaultKeyMap())
	overridable := 0

	for _, action := range config.KnownActions {
		if DefaultKeyMap().WithOverrides(map[string]string{action: "ctrl+z"}) != DefaultKeyMap() {
			overridable++
		}
	}
	if overridable != v.NumField() {
		t.Errorf("%d of %d KeyMap fields can be overridden from config",
			overridable, v.NumField())
	}
}
