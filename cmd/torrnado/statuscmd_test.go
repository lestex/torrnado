package main

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/config"
	"github.com/lestex/torrnado/internal/ipc"
)

// The not-running report is the one a reader hits when something is
// already wrong, so it has to say where it looked - a status that omits
// the socket cannot be told apart from one pointed at the wrong state
// directory.
func TestStatusReportWhenNothingIsRunning(t *testing.T) {
	cfg := config.Config{DaemonSocket: "/tmp/tn-test/daemon.sock"}
	info := ipc.DaemonStatus{Running: false, LockPath: cfg.DaemonSocket + ".lock"}

	var out strings.Builder
	if err := writeStatusReport(&out, cfg, info); err != nil {
		t.Fatalf("writeStatusReport: %v", err)
	}
	got := out.String()

	for _, want := range []string{"not running", cfg.DaemonSocket, info.LockPath, "torrnado daemon"} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not mention %q:\n%s", want, got)
		}
	}
}

// A daemon that predates the pid being recorded still holds the lock, and
// it is normal to meet one: the daemon outliving its clients is the point
// of the design. Reporting a bare "0" would read as a bug in the daemon.
func TestPidTextExplainsAnUnknownPid(t *testing.T) {
	if got := pidText(0); !strings.Contains(got, "unknown") {
		t.Errorf("pidText(0) = %q, want it to say the pid is unknown", got)
	}
	if got := pidText(4242); got != "4242" {
		t.Errorf("pidText(4242) = %q, want %q", got, "4242")
	}
}

// Same for the fields an older daemon leaves at their gob zero value.
func TestUnknownFieldsSayWhy(t *testing.T) {
	if got := orUnknown(""); !strings.Contains(got, "too old") {
		t.Errorf("orUnknown(\"\") = %q, want it to name the reason", got)
	}
	if got := orUnknown("v0.5.3"); got != "v0.5.3" {
		t.Errorf("orUnknown passed through wrong: %q", got)
	}
	if got := orUnknownInt(0); got != "unknown" {
		t.Errorf("orUnknownInt(0) = %q", got)
	}
}
