package ipc

import (
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/lestex/torrnado/internal/engine"
)

// Server accepts client connections on a Unix domain socket and
// dispatches their Requests against an *engine.Engine.
type Server struct {
	eng        *engine.Engine
	ln         net.Listener
	socketPath string

	wg sync.WaitGroup
}

// Serve starts listening on socketPath.
func Serve(socketPath string, eng *engine.Engine) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}
	// A Unix socket is a file, and binding fails if one is already there.
	// A daemon that crashed leaves its socket behind, so clear it first.
	os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socketPath, err)
	}

	s := &Server{eng: eng, ln: ln, socketPath: socketPath}
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

	case MethodRemove:
		if err := s.eng.RemoveTorrent(engine.TorrentID(req.ID), req.DeleteData); err != nil {
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

	default:
		resp.Err = fmt.Sprintf("unknown method %q", req.Method)
	}
	return resp
}
