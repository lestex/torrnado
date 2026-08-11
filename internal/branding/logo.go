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
// Logo approximates that curve in character cells.
//
// This is its own package rather than a corner of internal/tui because
// the TUI is not the only thing that will want it: a web or desktop front
// end reaches for the SVG, and anything that prints a banner reaches for
// these. Neither should have to import a bubbletea model to find out what
// the logo is.
package branding

import "strings"

// Logo is the mark in character cells, four columns by three rows. It
// heads the help screen, which is the only place the terminal draws it.
//
// Drawn with the rounded box-drawing set (U+256D..U+2570) because square
// corners make the same path read as a maze. A font without them degrades
// to missing glyphs rather than to something wrong.
const Logo = "" +
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
