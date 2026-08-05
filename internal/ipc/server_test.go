package ipc

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

// dispatch is a plain function of a request and an engine, so it can be
// tested without a socket anywhere in sight.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	eng, err := engine.New(engine.Config{DataDir: t.TempDir(), DisableDHT: true, DisablePEX: true})
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
		Source: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=Example",
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

// Every case that names a torrent is the same handful of lines copied,
// which is exactly where the wrong request field gets pasted. Each must
// route an unknown id to the same failure.
func TestDispatchRejectsUnknownIDEverywhere(t *testing.T) {
	s := newTestServer(t)

	for _, req := range []*Request{
		{Method: MethodRemove, ID: "nope"},
		{Method: MethodSetPaused, ID: "nope", Paused: true},
		{Method: MethodForceRecheck, ID: "nope"},
		{Method: MethodSetFilePriority, ID: "nope", FileIndex: 0, Priority: engine.PriorityHigh},
		{Method: MethodSetTorrentRateLimit, ID: "nope", UploadBps: 1024},
		{Method: MethodMoveStorage, ID: "nope", NewDir: t.TempDir()},
		{Method: MethodDetail, ID: "nope"},
	} {
		resp := s.dispatch(req)
		if resp.OK || resp.Err == "" {
			t.Errorf("%s with an unknown id: OK=%v Err=%q", req.Method, resp.OK, resp.Err)
		}
	}
}

// The global limits take no torrent id, so they succeed with no torrents
// at all -- and setting one must not be reported as a failure.
func TestDispatchGlobalLimits(t *testing.T) {
	s := newTestServer(t)

	for _, req := range []*Request{
		{Method: MethodSetGlobalUploadLimit, UploadBps: 500 << 10},
		{Method: MethodSetGlobalDownloadLimit, DownloadBps: 0}, // 0 means unlimited
	} {
		if resp := s.dispatch(req); !resp.OK {
			t.Errorf("%s: %q", req.Method, resp.Err)
		}
	}
}

// Only one daemon may own a socket. Two running against the same data
// directory corrupt each other's view of which pieces are complete.
func TestSecondDaemonIsRefused(t *testing.T) {
	eng, err := engine.New(engine.Config{DataDir: t.TempDir(), DisableDHT: true, DisablePEX: true})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	t.Cleanup(func() { eng.Close() })

	sock := filepath.Join(shortTempDir(t), "d.sock")

	first, err := Serve(sock, eng, nil)
	if err != nil {
		t.Fatalf("first Serve: %v", err)
	}
	defer first.Close()

	if second, err := Serve(sock, eng, nil); err == nil {
		second.Close()
		t.Fatal("a second daemon was allowed to take the same socket")
	}
}

// And once the first releases it, the socket can be claimed again --
// otherwise a restart would need the lock file deleted by hand.
func TestSocketCanBeReclaimedAfterClose(t *testing.T) {
	eng, err := engine.New(engine.Config{DataDir: t.TempDir(), DisableDHT: true, DisablePEX: true})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	t.Cleanup(func() { eng.Close() })

	sock := filepath.Join(shortTempDir(t), "d.sock")

	first, err := Serve(sock, eng, nil)
	if err != nil {
		t.Fatalf("first Serve: %v", err)
	}
	first.Close()

	second, err := Serve(sock, eng, nil)
	if err != nil {
		t.Fatalf("could not reclaim the socket after a clean shutdown: %v", err)
	}
	second.Close()
}
