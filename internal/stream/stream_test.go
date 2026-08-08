package stream

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

// startTestServer runs a real engine behind a real stream server and
// returns it plus a torrent id that exists in it.
func startTestServer(t *testing.T) (*Server, engine.TorrentID) {
	t.Helper()

	eng, err := engine.New(engine.Config{
		DataDir: t.TempDir(), DisableDHT: true, DisablePEX: true,
	})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	t.Cleanup(func() { eng.Close() })

	srv, err := Serve(eng)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	id, err := eng.AddMagnet(
		"magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=Example",
		engine.AddOpts{})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}
	return srv, id
}

// The server binds loopback only. Anything else would expose the user's
// torrents to the network.
func TestServerBindsLoopbackOnly(t *testing.T) {
	srv, _ := startTestServer(t)

	if !strings.HasPrefix(srv.Addr(), "127.0.0.1:") {
		t.Errorf("listening on %s, want a loopback address", srv.Addr())
	}
}

// Without a token any local process - including a web page, which can
// reach 127.0.0.1 - could enumerate and read torrents.
func TestWrongTokenIsRejected(t *testing.T) {
	srv, id := startTestServer(t)

	good := srv.URL(id, 0)
	bad := strings.Replace(good, srv.token, strings.Repeat("f", len(srv.token)), 1)

	resp, err := http.Get(bad)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("a wrong token was served content")
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "token") {
		t.Error("the response hints at why it failed; it should not")
	}
}

func TestMissingTokenIsRejected(t *testing.T) {
	srv, id := startTestServer(t)

	resp, err := http.Get(fmt.Sprintf("http://%s/stream/%s/0", srv.Addr(), id))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("a URL with no token was served content")
	}
}

// A URL naming a torrent that is not there fails, rather than hanging or
// serving something else.
func TestUnknownTorrentIsNotFound(t *testing.T) {
	srv, _ := startTestServer(t)

	resp, err := http.Get(srv.URL("nosuchtorrent", 0))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Errorf("status %d for an unknown torrent", resp.StatusCode)
	}
}

// Two calls for the same file give the same URL, so a player handed one
// can be handed it again.
func TestURLIsStable(t *testing.T) {
	srv, id := startTestServer(t)

	if a, b := srv.URL(id, 3), srv.URL(id, 3); a != b {
		t.Errorf("URL is not stable: %q then %q", a, b)
	}
	if same := srv.URL(id, 3) == srv.URL(id, 4); same {
		t.Error("different files share a URL")
	}
}
