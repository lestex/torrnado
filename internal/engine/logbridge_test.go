package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	anacrolixlog "github.com/anacrolix/log"
)

// The library's level is separate from ours precisely so its noise can be
// turned down without turning ours down too, which only works if the
// wrapper actually drops the records below it.
func TestLibraryHandlerDropsRecordsBelowItsOwnLevel(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	lib := slog.New(libraryHandler{h: base, min: slog.LevelWarn})

	lib.Info("chatter from a tracker")
	lib.Warn("tracker refused the announce")

	out := buf.String()
	if strings.Contains(out, "chatter") {
		t.Errorf("a record below the library level was written: %q", out)
	}
	if !strings.Contains(out, "tracker refused the announce") {
		t.Errorf("the warning was dropped: %q", out)
	}
}

// Library output shares the journal with ours, so it has to be
// distinguishable once it is there.
func TestLibraryRecordsAreTagged(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, nil)
	slog.New(libraryHandler{h: base, min: slog.LevelInfo}).Info("hello")

	if !strings.Contains(buf.String(), "src=torrent") {
		t.Errorf("library records should carry src=torrent: %q", buf.String())
	}
}

// The package-level logger is the one that would otherwise write its own
// format straight to stderr, bypassing everything we configured.
func TestRoutingCapturesTheLibrarysPackageLogger(t *testing.T) {
	saved := anacrolixlog.Default.Handlers
	t.Cleanup(func() { anacrolixlog.Default.Handlers = saved })

	var buf bytes.Buffer
	routeLibraryLogs(slog.NewTextHandler(&buf, nil), slog.LevelInfo)

	anacrolixlog.Default.Levelf(anacrolixlog.Warning, "piece completion unavailable")

	if !strings.Contains(buf.String(), "piece completion unavailable") {
		t.Errorf("the library's global logger was not routed to us: %q", buf.String())
	}
}

func TestLibraryHandlerEnabled(t *testing.T) {
	h := libraryHandler{h: slog.NewTextHandler(&bytes.Buffer{}, nil), min: slog.LevelWarn}
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("info should be disabled below a warn threshold")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("error should be enabled above a warn threshold")
	}
}
