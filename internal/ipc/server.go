package ipc

import (
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lestex/torrnado/internal/engine"
)

// Server accepts client connections on a Unix domain socket and
// dispatches their Requests against an *engine.Engine.
type Server struct {
	eng        *engine.Engine
	ln         net.Listener
	socketPath string
	// previewURL builds a stream URL for a file. Injected as a function
	// rather than taking the stream server itself, so this package still
	// depends on nothing but engine. Nil disables the method.
	previewURL func(engine.TorrentID, int) string
	// lock is held open for the daemon's lifetime; closing it is what
	// releases the exclusive flock.
	lock *os.File

	// conns is every client connection currently being served, and closing
	// guards against one being registered after shutdown has walked the
	// map. Closing the listener does not touch connections already
	// accepted, so without this Close would wait on a read that only ends
	// when the client feels like disconnecting - see Close.
	connMu  sync.Mutex
	conns   map[net.Conn]struct{}
	closing bool

	// shutdown is closed when a client calls MethodShutdown. The daemon
	// waits on it alongside the signals it already handles, so `torrnado
	// stop` needs no way to signal a process - which is what makes it
	// work on a platform that has no signals to send.
	shutdown     chan struct{}
	shutdownOnce sync.Once

	wg sync.WaitGroup
}

// Serve starts listening on socketPath. If another daemon owns that
// socket, it returns an error rather than displacing it.
//
// Ownership is decided by an exclusive lock file, not by dialing the
// socket - see acquireDaemonLock for why probing is not sound.
// previewURL may be nil, which disables MethodPreviewURL.
func Serve(socketPath string, eng *engine.Engine,
	previewURL func(engine.TorrentID, int) string,
) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	lock, err := acquireDaemonLock(socketPath)
	if err != nil {
		return nil, err
	}

	// A Unix socket is a file, and binding fails if one is already there.
	// Only safe to remove now: holding the lock means no live daemon can
	// be listening on it.
	os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		releaseDaemonLock(lock)
		return nil, fmt.Errorf("listen on %s: %w", socketPath, err)
	}

	s := &Server{
		eng:        eng,
		ln:         ln,
		socketPath: socketPath,
		previewURL: previewURL,
		lock:       lock,
		conns:      map[net.Conn]struct{}{},
		shutdown:   make(chan struct{}),
	}
	s.wg.Add(1)
	go s.acceptLoop()
	return s, nil
}

// Close stops accepting new connections, disconnects the clients still
// attached, and removes the socket file.
//
// Hanging up on them is not a courtesy that can be skipped. Closing the
// listener stops new connections and nothing else: a connection already
// accepted is parked in a blocking read that ends when the client
// disconnects, which for an attached TUI is whenever the person using it
// decides to quit. Waiting on that meant a SIGTERM logged "shutting down"
// and then sat there - no session saved, no client closed - until a
// service manager gave up and killed it.
func (s *Server) Close() error {
	err := s.ln.Close()
	s.closeConns()
	s.wg.Wait()
	os.Remove(s.socketPath)
	releaseDaemonLock(s.lock)
	return err
}

// ShutdownRequested is closed when a client has asked the daemon to stop.
// The daemon selects on it the way it selects on SIGTERM; this package
// does not shut anything down itself, because the engine, the stream
// server and the log are the daemon's to tear down in order.
func (s *Server) ShutdownRequested() <-chan struct{} { return s.shutdown }

func (s *Server) requestShutdown() {
	s.shutdownOnce.Do(func() { close(s.shutdown) })
}

// closeConns hangs up on every client being served and refuses any that
// arrive afterwards.
func (s *Server) closeConns() {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.closing = true
	for conn := range s.conns {
		conn.Close()
	}
}

// track registers a connection so closeConns can reach it, reporting
// false when shutdown has already been through the map - in which case
// the caller must hang up itself, or Close would wait forever on a
// connection accepted a moment too late.
func (s *Server) track(conn net.Conn) bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.closing {
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *Server) untrack(conn net.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	delete(s.conns, conn)
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed
		}
		// Each client gets its own goroutine, so a slow one can't hold
		// up anybody else's connection.
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

// handleConn reads requests from one client until it disconnects.
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	if !s.track(conn) {
		return // shutting down, and this one arrived too late to be tracked
	}
	defer s.untrack(conn)

	dec := gob.NewDecoder(conn)
	enc := gob.NewEncoder(conn)

	// Two goroutines now write to this connection: this one replying to
	// calls, and the event pump below. A gob stream is a sequence of
	// framed values and cannot be written concurrently, so every write
	// goes through one place holding a mutex.
	var writeMu sync.Mutex
	send := func(m message) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return enc.Encode(m)
	}

	// This is the reason for a custom protocol rather than plain
	// request/reply: the daemon pushes state at the client unprompted, on
	// the same connection it takes commands on.
	events, unsubscribe := s.eng.Subscribe()
	defer unsubscribe()

	go func() {
		for ev := range events {
			if send(message{Kind: kindEvent, Event: &ev}) != nil {
				return // connection gone; the read loop will clean up
			}
		}
	}()

	for {
		var msg message
		if err := dec.Decode(&msg); err != nil {
			return // client disconnected
		}
		if msg.Kind != kindCall || msg.Req == nil {
			continue
		}
		resp := s.dispatch(msg.Req)
		if send(message{Kind: kindReply, Resp: resp}) != nil {
			return
		}
		// Asked after the reply has gone out, not inside dispatch: the
		// daemon answers a shutdown by closing every connection, so
		// triggering it first would race the write and leave the caller
		// looking at a dropped connection instead of an OK.
		if msg.Req.Method == MethodShutdown && resp.OK {
			s.requestShutdown()
			return
		}
	}
}

// dispatch runs one request against the engine and builds its reply.
//
// This switch is the only place the two halves of the program meet: on
// one side a request that arrived over a socket, on the other the engine
// API. Every case does the same three things - call the engine, put any
// error into the response as a string, mark it OK.
//
// Errors cross the wire as text rather than as Go error values, because
// an error is an interface and gob cannot encode one.
func (s *Server) dispatch(req *Request) *Response {
	resp := &Response{Seq: req.Seq}

	switch req.Method {
	case MethodPing:
		resp.OK = true

	case MethodAddMagnet:
		id, err := s.eng.AddMagnet(req.Source, req.Opts)
		if err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true
		resp.ID = string(id)

	case MethodAddTorrentFile:
		id, err := s.eng.AddTorrentFile(req.Source, req.Opts)
		if err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true
		resp.ID = string(id)

	case MethodAddBatch:
		// One round trip for the whole batch: adding fifty torrents
		// should not mean fifty calls. A source that fails is recorded
		// and the rest carry on.
		for _, src := range req.Sources {
			var id engine.TorrentID
			var err error
			if strings.HasPrefix(src, "magnet:") {
				id, err = s.eng.AddMagnet(src, req.Opts)
			} else {
				id, err = s.eng.AddTorrentFile(src, req.Opts)
			}
			if err != nil {
				resp.Errs = append(resp.Errs, fmt.Sprintf("%s: %v", src, err))
				continue
			}
			resp.IDs = append(resp.IDs, string(id))
		}
		resp.OK = true

	case MethodRemove:
		if err := s.eng.RemoveTorrent(engine.TorrentID(req.ID), req.DeleteData); err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true

	case MethodPurgeData:
		if err := s.eng.PurgeData(engine.TorrentID(req.ID)); err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true

	case MethodSetPaused:
		if err := s.eng.SetPaused(engine.TorrentID(req.ID), req.Paused); err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true

	case MethodForceRecheck:
		if err := s.eng.ForceRecheck(engine.TorrentID(req.ID)); err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true

	case MethodSetFilePriority:
		if err := s.eng.SetFilePriority(engine.TorrentID(req.ID), req.FileIndex, req.Priority); err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true

	case MethodSetGlobalUploadLimit:
		s.eng.SetGlobalUploadLimit(req.UploadBps)
		resp.OK = true

	case MethodSetGlobalDownloadLimit:
		s.eng.SetGlobalDownloadLimit(req.DownloadBps)
		resp.OK = true

	case MethodSetTorrentRateLimit:
		if err := s.eng.SetTorrentRateLimit(engine.TorrentID(req.ID), req.UploadBps, req.DownloadBps); err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true

	case MethodSetSeedLimit:
		if err := s.eng.SetSeedLimit(engine.TorrentID(req.ID), req.SeedRatio, req.SeedTime); err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true

	case MethodMoveStorage:
		if err := s.eng.MoveStorage(engine.TorrentID(req.ID), req.NewDir); err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true

	case MethodList:
		resp.OK = true
		resp.Snapshot = s.eng.ListTorrents()

	case MethodDetail:
		d, err := s.eng.TorrentDetail(engine.TorrentID(req.ID))
		if err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true
		resp.Detail = &d

	case MethodPreviewURL:
		if s.previewURL == nil {
			resp.Err = "streaming is not enabled on this daemon"
			return resp
		}
		id := engine.TorrentID(req.ID)
		// Resume and raise priority before handing out the URL, so the
		// player does not open onto a stream that errors immediately.
		if err := s.eng.PrepareStream(id, req.FileIndex); err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true
		resp.URL = s.previewURL(id, req.FileIndex)

	case MethodSetLabel:
		if err := s.eng.SetLabel(engine.TorrentID(req.ID), req.Label); err != nil {
			resp.Err = err.Error()
			return resp
		}
		resp.OK = true

	case MethodShutdown:
		// Nothing to do here but agree. handleConn starts the shutdown
		// once this reply is on the wire; see the call site.
		resp.OK = true

	default:
		resp.Err = fmt.Sprintf("unknown method %q", req.Method)
	}
	return resp
}
