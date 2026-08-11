package branding

import (
	"strings"
	"testing"
)

// Only the rounded box-drawing set, which keeps two properties: every
// glyph is one cell wide, so Width (a rune count) is the printed width;
// and a font missing them degrades to missing glyphs rather than to
// square corners that read as a maze.
func TestLogoUsesOnlyRoundedBoxDrawing(t *testing.T) {
	const allowed = "╭╮╰╯─│ "
	for i, line := range LogoLines(Logo) {
		for _, r := range line {
			if !strings.ContainsRune(allowed, r) {
				t.Errorf("line %d: %q is not one of %q", i, r, allowed)
			}
		}
	}
}

func TestWidthIsTheWidestRow(t *testing.T) {
	want := 0
	for _, line := range LogoLines(Logo) {
		if n := len([]rune(line)); n > want {
			want = n
		}
	}
	if got := Width(Logo); got != want {
		t.Errorf("Width = %d, want %d", got, want)
	}
}

// Every row is drawn on its own line, so a trailing newline would render
// as a blank row under the mark.
func TestLogoHasNoTrailingNewline(t *testing.T) {
	if strings.HasSuffix(Logo, "\n") {
		t.Error("the mark ends with a newline")
	}
}
