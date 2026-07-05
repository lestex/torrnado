package engine

import "testing"

func TestParsePriority(t *testing.T) {
	cases := []struct {
		in   string
		want Priority
		ok   bool
	}{
		{"none", PriorityNone, true},
		{"skip", PriorityNone, true}, // alias
		{"low", PriorityLow, true},
		{"normal", PriorityNormal, true},
		{"", PriorityNormal, true}, // empty means "leave it alone"
		{"high", PriorityHigh, true},
		{"now", PriorityNow, true},
		{"max", PriorityNow, true}, // alias
		{"urgent", 0, false},       // not a priority
		{"NONE", 0, false},         // the switch is case-sensitive
	}
	for _, c := range cases {
		got, ok := ParsePriority(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ParsePriority(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// Every priority must have a name. Without this, adding a constant and
// forgetting the switch case silently prints "unknown" in the UI.
func TestPriorityStringIsExhaustive(t *testing.T) {
	for p := PriorityNone; p <= PriorityNow; p++ {
		if p.String() == "unknown" {
			t.Errorf("Priority(%d) has no name", p)
		}
	}
}

// The names String() produces must be the names ParsePriority accepts, or
// a value shown to a user can't be typed back in.
func TestPriorityRoundTrip(t *testing.T) {
	for p := PriorityNone; p <= PriorityNow; p++ {
		got, ok := ParsePriority(p.String())
		if !ok || got != p {
			t.Errorf("round trip of %v via %q gave %v, %v", p, p.String(), got, ok)
		}
	}
}

func TestStateStringIsExhaustive(t *testing.T) {
	for s := StateChecking; s <= StateError; s++ {
		if s.String() == "unknown" {
			t.Errorf("State(%d) has no name", s)
		}
	}
}
