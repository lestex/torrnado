package tui

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

const (
	thisBuild = "0.10.1 (da4134de07be)"
	oldBuild  = "0.10.0 (a649a39b4f79)"
)

// afterEvent delivers one pushed event, the way the daemon's stream does.
func afterEvent(m Model, version string) Model {
	next, _ := m.Update(engineEventMsg{Global: engine.GlobalStats{Version: version}})
	return next.(Model)
}

func builtAs(version string) Model {
	m := testModel("a")
	m.clientVersion = version
	return m
}

// The daemon outlives its clients, and the wire has no handshake, so a
// new interface on an old daemon finds something quietly missing and
// nothing to say why. The first event is when this can first be known.
func TestADaemonOfAnotherBuildIsSaidOnTheFirstEvent(t *testing.T) {
	m := afterEvent(builtAs(thisBuild), oldBuild)

	for _, want := range []string{oldBuild, thisBuild, "torrnado stop"} {
		if !strings.Contains(m.status, want) {
			t.Errorf("the message does not mention %q: %q", want, m.status)
		}
	}
	if !m.statusIsErr {
		t.Error("a mismatch is drawn as an ordinary message, not one that gets noticed")
	}
}

func TestAndTheSidebarKeepsSayingItForAsLongAsItIsTrue(t *testing.T) {
	p := layout(120, 30)

	m := afterEvent(builtAs(thisBuild), oldBuild)
	if got := m.renderSidebar(p); !strings.Contains(got, "! daemon differs") {
		t.Errorf("the sidebar says nothing about it:\n%s", got)
	}

	// The status line is gone a few seconds later; the condition is not.
	m.clearStatus(m.statusSeq)
	if m.status != "" {
		t.Fatalf("the status did not clear: %q", m.status)
	}
	if got := m.renderSidebar(p); !strings.Contains(got, "! daemon differs") {
		t.Errorf("the warning went away with the message:\n%s", got)
	}
}

// Said once: a daemon cannot become another build, so repeating it every
// tick would only bury whatever else wanted the status line.
func TestTheMessageIsNotRepeatedOnEveryTick(t *testing.T) {
	m := afterEvent(builtAs(thisBuild), oldBuild)
	m.clearStatus(m.statusSeq)

	m = afterEvent(m, oldBuild)

	if m.status != "" {
		t.Errorf("the second event said it again: %q", m.status)
	}
}

func TestTheSameBuildSaysNothingAnywhere(t *testing.T) {
	m := afterEvent(builtAs(thisBuild), thisBuild)

	if m.status != "" {
		t.Errorf("a matching daemon produced a message: %q", m.status)
	}
	if got := m.renderSidebar(layout(120, 30)); strings.Contains(got, "differs") {
		t.Errorf("a matching daemon drew a warning:\n%s", got)
	}
}

// Before the first event the daemon's version is simply not known yet, and
// the empty string it starts as would read as a daemon too old to report
// one. Nothing is flagged until something has actually been heard.
func TestNothingIsFlaggedBeforeTheDaemonHasSpoken(t *testing.T) {
	m := builtAs(thisBuild)

	if m.status != "" || m.versionNote != "" {
		t.Errorf("flagged before any event: status=%q note=%q", m.status, m.versionNote)
	}
	if got := m.renderSidebar(layout(120, 30)); strings.Contains(got, "differs") {
		t.Errorf("drew a warning before any event:\n%s", got)
	}
}

// A daemon older than the version field reports nothing, which is a
// daemon older than this build and not "unknown, so fine".
func TestADaemonTooOldToReportItsVersionIsFlagged(t *testing.T) {
	m := afterEvent(builtAs(thisBuild), "")

	if !strings.Contains(m.status, "too old") {
		t.Errorf("got %q, want the daemon called too old", m.status)
	}
}

// A model built with no version - a test, an embedder - has nothing to
// compare and must not complain about every daemon it meets.
func TestAnInterfaceWithNoVersionNeverComplains(t *testing.T) {
	m := afterEvent(builtAs(""), oldBuild)

	if m.status != "" || m.versionNote != "" {
		t.Errorf("complained with no version to compare: status=%q note=%q", m.status, m.versionNote)
	}
}

// The label section is told how many rows the daemon block wants, so the
// port, dht and disk readings are not pushed off the bottom of the pane.
func TestTheDaemonBlockReservesARowForTheWarning(t *testing.T) {
	plain := builtAs(thisBuild)
	skewed := afterEvent(builtAs(thisBuild), oldBuild)

	if got, want := skewed.daemonBlockRows(), plain.daemonBlockRows()+1; got != want {
		t.Errorf("daemonBlockRows = %d with the warning, want %d", got, want)
	}
}
