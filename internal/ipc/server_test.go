package ipc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// An engine error has to reach the caller as text in Err, with OK false -
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
		{Method: MethodPurgeData, ID: "nope"},
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
// at all - and setting one must not be reported as a failure.
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

// And once the first releases it, the socket can be claimed again -
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

// Shutting down must not wait on a client that is still attached. The TUI
// holds its connection open for as long as it runs, so a daemon that
// waited for the read loop to end would hang on every SIGTERM until the
// person at the keyboard happened to quit.
func TestCloseDisconnectsAnAttachedClient(t *testing.T) {
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

	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	// Ping first, so the connection is certainly accepted and being served
	// rather than still sitting in the listener's backlog.
	if err := c.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	done := make(chan struct{})
	go func() {
		srv.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked with a client still attached")
	}
}

// DaemonInfo is what `torrnado status` and `torrnado stop` are built on,
// so it has to be right about all three states.
func TestDaemonInfoReportsOwnership(t *testing.T) {
	eng, err := engine.New(engine.Config{DataDir: t.TempDir(), DisableDHT: true, DisablePEX: true})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	t.Cleanup(func() { eng.Close() })

	sock := filepath.Join(shortTempDir(t), "d.sock")

	// Nothing has ever run here: no lock file, and asking must not make
	// one - a question should not leave state behind.
	info, err := DaemonInfo(sock)
	if err != nil {
		t.Fatalf("DaemonInfo before start: %v", err)
	}
	if info.Running {
		t.Error("reported a daemon before one was started")
	}
	if _, err := os.Stat(info.LockPath); !os.IsNotExist(err) {
		t.Error("asking whether a daemon runs created the lock file")
	}

	srv, err := Serve(sock, eng, nil)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}

	info, err = DaemonInfo(sock)
	if err != nil {
		t.Fatalf("DaemonInfo while running: %v", err)
	}
	if !info.Running {
		t.Error("did not see the running daemon's lock")
	}
	if info.PID != os.Getpid() {
		t.Errorf("pid = %d, want this process (%d)", info.PID, os.Getpid())
	}

	// And the lock going away is what tells `torrnado stop` the daemon
	// has finished, so it must not linger past Close.
	srv.Close()
	info, err = DaemonInfo(sock)
	if err != nil {
		t.Fatalf("DaemonInfo after close: %v", err)
	}
	if info.Running {
		t.Error("still reported a daemon after the server closed")
	}
}

// DaemonInfo takes the same lock it is asking about, so a daemon starting
// while something asks must not be refused. Without the retry in
// acquireDaemonLock, asking often enough stops a daemon from starting at
// all.
func TestDaemonInfoDoesNotBlockAStartingDaemon(t *testing.T) {
	eng, err := engine.New(engine.Config{DataDir: t.TempDir(), DisableDHT: true, DisablePEX: true})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	t.Cleanup(func() { eng.Close() })

	sock := filepath.Join(shortTempDir(t), "d.sock")

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				DaemonInfo(sock)
			}
		}
	}()
	defer func() { close(stop); <-done }()

	srv, err := Serve(sock, eng, nil)
	if err != nil {
		t.Fatalf("Serve while status was being asked: %v", err)
	}
	srv.Close()
}

// `torrnado stop` is one Shutdown call plus a wait on the daemon lock, so
// the call has to come back OK. The daemon answers a shutdown by closing
// every client connection, which is a race with writing this very reply:
// the server must not start the shutdown until the reply is on the wire.
// The goroutine below is the daemon - it does exactly what daemon.go does
// when ShutdownRequested fires.
func TestShutdownRepliesBeforeTheDaemonTearsDown(t *testing.T) {
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

	closed := make(chan struct{})
	go func() {
		<-srv.ShutdownRequested()
		srv.Close()
		close(closed)
	}()

	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if err := c.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon was never told to shut down")
	}
}

// Nothing asks for a shutdown, so nothing should get one - a stray close
// of that channel would stop the daemon on the next unrelated call.
func TestShutdownIsNotRequestedByOtherCalls(t *testing.T) {
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
	defer srv.Close()

	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	if err := c.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	select {
	case <-srv.ShutdownRequested():
		t.Fatal("a ping asked the daemon to shut down")
	default:
	}
}
