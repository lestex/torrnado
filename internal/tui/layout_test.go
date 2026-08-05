package tui

import "testing"

// The panes have to tile the terminal exactly. A single cell unaccounted
// for shows up as a gap or, worse, as content pushed past the bottom of
// the screen taking the frame with it.
func TestLayoutTilesTheTerminal(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 24}, {120, 40}, {200, 60}, {minWidth, minHeight}, {61, 25},
	}
	for _, s := range sizes {
		p := layout(s.w, s.h)

		if got := p.sidebarW + p.listW; got != s.w {
			t.Errorf("%dx%d: sidebar+list = %d columns, want %d", s.w, s.h, got, s.w)
		}
		if got := p.sidebarW + p.detailW; got != s.w {
			t.Errorf("%dx%d: sidebar+detail = %d columns, want %d", s.w, s.h, got, s.w)
		}
		// One row goes to the footer; the panes share the rest.
		if got := p.listH + p.detailH; got != s.h-1 {
			t.Errorf("%dx%d: list+detail = %d rows, want %d", s.w, s.h, got, s.h-1)
		}
		if p.sidebarH != s.h-1 {
			t.Errorf("%dx%d: sidebar is %d rows, want %d", s.w, s.h, p.sidebarH, s.h-1)
		}
	}
}

// Content areas are what is left inside the borders, and must never go
// negative -- a negative width silently becomes a very wide one once it
// reaches strings.Repeat.
func TestLayoutContentAreasArePositive(t *testing.T) {
	for _, s := range []struct{ w, h int }{{minWidth, minHeight}, {80, 24}, {300, 100}} {
		p := layout(s.w, s.h)
		for name, v := range map[string]int{
			"sidebarContentW": p.sidebarContentW, "sidebarContentH": p.sidebarContentH,
			"listContentW": p.listContentW, "listContentH": p.listContentH,
			"detailContentW": p.detailContentW, "detailContentH": p.detailContentH,
		} {
			if v < 0 {
				t.Errorf("%dx%d: %s = %d", s.w, s.h, name, v)
			}
		}
	}
}

// On a short terminal the detail pane gives up its body so the list stays
// usable, rather than both being squeezed into uselessness.
func TestLayoutShrinksDetailOnShortTerminals(t *testing.T) {
	tall := layout(100, 40)
	short := layout(100, 20)

	if short.detailH >= tall.detailH {
		t.Errorf("detail pane is %d rows on a short terminal and %d on a tall one",
			short.detailH, tall.detailH)
	}
	if short.listH < 4 {
		t.Errorf("list is down to %d rows, too few to be useful", short.listH)
	}
}
