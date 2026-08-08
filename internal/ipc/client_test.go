package ipc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lestex/torrnado/internal/engine"
)

// startTestDaemon runs a real engine behind a real server on a socket in
// a temporary directory, and returns a client connected to it. The whole
// round trip is exercised: encode, socket, dispatch, reply, decode.
func startTestDaemon(t *testing.T) *Client {
	t.Helper()

	eng, err := engine.New(engine.Config{DataDir: t.TempDir(), DisableDHT: true, DisablePEX: true})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	t.Cleanup(func() { eng.Close() })

	sock := filepath.Join(shortTempDir(t), "d.sock")
	srv, err := Serve(sock, eng, nil)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// shortTempDir returns a temporary directory with a deliberately short
// path.
//
// t.TempDir() would be the obvious choice, but it builds the path out of
// the test's own name, and a Unix socket address is limited to about 100
// bytes on most systems. A long test name silently pushes the socket over
// that limit and bind fails with "invalid argument" - which looks like a
// flaky test rather than the length problem it is.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tn")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestClientPing(t *testing.T) {
	if err := startTestDaemon(t).Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestClientAddAndList(t *testing.T) {
	c := startTestDaemon(t)

	id, err := c.AddMagnet("magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=Example", engine.AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}

	list, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("List returned %+v, want the torrent just added", list)
	}
	if list[0].Name != "Example" {
		t.Errorf("Name = %q, want %q", list[0].Name, "Example")
	}
}

// Several calls in flight at once must each get their own reply back.
// This is the whole point of the sequence numbers.
func TestClientMatchesConcurrentReplies(t *testing.T) {
	c := startTestDaemon(t)

	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { errs <- c.Ping() }()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Ping: %v", err)
		}
	}
}

// An engine failure has to arrive as a Go error, not a silent empty
// response.
func TestClientSurfacesDaemonErrors(t *testing.T) {
	c := startTestDaemon(t)

	err := c.Remove("nope", false)
	if err == nil {
		t.Fatal("removing an unknown torrent should fail")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want it to mention the torrent was not found", err)
	}
}

func TestClientReceivesPushedEvents(t *testing.T) {
	c := startTestDaemon(t)

	if _, err := c.AddMagnet("magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=Example", engine.AddOpts{}); err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}

	// Adding broadcasts immediately, so an event should arrive without
	// waiting for the engine's next tick.
	select {
	case ev, ok := <-c.Events():
		if !ok {
			t.Fatal("event channel closed")
		}
		if len(ev.Torrents) != 1 {
			t.Errorf("event carried %d torrents, want 1", len(ev.Torrents))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event pushed by the daemon")
	}
}

// A caller blocked on a reply must be woken when the connection dies,
// rather than waiting out the full call timeout.
func TestClientCallFailsWhenClosed(t *testing.T) {
	c := startTestDaemon(t)
	c.Close()

	done := make(chan error, 1)
	go func() { done <- c.Ping() }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Ping on a closed client should fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ping hung on a closed client")
	}
}
