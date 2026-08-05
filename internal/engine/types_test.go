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

// While a hash check runs the status carries its progress, so a long
// recheck does not look like a hang.
func TestStatusTextShowsCheckProgress(t *testing.T) {
	cases := []struct {
		snap TorrentSnapshot
		want string
	}{
		{TorrentSnapshot{State: StateChecking, Checking: true, CheckProgress: 0.42}, "checking 42%"},
		{TorrentSnapshot{State: StateChecking, Checking: true, CheckProgress: 1}, "checking 100%"},
		// A check that has only just started is at zero, and saying so
		// beats falling back to a bare "checking" for the first tick of
		// what may be an hour's work.
		{TorrentSnapshot{State: StateChecking, Checking: true}, "checking 0%"},
		// Waiting for metadata is also "checking", but nothing is being
		// verified, so there is no number to show.
		{TorrentSnapshot{State: StateChecking}, "checking"},
		// Progress from an earlier check must not leak into other states.
		{TorrentSnapshot{State: StateSeeding, CheckProgress: 0.5}, "seeding"},
		{TorrentSnapshot{State: StateDownloading}, "downloading"},
	}
	for _, c := range cases {
		if got := c.snap.StatusText(); got != c.want {
			t.Errorf("StatusText(%v, %.2f) = %q, want %q",
				c.snap.State, c.snap.CheckProgress, got, c.want)
		}
	}
}

// The status has to fit the list's column, or it shears the table.
func TestStatusTextFitsTheColumn(t *testing.T) {
	const columnWidth = 13
	for s := StateChecking; s <= StateError; s++ {
		for _, p := range []float64{0, 0.5, 1} {
			got := (TorrentSnapshot{State: s, CheckProgress: p}).StatusText()
			if len(got) > columnWidth {
				t.Errorf("StatusText = %q, %d chars, wider than the %d-wide column",
					got, len(got), columnWidth)
			}
		}
	}
}
