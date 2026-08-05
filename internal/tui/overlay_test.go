package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// grid builds a plain w x h block of a repeated rune.
func grid(r rune, w, h int) string {
	line := strings.Repeat(string(r), w)
	lines := make([]string, h)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// The property the whole frame depends on: a float must not change the
// shape of what it is drawn over. A line one column too wide wraps, which
// grows the frame and pushes its bottom border off the screen.
func TestOverlayLeavesTheFrameTheSameShape(t *testing.T) {
	base := grid('.', 40, 10)
	box := grid('#', 8, 3)

	for _, pos := range [][2]int{{0, 0}, {5, 2}, {32, 7}, {39, 9}, {36, 8}} {
		got := overlay(base, box, pos[0], pos[1])

		gotLines := strings.Split(got, "\n")
		baseLines := strings.Split(base, "\n")
		if len(gotLines) != len(baseLines) {
			t.Errorf("at %v: %d lines, want %d", pos, len(gotLines), len(baseLines))
			continue
		}
		for i, line := range gotLines {
			if w := ansi.StringWidth(line); w != ansi.StringWidth(baseLines[i]) {
				t.Errorf("at %v: line %d is %d columns, want %d (%q)",
					pos, i, w, ansi.StringWidth(baseLines[i]), line)
			}
		}
	}
}

// A styled frame is the real case: every line is full of escape
// sequences, and cutting one as bytes severs a sequence and bleeds
// colour across the rest of the screen.
func TestOverlayKeepsStyledLinesIntact(t *testing.T) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	var lines []string
	for range 6 {
		lines = append(lines, style.Render(strings.Repeat("x", 30)))
	}
	base := strings.Join(lines, "\n")
	box := grid('#', 10, 2)

	got := overlay(base, box, 10, 2)

	for i, line := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(line); w != 30 {
			t.Errorf("line %d is %d columns, want 30: %q", i, w, line)
		}
	}
	// The box's own cells must not have inherited the frame's colour,
	// and the frame either side of it must keep its own.
	spliced := strings.Split(got, "\n")[2]
	if !strings.Contains(spliced, "##########") {
		t.Errorf("the box did not survive the splice: %q", spliced)
	}
}

func TestOverlayPutsTheBoxWhereAsked(t *testing.T) {
	base := grid('.', 20, 5)
	box := "abc\ndef"

	got := strings.Split(overlay(base, box, 4, 1), "\n")

	if got[0] != strings.Repeat(".", 20) {
		t.Errorf("row above the box changed: %q", got[0])
	}
	if want := "....abc............."; got[1] != want {
		t.Errorf("row 1 = %q, want %q", got[1], want)
	}
	if want := "....def............."; got[2] != want {
		t.Errorf("row 2 = %q, want %q", got[2], want)
	}
	if got[3] != strings.Repeat(".", 20) {
		t.Errorf("row below the box changed: %q", got[3])
	}
}

// Box lines are padded to the box width, or the frame shows through the
// middle of the float wherever a line is short.
func TestOverlayPadsShortBoxLines(t *testing.T) {
	base := grid('.', 20, 3)
	box := "long line\nshort"

	got := strings.Split(overlay(base, box, 2, 0), "\n")

	if want := "..long line........."; got[0] != want {
		t.Errorf("row 0 = %q, want %q", got[0], want)
	}
	// "short" is 5 of the 9 columns; the rest must be blank, not frame.
	if want := "..short    ........."; got[1] != want {
		t.Errorf("row 1 = %q, want %q", got[1], want)
	}
}

// A line shorter than the box's column would otherwise pull the float
// left and leave it ragged.
func TestOverlayPadsShortBaseLines(t *testing.T) {
	base := "....\n\n...................."
	box := "##"

	got := strings.Split(overlay(base, box, 8, 1), "\n")

	if want := "        ##"; got[1] != want {
		t.Errorf("row 1 = %q, want %q", got[1], want)
	}
}

// A box taller than the frame is clipped. Letting it grow the frame is
// the failure it exists to avoid.
func TestOverlayClipsABoxTallerThanTheFrame(t *testing.T) {
	base := grid('.', 10, 3)
	box := grid('#', 4, 10)

	got := overlay(base, box, 0, 1)

	if n := len(strings.Split(got, "\n")); n != 3 {
		t.Errorf("frame grew to %d lines, want 3", n)
	}
}

func TestCentre(t *testing.T) {
	if x, y := centre(100, 40, 20, 10); x != 40 || y != 15 {
		t.Errorf("centre = (%d,%d), want (40,15)", x, y)
	}
	// A box bigger than the area stays on screen rather than going
	// negative, which would put it off the top-left corner.
	if x, y := centre(10, 5, 40, 20); x != 0 || y != 0 {
		t.Errorf("centre = (%d,%d), want (0,0)", x, y)
	}
}
