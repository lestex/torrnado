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
// labeled 100%.
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
// along a torrent is beats how fast it is going. At that width progress
// is the percentage alone - there is no room for a bar.
func TestNarrowPanesKeepNameAndProgress(t *testing.T) {
	m := testModel("a")
	p := layout(minWidth, 40)

	if _, wide := nameWidth(p.listContentW); wide {
		t.Fatalf("a %d-column terminal should not fit the full column set", minWidth)
	}

	snap := engine.TorrentSnapshot{Name: "torrent", TotalLength: 1024, Progress: 0.5, DownloadBPS: 5000}
	row := m.renderRow(p, snap, false)[0]

	if !strings.Contains(row, "50%") {
		t.Errorf("a narrow row dropped the progress: %q", row)
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
// allocated - and the trim must not cut through the styling's escape
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

// The bar grows with the pane, because a width that reads well at 160
// columns looks like a rounding error across a 4K screen.
func TestTheBarGrowsWithThePane(t *testing.T) {
	if got := barWidth(400); got != maxBarWidth {
		t.Errorf("a very wide pane has a %d-cell bar, want it capped at %d", got, maxBarWidth)
	}
	if barWidth(180) <= barWidth(140) {
		t.Errorf("the bar did not grow between 140 and 180 columns: %d then %d",
			barWidth(140), barWidth(180))
	}

	prev := 0
	for w := 20; w <= 600; w++ {
		got := barWidth(w)
		if got < prev {
			t.Fatalf("bar shrank from %d to %d between %d and %d columns", prev, got, w-1, w)
		}
		// Either there is no bar, or it is long enough to read as one.
		if got != 0 && (got < minBarWidth || got > maxBarWidth) {
			t.Fatalf("bar is %d cells at %d columns, outside [%d,%d]",
				got, w, minBarWidth, maxBarWidth)
		}
		prev = got
	}
}

// Drawing the bar must not take anything away: any pane that could show
// the full column set without one still shows it with one.
func TestTheBarNeverCostsTheOtherColumns(t *testing.T) {
	for w := 20; w <= 600; w++ {
		fitsWithoutABar := w-fixedColumnsWith(0) >= minNameWidth
		_, wide := nameWidth(w)

		if fitsWithoutABar && !wide {
			t.Errorf("%d columns fit the full set with no bar but dropped to the narrow "+
				"layout with a %d-cell one", w, barWidth(w))
		}
	}
}

// Names come first. A pane that cannot afford both shows the percentage
// alone and spends the room on the name instead - a name truncated
// beside a stub of a bar serves nobody.
func TestATightPaneSpendsTheRoomOnTheName(t *testing.T) {
	// Wide enough for the full column set, not wide enough for a bar.
	const tight = 116

	if bar := barWidth(tight); bar != 0 {
		t.Fatalf("a %d-column pane drew a %d-cell bar; this test needs the tight case",
			tight, bar)
	}
	nameW, wide := nameWidth(tight)
	if !wide {
		t.Fatalf("a %d-column pane should still fit the full column set", tight)
	}

	// The name is longer by exactly what the bar would have cost.
	withBar, _ := nameWidth(tight + 2*(minBarWidth+colGap))
	if nameW <= withBar-minBarWidth {
		t.Errorf("name is %d columns without a bar; a bar would have left it %d",
			nameW, withBar)
	}

	// And the heading names what is actually shown.
	if got := progressHeading(0); got != "%" {
		t.Errorf("heading over a bare percentage is %q, want %q", got, "%")
	}
	if got := progressHeading(minBarWidth); got != "Progress" {
		t.Errorf("heading over a bar is %q, want %q", got, "Progress")
	}
}

// The cursor and the selection are independent, and one marker cell
// could only ever show one of them - so a selected row under the cursor
// looked unselected, and there was no telling which of several selected
// rows the cursor was on.
func TestCursorAndSelectionAreBothVisible(t *testing.T) {
	m := testModel("a", "b")
	p := layout(200, 40)

	snap := engine.TorrentSnapshot{ID: "a", Name: "a", TotalLength: 100}

	m.selected = map[engine.TorrentID]bool{}
	plain := m.renderRow(p, snap, false)[0]
	cursor := m.renderRow(p, snap, true)[0]

	m.selected = map[engine.TorrentID]bool{"a": true}
	selected := m.renderRow(p, snap, false)[0]
	both := m.renderRow(p, snap, true)[0]

	if !strings.Contains(cursor, ">") {
		t.Errorf("the cursor row has no cursor marker: %q", cursor)
	}
	if !strings.Contains(selected, "*") {
		t.Errorf("a selected row has no selection marker: %q", selected)
	}
	if !strings.Contains(both, ">") || !strings.Contains(both, "*") {
		t.Errorf("a selected row under the cursor shows only one marker: %q", both)
	}
	if strings.Contains(plain, ">") || strings.Contains(plain, "*") {
		t.Errorf("an ordinary row is marked: %q", plain)
	}

	// All four states are told apart by their styling too, not only by
	// the markers: the row under the cursor used to lose the selection's
	// background entirely.
	for _, pair := range [][2]string{
		{plain, cursor}, {plain, selected}, {plain, both},
		{cursor, selected}, {cursor, both}, {selected, both},
	} {
		if ansiPrefix(pair[0]) == ansiPrefix(pair[1]) && pair[0] == pair[1] {
			t.Errorf("two different row states render identically:\n%q\n%q", pair[0], pair[1])
		}
	}
}

// Every row is the same width whatever its markers, or the columns walk
// left and right as the cursor moves.
func TestMarkersDoNotChangeTheRowWidth(t *testing.T) {
	m := testModel("a")
	p := layout(200, 40)
	snap := engine.TorrentSnapshot{ID: "a", Name: "a", TotalLength: 100}

	m.selected = map[engine.TorrentID]bool{}
	plain := lipgloss.Width(m.renderRow(p, snap, false)[0])
	cursor := lipgloss.Width(m.renderRow(p, snap, true)[0])

	m.selected = map[engine.TorrentID]bool{"a": true}
	both := lipgloss.Width(m.renderRow(p, snap, true)[0])

	if plain != cursor || plain != both {
		t.Errorf("row widths differ by marker state: plain %d, cursor %d, both %d",
			plain, cursor, both)
	}
	if header := lipgloss.Width(m.renderListHeader(p)); header != plain {
		t.Errorf("header is %d columns and a row is %d", header, plain)
	}
}

// The first screen a new user sees. "no torrents yet" answers a question
// nobody asked; what to do about it is the one they have, so both ways
// to add a torrent and the way to the reference have to be on it.
func TestEmptyListSaysWhatToDoNext(t *testing.T) {
	m := testModel()
	m.styles = newStyles(loadTestTheme(t))

	got := m.renderEmptyList(80, 10)

	for _, want := range []string{"no torrents yet", ":add", "torrnado add", "h / ?"} {
		if !strings.Contains(got, want) {
			t.Errorf("the empty list does not mention %q:\n%s", want, got)
		}
	}
}

// It is drawn into a pane that has already been measured, so it must not
// hand back more lines than it was given - lipgloss grows the box rather
// than clipping, which pushes the frame off the screen.
func TestEmptyListFitsTheHeightItIsGiven(t *testing.T) {
	m := testModel()
	m.styles = newStyles(loadTestTheme(t))

	for height := 1; height <= 6; height++ {
		if got := strings.Count(m.renderEmptyList(80, height), "\n") + 1; got > height {
			t.Errorf("at height %d the empty list rendered %d lines", height, got)
		}
	}
}
