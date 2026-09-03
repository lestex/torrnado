package engine

import (
	"strings"
	"testing"
)

func TestSetLabelShowsUpInTheSnapshot(t *testing.T) {
	e := newTestEngine(t)
	id, _ := e.AddMagnet(testMagnet, AddOpts{})

	if err := e.SetLabel(id, "tv"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	if got := e.ListTorrents()[0].Label; got != "tv" {
		t.Errorf("Label = %q, want %q", got, "tv")
	}
}

// Clearing is an empty label rather than its own call, so it has to
// actually clear rather than being rejected as missing input.
func TestAnEmptyLabelClearsIt(t *testing.T) {
	e := newTestEngine(t)
	id, _ := e.AddMagnet(testMagnet, AddOpts{})
	e.SetLabel(id, "tv")

	if err := e.SetLabel(id, ""); err != nil {
		t.Fatalf("SetLabel to clear: %v", err)
	}
	if got := e.ListTorrents()[0].Label; got != "" {
		t.Errorf("Label = %q, want it cleared", got)
	}
}

// A label with a tab or a newline in it would corrupt every line of the
// list it appeared on, and would read as a rendering bug rather than as
// the input it came from.
func TestLabelsAreTrimmedAndValidated(t *testing.T) {
	e := newTestEngine(t)
	id, _ := e.AddMagnet(testMagnet, AddOpts{})

	if err := e.SetLabel(id, "  tv  "); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	if got := e.ListTorrents()[0].Label; got != "tv" {
		t.Errorf("Label = %q, want it trimmed to %q", got, "tv")
	}

	for _, bad := range []string{"a\tb", "a\nb", string(make([]byte, maxLabelLen+1))} {
		if err := e.SetLabel(id, bad); err == nil {
			t.Errorf("SetLabel(%q) was accepted", bad)
		}
	}
	// And a rejected label must not have disturbed the good one.
	if got := e.ListTorrents()[0].Label; got != "tv" {
		t.Errorf("Label = %q after rejections, want %q", got, "tv")
	}
}

func TestSetLabelRejectsAnUnknownTorrent(t *testing.T) {
	e := newTestEngine(t)
	if err := e.SetLabel("nope", "tv"); err == nil {
		t.Error("labelling an unknown torrent should fail")
	}
}

// Every other mutating operation writes a line; labelling did not, which
// left "where did that label go" answerable only by inferring it from the
// timestamps of unrelated events.
func TestLabelChangesAreLogged(t *testing.T) {
	e, logs := newLoggingEngine(t)
	id, _ := e.AddMagnet(testMagnet, AddOpts{})

	if err := e.SetLabel(id, "tv"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "torrent labelled") || !strings.Contains(out, string(id)) {
		t.Errorf("the label was not logged:\n%s", out)
	}

	// The value it replaced is the point: a label that changed under you
	// is exactly what the log has to be able to show.
	if err := e.SetLabel(id, "films"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	if out := logs.String(); !strings.Contains(out, `was=tv`) {
		t.Errorf("the previous label was not recorded:\n%s", out)
	}
}

// Setting the label it already has is not a change, and a log full of
// lines saying nothing happened is how a log stops being read.
func TestSettingTheSameLabelIsNotLogged(t *testing.T) {
	e, logs := newLoggingEngine(t)
	id, _ := e.AddMagnet(testMagnet, AddOpts{})
	e.SetLabel(id, "tv")

	before := strings.Count(logs.String(), "torrent labelled")
	if err := e.SetLabel(id, "tv"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	if after := strings.Count(logs.String(), "torrent labelled"); after != before {
		t.Errorf("logged %d times, want %d - setting the same label is not a change", after, before)
	}
}
