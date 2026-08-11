package tui

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/branding"
	"github.com/lestex/torrnado/internal/theme"
)

func loadTestTheme(t *testing.T) theme.Theme {
	t.Helper()
	th, err := theme.Load("dracula", t.TempDir())
	if err != nil {
		t.Fatalf("loading a built-in theme: %v", err)
	}
	return th
}

// The mark costs five rows the keys would otherwise have, so it must
// never be the reason the reference gets clipped. A short screen clips
// on its own and that is fine; drawing the logo and then cutting the
// keys off to fit it is not. Swept rather than sampled, because the
// heights either side of the cutoff are exactly where this goes wrong.
func TestHelpDropsTheMarkBeforeItClipsTheKeys(t *testing.T) {
	m := testModel("a")
	m.styles = newStyles(loadTestTheme(t))

	firstMarkRow := branding.LogoLines(branding.Logo)[0]

	sawMark, sawFallback := false, false
	for height := 16; height <= 48; height++ {
		got := m.renderHelp(100, height)
		hasMark := strings.Contains(got, firstMarkRow)

		if hasMark && strings.Contains(got, "clipped") {
			t.Errorf("at height %d the mark was drawn and the keys were clipped to fit it", height)
		}
		sawMark = sawMark || hasMark
		sawFallback = sawFallback || (!hasMark && strings.Contains(got, "torrnado - keys"))
	}

	// Both branches have to be reachable, or the sweep above proves
	// nothing: a header that never draws the mark also never clips.
	if !sawMark {
		t.Error("the mark was never drawn, at any height")
	}
	if !sawFallback {
		t.Error("the one-line title was never used, at any height")
	}
}

// height 0 is "unbounded" everywhere else in this package; the header
// must not read it as "no room".
func TestHelpDrawsTheMarkAtUnboundedHeight(t *testing.T) {
	m := testModel("a")
	m.styles = newStyles(loadTestTheme(t))

	got := m.renderHelp(100, 0)

	if !strings.Contains(got, branding.LogoLines(branding.Logo)[0]) {
		t.Error("an unbounded help screen did not draw the mark")
	}
}
