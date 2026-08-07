package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// overlay splices box into base at (x, y), leaving base's dimensions
// exactly as they were.
//
// This is how a floating window is drawn at all here: lipgloss cannot
// composite. Joining two blocks makes a taller string, and Place
// positions a block inside a region rather than layering one over
// another -- so the frame is rendered as usual and the box is cut into
// it, a row at a time.
//
// The cutting has to understand ANSI. Every line of the frame is full of
// escape sequences, and slicing one as bytes severs a sequence in the
// middle: the terminal then reads the tail of a color code as text and
// paints the rest of the screen with whatever it did manage to parse.
// ansi.Truncate and ansi.TruncateLeft measure printable width and, more
// to the point, re-emit the style that was active at the cut -- which is
// what TruncateLeft's prefix argument is for.
func overlay(base, box string, x, y int) string {
	if box == "" {
		return base
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	lines := strings.Split(base, "\n")
	boxLines := strings.Split(box, "\n")

	boxW := 0
	for _, line := range boxLines {
		if w := ansi.StringWidth(line); w > boxW {
			boxW = w
		}
	}

	// The frame is rectangular, so its width is the widest row. A box
	// running past the right edge is clipped to it rather than allowed
	// to make those rows wider than the rest -- an over-wide row wraps,
	// and one wrapped row pushes everything below it down and the
	// bottom border off the screen.
	frameW := 0
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > frameW {
			frameW = w
		}
	}
	if x >= frameW {
		return base
	}
	if avail := frameW - x; boxW > avail {
		boxW = avail
	}

	for i, boxLine := range boxLines {
		row := y + i
		if row < 0 || row >= len(lines) {
			// A box taller than the frame is clipped rather than
			// allowed to grow it: a frame one row too tall pushes its
			// own bottom border off the screen.
			break
		}

		line := lines[row]
		// A short line would otherwise pull the box left, out of its
		// column, and leave the float ragged.
		if w := ansi.StringWidth(line); w < x {
			line += strings.Repeat(" ", x-w)
		}

		left := ansi.Truncate(line, x, "")
		right := ansi.TruncateLeft(line, x+boxW, "")
		// Box lines are padded to the box's width so a short one does
		// not let the frame show through the middle of the float, and
		// clipped to it so a long one cannot widen the frame.
		switch w := ansi.StringWidth(boxLine); {
		case w < boxW:
			boxLine += strings.Repeat(" ", boxW-w)
		case w > boxW:
			boxLine = ansi.Truncate(boxLine, boxW, "")
		}
		lines[row] = left + boxLine + right
	}

	return strings.Join(lines, "\n")
}

// centre returns the top-left corner that centres a boxW x boxH box in a
// width x height area, kept on screen when the box is the larger of the
// two.
func centre(width, height, boxW, boxH int) (x, y int) {
	return max(0, (width-boxW)/2), max(0, (height-boxH)/2)
}
