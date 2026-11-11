package main

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

const (
	clientBuild = "0.10.1 (da4134de07be)"
	daemonBuild = "0.10.0 (a649a39b4f79)"
)

func skewReport(client, daemon string) string {
	var b strings.Builder
	writeVersionSkew(&b, client, engine.GlobalStats{Version: daemon})
	return b.String()
}

func TestStatusSaysWhenTheDaemonIsAnotherBuild(t *testing.T) {
	got := skewReport(clientBuild, daemonBuild)

	for _, want := range []string{"Warning", daemonBuild, clientBuild, "torrnado stop", "next command"} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not say %q:\n%s", want, got)
		}
	}
}

// The advice is conditional because build strings are not ordered: this
// cannot know the daemon is the older one, and restarting only helps then.
func TestTheAdviceDoesNotAssumeWhichSideIsOld(t *testing.T) {
	if got := skewReport(clientBuild, daemonBuild); !strings.Contains(got, "If the") {
		t.Errorf("the advice is stated as certain:\n%s", got)
	}
}

func TestStatusSaysNothingWhenTheBuildsMatch(t *testing.T) {
	if got := skewReport(clientBuild, clientBuild); got != "" {
		t.Errorf("a matching daemon produced output:\n%s", got)
	}
}

func TestStatusCallsADaemonThatCannotReportItsVersionTooOld(t *testing.T) {
	if got := skewReport(clientBuild, ""); !strings.Contains(got, "too old") {
		t.Errorf("an unversioned daemon was not called too old:\n%s", got)
	}
}
