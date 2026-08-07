// Package branding holds torrnado's mark in the forms a terminal can
// draw it.
//
// The mark is a spiral: a vortex seen from above, for a client named
// after a storm. It exists in three renderings that are meant to stay the
// same shape rather than three separate designs. The vector original is
// overrides/.icons/torrnado/logo.svg, a real Archimedean spiral that
// strokes currentColor so it takes the color of whatever draws it;
// docs/assets/favicon.svg is the same path with the accent baked in,
// because a browser tab has no page for currentColor to inherit from.
// The constants here approximate that curve in character cells.
//
// This is its own package rather than a corner of internal/tui because
// the TUI is not the only thing that will want it: a web or desktop front
// end reaches for the SVG, and anything that prints a banner reaches for
// these. Neither should have to import a bubbletea model to find out what
// the logo is.
package branding

import "strings"

// Logo is the mark at full size, five rows by seven columns. Sized for a
// screen with room to spare, such as a help overlay or a splash.
//
// Drawn with the rounded box-drawing set (U+256D..U+2570) because the
// square corners make the same path read as a maze rather than a curve.
// A terminal font without them degrades to missing glyphs rather than to
// something wrong, which is the failure worth having.
const Logo = "" +
	"╭─────╮\n" +
	"│ ╭─╮ │\n" +
	"│ │ ╰─╯\n" +
	"│ ╰───╯\n" +
	"╰──────"

// LogoSmall is the mark at four columns by three rows, for a sidebar or
// anywhere else a full-size one would crowd the content around it.
//
// The turn count drops from two to one: at this size a second turn is a
// solid block rather than a spiral, which is the same reason the SVG uses
// two turns and not the three that look best on paper.
const LogoSmall = "" +
	"╭──╮\n" +
	"│╭╯\n" +
	"╰╯"

// LogoLines splits a mark into rows, for a caller that lays out line by
// line rather than printing a block.
func LogoLines(logo string) []string {
	return strings.Split(logo, "\n")
}

// Width reports the widest row of a mark, in cells. Every glyph used is
// single width, so a count of runes is the printed width.
func Width(logo string) int {
	w := 0
	for _, line := range LogoLines(logo) {
		if n := len([]rune(line)); n > w {
			w = n
		}
	}
	return w
}
