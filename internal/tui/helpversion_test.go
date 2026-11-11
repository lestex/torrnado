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

// withDaemon is a model that has heard from a daemon of the given build.
func withDaemon(t *testing.T, client, daemon string) Model {
	t.Helper()
	return afterEvent(helpModel(t, client), daemon)
}

// headerOf is the lines of the help screen above its first section.
func headerOf(rendered string) string {
	head, _, _ := strings.Cut(rendered, "NAVIGATION")
	return head
}

// "v0.10.1 is enough": the release, not the commit the build string appends.
func TestHelpShowsTheReleaseNotTheCommit(t *testing.T) {
	got := helpModel(t, thisBuild).renderHelp(100, 0)

	if !strings.Contains(got, "v0.10.1") {
		t.Errorf("the help screen does not show the version:\n%s", got)
	}
	if strings.Contains(got, "da4134de07be") {
		t.Errorf("the commit is shown where the release was wanted:\n%s", got)
	}
}

func TestTheDaemonsVersionIsOnTheSameLine(t *testing.T) {
	got := withDaemon(t, thisBuild, thisBuild).renderHelp(100, 0)

	if !strings.Contains(got, "v0.10.1 | daemon v0.10.1") {
		t.Errorf("want 'v0.10.1 | daemon v0.10.1' on one line:\n%s", got)
	}
}

func TestAMismatchShowsBothOnOneLine(t *testing.T) {
	got := withDaemon(t, thisBuild, oldBuild).renderHelp(100, 0)

	if !strings.Contains(got, "v0.10.1 | daemon v0.10.0") {
		t.Errorf("want 'v0.10.1 | daemon v0.10.0' on one line:\n%s", got)
	}
}

// One line, however it reads: the header is as tall as the mark whether the
// version is absent, shown alone, shown with the daemon's, or a mismatch.
// A second line for the warning was exactly what this replaced.
func TestTheHeaderIsTheSameHeightWhateverItSays(t *testing.T) {
	want := strings.Count(helpModel(t, "").renderHelp(100, 0), "\n")

	for name, m := range map[string]Model{
		"this build only": helpModel(t, thisBuild),
		"matching daemon": withDaemon(t, thisBuild, thisBuild),
		"mismatched":      withDaemon(t, thisBuild, oldBuild),
		"daemon too old":  withDaemon(t, thisBuild, ""),
		"same release":    withDaemon(t, "0.10.1 (aaaaaaaaaaaa)", "0.10.1 (bbbbbbbbbbbb)"),
	} {
		if got := strings.Count(m.renderHelp(100, 0), "\n"); got != want {
			t.Errorf("%s: %d lines, want %d - the header grew", name, got, want)
		}
	}
}

// The same release can be two different builds: a client rebuilt over a
// daemon left running. Short forms would read as equal beside a warning
// that says they are not, so the commits are said.
func TestTheSameReleaseOfAnotherBuildNamesTheCommits(t *testing.T) {
	m := withDaemon(t, "0.10.1 (aaaaaaaaaaaa)", "0.10.1 (bbbbbbbbbbbb)")

	got := headerOf(m.renderHelp(120, 0))

	for _, want := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"} {
		if !strings.Contains(got, want) {
			t.Errorf("the header does not say %q:\n%s", want, got)
		}
	}
}

// A daemon older than the version field says nothing, which is not the
// same as not having spoken yet.
func TestADaemonTooOldToSayItsVersionIsSaidSo(t *testing.T) {
	got := withDaemon(t, thisBuild, "").renderHelp(100, 0)

	if !strings.Contains(got, "v0.10.1 | daemon too old") {
		t.Errorf("got:\n%s", got)
	}
}

// Before the first event nothing is known about the daemon's version, and
// showing "daemon" beside a blank would say something false.
func TestNothingIsSaidAboutTheDaemonBeforeItHasSpoken(t *testing.T) {
	got := headerOf(helpModel(t, thisBuild).renderHelp(100, 0))

	if !strings.Contains(got, "v0.10.1") {
		t.Errorf("this build is missing:\n%s", got)
	}
	if strings.Contains(got, "daemon") {
		t.Errorf("said something about a daemon not yet heard from:\n%s", got)
	}
}

// Both header forms carry it, or a short terminal - exactly where the
// one-line title is used - would be the one place it was missing.
func TestBothHeaderFormsCarryBothVersions(t *testing.T) {
	m := withDaemon(t, thisBuild, oldBuild)
	mark := branding.LogoLines(branding.Logo)[0]

	var sawFull, sawOneLine bool
	for height := 16; height <= 48; height++ {
		got := m.renderHelp(100, height)
		if !strings.Contains(got, "v0.10.1 | daemon v0.10.0") {
			t.Errorf("at height %d the versions are missing:\n%s", height, got)
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

func TestTheOneLineTitleReadsAsTitleVersionsKeys(t *testing.T) {
	got := withDaemon(t, thisBuild, oldBuild).helpTitleLine(100)

	if !strings.Contains(got, "torrnado") || !strings.Contains(got, "v0.10.1") ||
		!strings.Contains(got, "daemon v0.10.0") || !strings.Contains(got, "keys") {
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
	got := m.renderHelp(100, 0)
	if !strings.Contains(got, "keys & commands") || strings.Contains(got, "daemon v") {
		t.Errorf("full header:\n%s", got)
	}
}

// The line is one of three beside a three-row mark, so the budget that
// decides when the mark is dropped is the one it always was - and the sweep
// shows it: the full header is never drawn at a height that clips the keys.
func TestTheVersionLineNeverCostsTheKeysARow(t *testing.T) {
	m := withDaemon(t, thisBuild, oldBuild)
	mark := branding.LogoLines(branding.Logo)[0]

	var sawMark bool
	for height := 16; height <= 48; height++ {
		got := m.renderHelp(120, height)
		if strings.Contains(got, mark) {
			sawMark = true
			if strings.Contains(got, "clipped") {
				t.Errorf("at height %d the full header was drawn and the keys were clipped to fit it", height)
			}
		}
	}
	if !sawMark {
		t.Error("the full header was never drawn, so the sweep proves nothing")
	}
}

// A development build's version is long, and two of them longer. Cut to
// fit rather than wrapped by the pane: a wrapped header line pushes a row
// of the reference off the bottom.
func TestALongVersionIsCutNotWrapped(t *testing.T) {
	m := withDaemon(t, "v0.10.1-1-g8b8ecbf-dirty (8b8ecbf2105d-dirty)", "v0.10.0-12-gabcdef0-dirty (abcdef0123456)")

	for _, width := range []int{40, 50, 60, 80} {
		for height := 0; height <= 40; height += 20 {
			for i, line := range strings.Split(m.renderHelp(width, height), "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("width %d height %d: line %d is %d cells wide:\n%s", width, height, i, w, line)
				}
			}
		}
	}
}

func TestShortVersion(t *testing.T) {
	for in, want := range map[string]string{
		"0.10.1 (da4134de07be)":                         "v0.10.1", // a release build: stamped without the v
		"v0.10.1 (da4134de07be)":                        "v0.10.1",
		"v0.10.1-1-g8b8ecbf-dirty (8b8ecbf2105d-dirty)": "v0.10.1-1-g8b8ecbf-dirty", // a working tree says so
		"0.10.1":       "v0.10.1", // no commit to drop
		"dev":          "dev",     // not a version: no v invented
		"dev (abc123)": "dev",
		"":             "",
	} {
		if got := shortVersion(in); got != want {
			t.Errorf("shortVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// The whole path, from the key: pressing help shows the version.
func TestPressingHelpShowsTheVersion(t *testing.T) {
	m := withDaemon(t, thisBuild, thisBuild)
	m.width, m.height = 120, 40

	m = press(m, m.keymap.Help)

	if !m.showHelp {
		t.Fatal("the help key did not open the help screen")
	}
	if got := m.View(); !strings.Contains(got, "v0.10.1 | daemon v0.10.1") {
		t.Errorf("the screen opened by the help key does not show the versions:\n%s", got)
	}
}
