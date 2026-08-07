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

	wg sync.WaitGroup
}

// Serve starts listening on socketPath. If another daemon owns that
// socket, it returns an error rather than displacing it.
//
// Ownership is decided by an exclusive lock file, not by dialing the
// socket -- see acquireDaemonLock for why probing is not sound.
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

	s := &Server{eng: eng, ln: ln, socketPath: socketPath, previewURL: previewURL, lock: lock}
	s.wg.Add(1)
	go s.acceptLoop()
	return s, nil
}

// Close stops accepting new connections, waits for in-flight ones to
// finish, and removes the socket file.
func (s *Server) Close() error {
	err := s.ln.Close()
	s.wg.Wait()
	os.Remove(s.socketPath)
	releaseDaemonLock(s.lock)
	return err
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
	}
}

// dispatch runs one request against the engine and builds its reply.
//
// This switch is the only place the two halves of the program meet: on
// one side a request that arrived over a socket, on the other the engine
// API. Every case does the same three things -- call the engine, put any
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

	default:
		resp.Err = fmt.Sprintf("unknown method %q", req.Method)
	}
	return resp
}
