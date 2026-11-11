package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/branding"
)

func helpModel(t *testing.T, version string) Model {
	t.Helper()
	m := testModel("a")
	m.styles = newStyles(loadTestTheme(t))
	m.clientVersion = version
	return m
}

func TestHelpSaysWhichBuildThisIs(t *testing.T) {
	got := helpModel(t, thisBuild).renderHelp(100, 0)

	if !strings.Contains(got, thisBuild) {
		t.Errorf("the help screen does not show the version:\n%s", got)
	}
}

// The version sits under the wordmark, which is as many lines as the mark
// is tall, so showing it must not cost the reference a single row.
func TestTheVersionCostsTheKeysNothing(t *testing.T) {
	with := helpModel(t, thisBuild).renderHelp(100, 0)
	without := helpModel(t, "").renderHelp(100, 0)

	if a, b := strings.Count(with, "\n"), strings.Count(without, "\n"); a != b {
		t.Errorf("the version added %d row(s) to the header", a-b)
	}
}

// Both header forms have to carry it, or a short terminal - exactly where
// the one-line title is used - would be the one place it was missing.
func TestBothHeaderFormsCarryTheVersion(t *testing.T) {
	m := helpModel(t, thisBuild)
	mark := branding.LogoLines(branding.Logo)[0]

	var sawFull, sawOneLine bool
	for height := 16; height <= 48; height++ {
		got := m.renderHelp(100, height)
		if !strings.Contains(got, thisBuild) {
			t.Errorf("at height %d the version is missing:\n%s", height, got)
		}
		if strings.Contains(got, mark) {
			sawFull = true
		} else {
			sawOneLine = true
		}
	}
	if !sawFull || !sawOneLine {
		t.Errorf("the sweep missed a form: full=%v oneLine=%v", sawFull, sawOneLine)
	}
}

func TestOneLineTitleReadsAsTitleVersionKeys(t *testing.T) {
	got := helpModel(t, thisBuild).helpTitleLine(100)

	if !strings.Contains(got, "torrnado") || !strings.Contains(got, thisBuild) || !strings.Contains(got, "keys") {
		t.Errorf("got %q", got)
	}
}

// A build with no version - a test, an embedder - gets the header it
// always had, not one with a hole in it.
func TestWithNoVersionTheHeaderIsAsItWas(t *testing.T) {
	m := helpModel(t, "")

	if got := m.helpTitleLine(100); !strings.Contains(got, "torrnado - keys") {
		t.Errorf("one-line title: %q", got)
	}
	if got := m.renderHelp(100, 0); !strings.Contains(got, "keys & commands") {
		t.Errorf("full header lost its subtitle:\n%s", got)
	}
}

// The sidebar's warning is not on screen while help is open - help replaces
// the panes - so the screen has to say it itself.
func TestAMismatchIsSaidOnTheHelpScreenToo(t *testing.T) {
	m := afterEvent(helpModel(t, thisBuild), oldBuild)

	full := m.renderHelp(120, 0)
	if !strings.Contains(full, oldBuild) {
		t.Errorf("the full header does not name the daemon's build:\n%s", full)
	}
	if line := m.helpTitleLine(120); !strings.Contains(line, "daemon differs") {
		t.Errorf("the one-line title does not flag it: %q", line)
	}
}

// The warning is a fourth header line, beside a three-row mark. The budget
// that decides when the mark is dropped has to count it, or it is drawn
// and then the last of the reference is cut to make room.
func TestAWarningNeverCostsTheKeysARow(t *testing.T) {
	m := afterEvent(helpModel(t, thisBuild), oldBuild)
	mark := branding.LogoLines(branding.Logo)[0]

	var sawMark bool
	for height := 16; height <= 48; height++ {
		got := m.renderHelp(120, height)
		if strings.Contains(got, mark) {
			sawMark = true
			if strings.Contains(got, "clipped") {
				t.Errorf("at height %d the header with a warning was drawn and the keys were clipped to fit it", height)
			}
		}
	}
	if !sawMark {
		t.Error("the full header was never drawn, so the sweep proves nothing")
	}
}

// A real build string is long, and a daemon's makes a longer note. Cut
// to fit rather than wrapped by the pane: a wrapped header line pushes a
// row of the reference off the bottom.
func TestALongBuildStringIsCutNotWrapped(t *testing.T) {
	long := "v0.10.1-dirty (da4134de07be-dirty) with some extra build text"
	m := afterEvent(helpModel(t, long), "0.10.0 (a649a39b4f79) and then some")

	for _, width := range []int{50, 60, 80} {
		for height := 0; height <= 40; height += 20 {
			for i, line := range strings.Split(m.renderHelp(width, height), "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("width %d height %d: line %d is %d cells wide:\n%s", width, height, i, w, line)
				}
			}
		}
	}
}

// The whole path, from the key: pressing help shows the version.
func TestPressingHelpShowsTheVersion(t *testing.T) {
	m := helpModel(t, thisBuild)
	m.width, m.height = 120, 40

	m = press(m, m.keymap.Help)

	if !m.showHelp {
		t.Fatal("the help key did not open the help screen")
	}
	if got := m.View(); !strings.Contains(got, thisBuild) {
		t.Errorf("the screen opened by the help key does not show the version:\n%s", got)
	}
}
