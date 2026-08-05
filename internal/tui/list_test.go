package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/engine"
)

func TestProgressCellDrawsTheFraction(t *testing.T) {
	// Expectations are fractions of the bar rather than cell counts, so
	// they hold at whatever width the bar is drawn.
	cases := []struct {
		frac       float64
		wantFilled func(bar int) int
		wantText   string
	}{
		{0, func(int) int { return 0 }, "0%"},
		{0.25, func(bar int) int { return bar / 4 }, "25%"},
		{0.5, func(bar int) int { return bar / 2 }, "50%"},
		{1, func(bar int) int { return bar }, "100%"},
		// Progress can read fractionally over 1 while unverified bytes
		// are counted; the bar must not grow a cell wider than its column.
		{1.2, func(bar int) int { return bar }, "100%"},
		{-0.1, func(int) int { return 0 }, "0%"},
	}
	for _, bar := range []int{minBarWidth, 20, maxBarWidth} {
		for _, c := range cases {
			got := progressCell(c.frac, bar)

			if n := strings.Count(got, "━"); n != c.wantFilled(bar) {
				t.Errorf("progressCell(%v, %d) has %d filled cells, want %d (%q)",
					c.frac, bar, n, c.wantFilled(bar), got)
			}
			if n := strings.Count(got, "━") + strings.Count(got, "─"); n != bar {
				t.Errorf("progressCell(%v, %d) drew a %d-cell bar, want %d (%q)",
					c.frac, bar, n, bar, got)
			}
			if !strings.Contains(got, c.wantText) {
				t.Errorf("progressCell(%v, %d) = %q, want it to contain %q",
					c.frac, bar, got, c.wantText)
			}
			if w := lipgloss.Width(got); w != bar+colGap+colPercent {
				t.Errorf("progressCell(%v, %d) is %d columns, want %d",
					c.frac, bar, w, bar+colGap+colPercent)
			}
		}
	}
}

// Percentages floor rather than round, so a bar with a gap in it is never
// labelled 100%.
func TestProgressCellDoesNotRoundUpToComplete(t *testing.T) {
	got := progressCell(0.996, minBarWidth)
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
		h, _ := columnOf(header, "Progress")
		v, _ := columnOf(row, "━")
		if h != v {
			t.Errorf("width %d: header's Progress is at column %d, the bar at %d\n%s\n%s",
				w, h, v, header, row)
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

func TestTruncateTailKeepsTheEnd(t *testing.T) {
	cases := []struct {
		in, want string
		width    int
	}{
		{"short", "short", 10},
		{"magnet:?xt=urn:btih:abc", "…btih:abc", 9},
		{"abcdef", "…f", 2},
		{"abcdef", "…", 1},
		{"abcdef", "", 0},
	}
	for _, c := range cases {
		if got := truncateTail(c.in, c.width); got != c.want {
			t.Errorf("truncateTail(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
		}
		if got := truncateTail(c.in, c.width); lipgloss.Width(got) > c.width {
			t.Errorf("truncateTail(%q, %d) = %q, %d columns wide",
				c.in, c.width, got, lipgloss.Width(got))
		}
	}
}

// The prompt is a plain input line. It used to be rendered with
// SelectedRow, the list's selection highlight, which drew a block of
// background around whatever had been typed.
func TestPromptDoesNotWearTheSelectionHighlight(t *testing.T) {
	m := testModel("a")

	got := m.renderPrompt(":", "add test", 80)

	if bg := m.styles.SelectedRow.Render("x"); strings.Contains(got, ansiPrefix(bg)) {
		t.Errorf("the prompt is drawn with the selection style: %q", got)
	}
	if !strings.Contains(got, "add test") {
		t.Errorf("the prompt lost what was typed: %q", got)
	}
	if !strings.Contains(got, ":") {
		t.Errorf("the prompt lost its sigil: %q", got)
	}
}

// A long paste has to scroll rather than overflow the line the layout
// allocated -- and the trim must not cut through the styling's escape
// sequences.
func TestPromptFitsTheFooter(t *testing.T) {
	m := testModel("a")
	long := "add magnet:?xt=urn:btih:" + strings.Repeat("f", 200)

	for _, width := range []int{80, 40, 12, 4, 2} {
		got := m.renderPrompt(":", long, width)
		if w := lipgloss.Width(got); w > width {
			t.Errorf("width %d: prompt is %d columns: %q", width, w, got)
		}
	}
}

// While typing, what matters is what was just entered.
func TestPromptShowsTheEndOfALongLine(t *testing.T) {
	m := testModel("a")

	got := m.renderPrompt(":", "add "+strings.Repeat("x", 100)+"TAIL", 30)

	if !strings.Contains(got, "TAIL") {
		t.Errorf("the prompt scrolled the wrong way: %q", got)
	}
}

// columnOf reports which screen column sub starts at.
//
// Measured, not counted: strings.Index gives a byte offset, and a row
// full of three-byte bar glyphs is nowhere near its own column numbers.
func columnOf(s, sub string) (int, bool) {
	i := strings.Index(s, sub)
	if i < 0 {
		return 0, false
	}
	return lipgloss.Width(s[:i]), true
}

// ansiPrefix returns the escape sequence a style opens with, so one
// style's rendering can be told apart from another's.
func ansiPrefix(rendered string) string {
	if i := strings.Index(rendered, "m"); i > 0 {
		return rendered[:i+1]
	}
	return rendered
}

// Every column is left-aligned, so each heading starts in the same
// column as the values beneath it. Right-aligned numbers put a heading
// nowhere near its data once the values are narrower than their column.
func TestEachHeadingStartsWhereItsValuesDo(t *testing.T) {
	m := testModel("a")
	p := layout(200, 40)

	if _, wide := nameWidth(p.listContentW); !wide {
		t.Fatal("this test needs the full column set")
	}

	snap := engine.TorrentSnapshot{
		Name:        "torrent",
		TotalLength: 1_610_612_736, // 1.5GiB
		Progress:    0.5,
		State:       engine.StateDownloading,
		DownloadBPS: 2 * 1024 * 1024,
		UploadBPS:   3 * 1024 * 1024,
		ETA:         100 * 1e9, // 1m40s
	}
	header := m.renderListHeader(p)
	row := m.renderRow(p, snap, false)[0]

	for _, c := range []struct{ heading, value string }{
		{"Name", "torrent"},
		{"Progress", "━"},
		{"Size", "1.5GiB"},
		{"Status", "downloading"},
		{"↓ Speed", "↓ 2.0MiB/s"},
		{"↑ Speed", "↑ 3.0MiB/s"},
		{"ETA", "1m40s"},
	} {
		h, ok := columnOf(header, c.heading)
		if !ok {
			t.Errorf("no %q in the header:\n%s", c.heading, header)
			continue
		}
		v, ok := columnOf(row, c.value)
		if !ok {
			t.Errorf("no %q in the row:\n%s", c.value, row)
			continue
		}
		if h != v {
			t.Errorf("%q starts at %d but %q at %d\n%s\n%s",
				c.heading, h, c.value, v, header, row)
		}
	}
}
