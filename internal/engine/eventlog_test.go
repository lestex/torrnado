package engine

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// logCapture is a writer the engine can log into from any goroutine -
// the completion message is emitted from the tick loop, not the caller's.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *logCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func newLoggingEngine(t *testing.T) (*Engine, *logCapture) {
	t.Helper()
	capture := &logCapture{}
	cfg := testConfig(t)
	cfg.Logger = slog.New(slog.NewTextHandler(capture, nil))
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e, capture
}

func TestAddAndRemoveAreLogged(t *testing.T) {
	e, logs := newLoggingEngine(t)

	id, err := e.AddMagnet(testMagnet, AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}
	if out := logs.String(); !strings.Contains(out, "torrent added") || !strings.Contains(out, string(id)) {
		t.Errorf("adding a torrent was not logged with its id: %q", out)
	}

	if err := e.RemoveTorrent(id, false); err != nil {
		t.Fatalf("RemoveTorrent: %v", err)
	}
	if out := logs.String(); !strings.Contains(out, "torrent removed") {
		t.Errorf("removing a torrent was not logged: %q", out)
	}
}

// A magnet whose metadata has never arrived knows neither its size nor
// whether it is done. Asking the library anyway is what crashes the
// daemon, so the completion check has to skip it rather than treat an
// empty torrent as a finished one.
func TestCompletionSkipsATorrentWithoutMetadata(t *testing.T) {
	e, logs := newLoggingEngine(t)

	if _, err := e.AddMagnet(testMagnet, AddOpts{}); err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}
	e.tick()
	e.tick()

	if strings.Contains(logs.String(), "torrent complete") {
		t.Errorf("a torrent with no metadata was reported complete: %q", logs.String())
	}
}

// The engine logs completion from a loop that runs every second, so
// without the flag the message would repeat forever.
func TestCompletionIsReportedOnce(t *testing.T) {
	e, logs := newLoggingEngine(t)

	id, err := e.AddMagnet(testMagnet, AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}

	// Stand in for a finished download: the flag is what suppresses the
	// repeat, and it is set the first time a torrent is seen complete.
	e.mu.Lock()
	e.torrents[id].completeLogged = true
	e.mu.Unlock()

	e.tick()
	if n := strings.Count(logs.String(), "torrent complete"); n != 0 {
		t.Errorf("an already-reported torrent was logged again %d time(s)", n)
	}
}

// Nil is the normal case for every caller that is not the daemon - the
// tests above, and any embedder that does not want output - so it has to
// discard rather than panic on the first log call.
func TestANilLoggerDiscards(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.AddMagnet(testMagnet, AddOpts{}); err != nil {
		t.Fatalf("AddMagnet with no logger: %v", err)
	}
	e.tick()
}
