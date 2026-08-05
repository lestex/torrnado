package tui

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

// A torrent can have tens of thousands of pieces and the pane a few
// hundred cells, so each cell stands for a range. The arithmetic is easy
// to get wrong at the edges, and wrong here means an index out of range.
func TestDownsampleFillsExactlyTheCellsAsked(t *testing.T) {
	runs := []engine.PieceRun{{Length: 10000, Known: true, Complete: true}}

	for _, cells := range []int{1, 7, 100, 999, 10000} {
		got := downsample(runs, 10000, cells)
		if len(got) != cells {
			t.Errorf("downsample to %d cells produced %d", cells, len(got))
		}
	}
}

func TestDownsampleWithNoCells(t *testing.T) {
	if got := downsample(nil, 0, 0); got != nil {
		t.Errorf("downsample(0 cells) = %v, want nil", got)
	}
}

// A cell keeps the worst state in its range, so one missing piece is
// enough to stop it reading as complete. Reporting otherwise would show a
// gap-ridden torrent as finished.
func TestDownsampleKeepsTheWorstStateInARange(t *testing.T) {
	runs := []engine.PieceRun{
		{Length: 99, Known: true, Complete: true},
		{Length: 1, Known: true}, // one missing piece at the end
	}

	got := downsample(runs, 100, 1)
	if len(got) != 1 {
		t.Fatalf("got %d cells, want 1", len(got))
	}
	if got[0] != cellMissing {
		t.Errorf("cell = %v, want missing: one absent piece is not complete", got[0])
	}
}

// Unknown is its own state, not missing. Completion is only believed once
// the library has consulted storage, and it does that lazily -- so a
// finished torrent reports most pieces unknown for a while, and folding
// the two together would draw it as half empty.
func TestUnknownIsNotMissing(t *testing.T) {
	unknown := runState(engine.PieceRun{Length: 1, Known: false})
	missing := runState(engine.PieceRun{Length: 1, Known: true})

	if unknown == missing {
		t.Error("unknown and missing should be different states")
	}
	if unknown != cellUnknown {
		t.Errorf("unknown run gave %v", unknown)
	}
}

func TestRunStateOrdering(t *testing.T) {
	cases := []struct {
		run  engine.PieceRun
		want cellState
	}{
		{engine.PieceRun{Known: true, Complete: true}, cellComplete},
		{engine.PieceRun{Known: true, Checking: true}, cellChecking},
		{engine.PieceRun{Known: true, Partial: true}, cellPartial},
		{engine.PieceRun{Known: true}, cellMissing},
		{engine.PieceRun{}, cellUnknown},
	}
	for _, c := range cases {
		if got := runState(c.run); got != c.want {
			t.Errorf("runState(%+v) = %v, want %v", c.run, got, c.want)
		}
	}
}

// The summary says "verified", and the count has to match what the map
// draws or the two disagree on screen.
func TestPieceSummaryCountsVerifiedAndUnknown(t *testing.T) {
	d := engine.TorrentDetail{
		NumPieces:   100,
		PieceLength: 256 << 10,
		Pieces: []engine.PieceRun{
			{Length: 40, Known: true, Complete: true},
			{Length: 60}, // not consulted yet
		},
	}

	got := pieceSummary(d)
	for _, want := range []string{"40/100", "verified", "60 unknown"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q should mention %q", got, want)
		}
	}
}
