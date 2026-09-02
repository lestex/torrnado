package engine

import "testing"

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
