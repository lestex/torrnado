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

	for {
		var msg message
		if err := dec.Decode(&msg); err != nil {
			return // client disconnected
		}
		if msg.Kind != kindCall || msg.Req == nil {
			continue
		}
		resp := s.dispatch(msg.Req)
		if enc.Encode(message{Kind: kindReply, Resp: resp}) != nil {
			return
		}
	}
}

// dispatch runs one request against the engine. Filled in next.
func (s *Server) dispatch(req *Request) *Response {
	return &Response{Seq: req.Seq, Err: fmt.Sprintf("unknown method %q", req.Method)}
}
