// Package stream serves torrent files over loopback HTTP so a media
// player can play them while they are still downloading.
//
// This exists as a second listener beside the gob IPC socket rather than
// as another IPC method because the two carry fundamentally different
// traffic. internal/ipc is one gob message per call, dispatched serially
// per connection under a 30s timeout, with no framing or flow control -
// nothing about it can carry an open-ended byte stream that a player
// seeks around in for an hour. HTTP already solves exactly that: net/http
// gives range requests, seeking, content-type sniffing and cancellation
// on client disconnect, and torrent.Reader satisfies the io.ReadSeeker
// that http.ServeContent asks for.
package stream

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/lestex/torrnado/internal/engine"
)

const (
	// readahead is how far ahead of the play head to prioritize pieces.
	//
	// This is set explicitly because the library's default readahead is
	// adaptive and starts near zero, growing only as far as the reader
	// has already read contiguously. For sequential bulk reads that ramp
	// is sensible; for video it is the worst possible shape, since the
	// moments needing the deepest buffer are exactly the ones with no
	// read history - the first seconds of playback and every seek.
	readahead = 8 << 20 // 8 MiB

	// streamPrefix is the first path segment of every stream URL, so the
	// shape is /stream/<token>/<infohash>/<file-index>.
	streamPrefix = "stream"
)

// Server serves torrent file data over HTTP on the loopback interface.
type Server struct {
	eng   *engine.Engine
	ln    net.Listener
	srv   *http.Server
	token string
}

// Serve starts the stream server on an OS-assigned loopback port.
//
// It binds 127.0.0.1 rather than any routable address, and every URL
// carries a per-session token: without one, any local process - which
// includes any web page the browser happens to load, since pages can
// reach 127.0.0.1 - could enumerate torrents and read their contents.
func Serve(eng *engine.Engine) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for stream server: %w", err)
	}

	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		ln.Close()
		return nil, fmt.Errorf("generate stream token: %w", err)
	}

	s := &Server{eng: eng, ln: ln, token: hex.EncodeToString(tok)}
	mux := http.NewServeMux()
	mux.HandleFunc("/"+streamPrefix+"/", s.handleStream)
	s.srv = &http.Server{Handler: mux}

	go s.srv.Serve(ln)
	return s, nil
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// URL returns the stream URL for one file of one torrent. It does not
// check that either exists - that happens when the URL is fetched.
func (s *Server) URL(id engine.TorrentID, fileIndex int) string {
	return fmt.Sprintf("http://%s/%s/%s/%s/%d",
		s.Addr(), streamPrefix, s.token, string(id), fileIndex)
}

// Close stops the server.
func (s *Server) Close() error { return s.srv.Close() }

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	id, fileIndex, ok := s.parsePath(r.URL.Path)
	if !ok {
		// Deliberately indistinguishable from a valid URL naming a
		// torrent that doesn't exist, so a wrong token can't be told
		// apart from a wrong id by probing.
		http.NotFound(w, r)
		return
	}

	reader, info, err := s.eng.OpenFile(id, fileIndex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer reader.Close()

	// Without a context the reader has no way to learn the player has
	// gone away: a read blocked on a piece that may never arrive would
	// park a goroutine for the lifetime of the daemon. Every abandoned
	// stream would leak one.
	reader.SetContext(r.Context())
	reader.SetReadahead(readahead)

	// A zero modtime tells ServeContent to skip If-Modified-Since
	// handling, which is what we want: the bytes at a given offset never
	// change, but the file grows, and a cached 304 would be wrong.
	http.ServeContent(w, r, path.Base(info.Path), time.Time{}, reader)
}

// parsePath splits /stream/<token>/<infohash>/<index>, verifying the
// token in constant time.
func (s *Server) parsePath(p string) (engine.TorrentID, int, bool) {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(parts) != 4 || parts[0] != streamPrefix {
		return "", 0, false
	}
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(s.token)) != 1 {
		return "", 0, false
	}
	idx, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", 0, false
	}
	return engine.TorrentID(parts[2]), idx, true
}
