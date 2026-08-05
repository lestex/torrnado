package ipc

import (
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

// dispatch is a plain function of a request and an engine, so it can be
// tested without a socket anywhere in sight.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	eng, err := engine.New(engine.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	t.Cleanup(func() { eng.Close() })
	return &Server{eng: eng}
}

func TestDispatchPing(t *testing.T) {
	resp := newTestServer(t).dispatch(&Request{Seq: 3, Method: MethodPing})

	if !resp.OK || resp.Err != "" {
		t.Errorf("ping failed: %+v", resp)
	}
	// The sequence number must come back untouched, or the client cannot
	// match this reply to the call that is waiting for it.
	if resp.Seq != 3 {
		t.Errorf("Seq = %d, want 3", resp.Seq)
	}
}

func TestDispatchUnknownMethod(t *testing.T) {
	resp := newTestServer(t).dispatch(&Request{Method: "Nonsense"})

	if resp.OK {
		t.Error("an unknown method should not report OK")
	}
	if !strings.Contains(resp.Err, "unknown method") {
		t.Errorf("Err = %q, want it to mention the unknown method", resp.Err)
	}
}

func TestDispatchAddThenList(t *testing.T) {
	s := newTestServer(t)

	add := s.dispatch(&Request{
		Method: MethodAddMagnet,
		Source: "magnet:?xt=urn:btih:abc&dn=Example",
	})
	if !add.OK {
		t.Fatalf("add failed: %q", add.Err)
	}

	list := s.dispatch(&Request{Method: MethodList})
	if len(list.Snapshot) != 1 {
		t.Fatalf("got %d torrents, want 1", len(list.Snapshot))
	}
	if list.Snapshot[0].ID != engine.TorrentID(add.ID) {
		t.Errorf("listed id %q, added %q", list.Snapshot[0].ID, add.ID)
	}
}

// An engine error has to reach the caller as text in Err, with OK false --
// this is the only channel a failure has.
func TestDispatchReportsEngineErrors(t *testing.T) {
	s := newTestServer(t)

	resp := s.dispatch(&Request{Method: MethodRemove, ID: "nope"})

	if resp.OK {
		t.Error("removing an unknown torrent should not report OK")
	}
	if !strings.Contains(resp.Err, engine.ErrNotFound.Error()) {
		t.Errorf("Err = %q, want it to carry the engine's message", resp.Err)
	}
}
