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

// The screen is where someone goes to find out what the palette accepts,
// so a command missing from it is a command nobody finds. Checked at both
// layouts: the two-column path is a different code path, not a wider
// version of the same one.
func TestHelpListsEveryPaletteCommand(t *testing.T) {
	m := testModel("a")
	m.styles = newStyles(loadTestTheme(t))

	for _, width := range []int{80, 140} {
		got := m.renderHelp(width, 0)
		for _, c := range paletteCommands {
			// The usage carries its arguments, which truncate on a narrow
			// column; the name is the part that has to survive.
			if !strings.Contains(got, c.names[0]) {
				t.Errorf("at width %d the help screen does not mention %q:\n%s", width, c.names[0], got)
			}
		}
	}
}

// The reason the layout splits at all. Stacked, COMMANDS is the last
// section and the first thing lost to a short terminal - which would
// leave the commands documented everywhere except the screen that exists
// to document them.
func TestHelpKeepsCommandsOnAShortTerminal(t *testing.T) {
	m := testModel("a")
	m.styles = newStyles(loadTestTheme(t))

	got := m.renderHelp(120, 24)

	if !strings.Contains(got, "COMMANDS") {
		t.Errorf("the commands section is gone at 120x24:\n%s", got)
	}
	// The last line of the section, so its presence means the whole of it
	// is there rather than the header alone.
	if !strings.Contains(got, ":q") {
		t.Errorf("the commands section is cut short at 120x24:\n%s", got)
	}
}

// One column reads better - full descriptions, nothing truncated - so
// the split has to be what happens when the sections do not otherwise
// fit, not what happens on a wide screen.
func TestHelpSplitsIntoColumnsOnlyWhenItHasTo(t *testing.T) {
	m := testModel("a")
	m.styles = newStyles(loadTestTheme(t))

	short := m.renderHelp(140, 24)
	tall := m.renderHelp(140, 60)
	narrow := m.renderHelp(80, 24)

	// Two columns means the section titles share a line; one column means
	// they cannot.
	if !lineWith(short, "NAVIGATION", "COMMANDS") {
		t.Errorf("at 140x24 the sections are not side by side:\n%s", short)
	}
	if lineWith(tall, "NAVIGATION", "COMMANDS") {
		t.Errorf("at 140x60 the sections were split with room to stack them:\n%s", tall)
	}
	// Too narrow to split: the columns would truncate to nothing, which
	// is worse than the clipping it avoids.
	if lineWith(narrow, "NAVIGATION", "COMMANDS") {
		t.Errorf("at 80x24 the sections were split into columns:\n%s", narrow)
	}
	if strings.Count(short, "\n") >= strings.Count(tall, "\n") {
		t.Error("splitting into columns did not make the screen shorter")
	}
}

// lineWith reports whether any one line contains all of the substrings.
func lineWith(s string, want ...string) bool {
	for _, line := range strings.Split(s, "\n") {
		all := true
		for _, w := range want {
			if !strings.Contains(line, w) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
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
