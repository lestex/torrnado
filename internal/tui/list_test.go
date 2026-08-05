package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/engine"
)

func TestProgressCellDrawsTheFraction(t *testing.T) {
	// Expectations are expressed in terms of barWidth rather than as cell
	// counts, so widening the bar stays a one-constant change.
	cases := []struct {
		frac       float64
		wantFilled int
		wantText   string
	}{
		{0, 0, "0%"},
		{0.25, barWidth / 4, "25%"},
		{0.5, barWidth / 2, "50%"},
		{1, barWidth, "100%"},
		// Progress can read fractionally over 1 while unverified bytes
		// are counted; the bar must not grow a cell wider than its column.
		{1.2, barWidth, "100%"},
		{-0.1, 0, "0%"},
	}
	for _, c := range cases {
		got := progressCell(c.frac)

		if n := strings.Count(got, "━"); n != c.wantFilled {
			t.Errorf("progressCell(%v) has %d filled cells, want %d (%q)",
				c.frac, n, c.wantFilled, got)
		}
		if n := strings.Count(got, "━") + strings.Count(got, "─"); n != barWidth {
			t.Errorf("progressCell(%v) drew a %d-cell bar, want %d (%q)",
				c.frac, n, barWidth, got)
		}
		if !strings.Contains(got, c.wantText) {
			t.Errorf("progressCell(%v) = %q, want it to contain %q", c.frac, got, c.wantText)
		}
		if w := lipgloss.Width(got); w != colProgress {
			t.Errorf("progressCell(%v) is %d columns, want exactly %d", c.frac, w, colProgress)
		}
	}
}

// Percentages floor rather than round, so a bar with a gap in it is never
// labelled 100%.
func TestProgressCellDoesNotRoundUpToComplete(t *testing.T) {
	got := progressCell(0.996)
	if strings.Contains(got, "100%") {
		t.Errorf("progressCell(0.996) = %q, want it to stay below 100%%", got)
	}
	if strings.Count(got, "─") == 0 {
		t.Errorf("progressCell(0.996) = %q, want an unfilled cell left", got)
	}
}

// Progress used to be an underline beneath the name, which made every
// torrent two rows tall.
func TestARowIsOneLine(t *testing.T) {
	m := testModel("a")
	p := layout(200, 50)

	for _, frac := range []float64{0, 0.5, 1} {
		snap := engine.TorrentSnapshot{Name: "a", Progress: frac, TotalLength: 100}
		if lines := m.renderRow(p, snap, false); len(lines) != 1 {
			t.Errorf("a row at %v progress is %d lines, want 1", frac, len(lines))
		}
	}
}

// The failure this guards against is the worst one in the package: a line
// wider than its pane wraps onto a row the layout never allocated, which
// grows the box and pushes the frame off the bottom of the screen.
func TestRowsAndHeaderFitTheirPane(t *testing.T) {
	widths := []int{minWidth, 61, 80, 100, 120, 200, 300}

	long := engine.TorrentSnapshot{
		Name:        strings.Repeat("a-very-long-torrent-name", 8),
		TotalLength: 9_999_999_999,
		Progress:    0.39,
		DownloadBPS: 999_900_000,
		UploadBPS:   999_900_000,
		ETA:         99 * 3600,
		State:       engine.StateChecking,
		Checking:    true,
	}

	for _, w := range widths {
		m := testModel("a")
		p := layout(w, 40)

		if got := lipgloss.Width(m.renderListHeader(p)); got > p.listContentW {
			t.Errorf("width %d: header is %d columns, pane holds %d", w, got, p.listContentW)
		}
		for _, line := range m.renderRow(p, long, true) {
			if got := lipgloss.Width(line); got > p.listContentW {
				t.Errorf("width %d: row is %d columns, pane holds %d", w, got, p.listContentW)
			}
		}
	}
}

// A header that does not line up with its rows is worse than no header.
func TestHeaderAndRowsShareTheirColumns(t *testing.T) {
	for _, w := range []int{minWidth, 80, 200} {
		m := testModel("a")
		p := layout(w, 40)

		snap := engine.TorrentSnapshot{Name: "torrent", TotalLength: 100, Progress: 0.5}
		row := m.renderRow(p, snap, false)[0]
		header := m.renderListHeader(p)

		if lipgloss.Width(header) != lipgloss.Width(row) {
			t.Errorf("width %d: header is %d columns and a row is %d",
				w, lipgloss.Width(header), lipgloss.Width(row))
		}
		// The bar starts where "Progress" does, or the column is only a
		// header and a coincidence.
		if strings.Index(header, "Progress") != strings.Index(row, "━")-0 {
			t.Errorf("width %d: header's Progress is at %d, the bar at %d\n%s\n%s",
				w, strings.Index(header, "Progress"), strings.Index(row, "━"), header, row)
		}
	}
}

// Everything except Name and Progress goes on a narrow terminal: how far
// along a torrent is beats how fast it is going.
func TestNarrowPanesKeepNameAndProgress(t *testing.T) {
	m := testModel("a")
	p := layout(minWidth, 40)

	if _, wide := nameWidth(p.listContentW); wide {
		t.Fatalf("a %d-column terminal should not fit the full column set", minWidth)
	}

	snap := engine.TorrentSnapshot{Name: "torrent", TotalLength: 1024, Progress: 0.5, DownloadBPS: 5000}
	row := m.renderRow(p, snap, false)[0]

	if !strings.Contains(row, "━") {
		t.Errorf("a narrow row dropped the progress bar: %q", row)
	}
	if !strings.Contains(row, "torrent") {
		t.Errorf("a narrow row dropped the name: %q", row)
	}
	if strings.Contains(row, "MiB/s") || strings.Contains(row, "KiB/s") {
		t.Errorf("a narrow row kept the speed columns: %q", row)
	}
}
