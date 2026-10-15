package ipc

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/indexer"
)

// fakeSearcher stands in for an indexer, with a hook so a test can make
// a search take as long as it likes.
type fakeSearcher struct {
	name    string
	results []indexer.Result
	err     error
	// block, when set, holds Search until it is closed - the stand-in
	// for an external solver driving a browser through a challenge.
	block chan struct{}
	// started is closed the moment Search is entered, so a test can
	// wait for it rather than sleeping and hoping.
	started chan struct{}
	// warnings is what a partly-failed search reports beside results.
	warnings []string

	once sync.Once
}

func (f *fakeSearcher) Sources() []string { return []string{f.name} }

func (f *fakeSearcher) Search(ctx context.Context, query string) ([]indexer.Result, []string, error) {
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	if f.block != nil {
		<-f.block
	}
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.results, f.warnings, nil
}

// startSearchDaemon is startTestDaemon with a searcher injected.
func startSearchDaemon(t *testing.T, s indexer.Searcher) *Client {
	t.Helper()

	eng, err := engine.New(engine.Config{DataDir: t.TempDir(), DisableDHT: true, DisablePEX: true})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	t.Cleanup(func() { eng.Close() })

	sock := filepath.Join(shortTempDir(t), "d.sock")
	srv, err := Serve(sock, eng, nil, s)
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

func TestSearchRoundTrip(t *testing.T) {
	want := []indexer.Result{
		{Title: "ubuntu-24.04.iso", Size: 6203355136, Seeders: 1243, Leechers: 87,
			Tracker: "ubuntu-official", Link: "magnet:?xt=urn:btih:aaa"},
		// Unknown has to survive the wire: gob omits zero values, and
		// -1 is not zero, but a reader who "tidied" it to 0 would turn
		// "not reported" into "dead torrent" with nothing to catch it.
		{Title: "quiet", Seeders: indexer.Unknown, Leechers: indexer.Unknown, Link: "magnet:?xt=urn:btih:bbb"},
	}
	c := startSearchDaemon(t, &fakeSearcher{name: "jackett", results: want})

	got, warnings, err := c.Search("ubuntu")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestSearchSources(t *testing.T) {
	c := startSearchDaemon(t, &fakeSearcher{name: "prowlarr"})

	got, err := c.SearchSources()
	if err != nil {
		t.Fatalf("SearchSources: %v", err)
	}
	if len(got) != 1 || got[0] != "prowlarr" {
		t.Errorf("sources = %v, want [prowlarr]", got)
	}
}

// Having no indexer is the ordinary state, not a failure, so the daemon
// has to say what to configure rather than merely refusing.
func TestSearchWithNoIndexerConfigured(t *testing.T) {
	c := startTestDaemon(t) // no searcher injected

	_, _, err := c.Search("ubuntu")
	if err == nil {
		t.Fatal("expected an error with no indexer configured")
	}
	for _, want := range []string{"search.jackett.url", "search.jackett.api_key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}

	sources, err := c.SearchSources()
	if err != nil {
		t.Fatalf("SearchSources: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("sources = %v, want none", sources)
	}
}

func TestSearchSurfacesTheIndexersError(t *testing.T) {
	c := startSearchDaemon(t, &fakeSearcher{
		name: "jackett",
		err:  errors.New("jackett: rejected the API key (HTTP 401) - check search.jackett.api_key"),
	})

	_, _, err := c.Search("ubuntu")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The remedy has to survive the trip: the error crosses as text, and
	// what reaches the user is all they get.
	if !strings.Contains(err.Error(), "search.jackett.api_key") {
		t.Errorf("error lost its advice: %v", err)
	}
}

// The one that justifies handling search off the connection goroutine.
//
// Requests on a connection were answered strictly in order, so a search
// that takes a minute - a browser page load behind an external solver -
// would hold up every refresh the TUI makes in the meantime and the
// interface would look frozen.
func TestASlowSearchDoesNotBlockOtherCalls(t *testing.T) {
	fake := &fakeSearcher{
		name:    "slowpoke",
		block:   make(chan struct{}),
		started: make(chan struct{}),
	}
	c := startSearchDaemon(t, fake)

	searchDone := make(chan error, 1)
	go func() {
		_, _, err := c.Search("ubuntu")
		searchDone <- err
	}()

	// Wait for the search to actually be in the daemon, so what follows
	// is queued behind it rather than racing it.
	select {
	case <-fake.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the search never reached the daemon")
	}

	listed := make(chan error, 1)
	go func() {
		_, err := c.List()
		listed <- err
	}()

	select {
	case err := <-listed:
		if err != nil {
			t.Fatalf("List while a search was in flight: %v", err)
		}
	case <-time.After(5 * time.Second):
		close(fake.block)
		t.Fatal("List blocked behind an in-flight search")
	}

	close(fake.block)
	if err := <-searchDone; err != nil {
		t.Fatalf("Search: %v", err)
	}
}

// A daemon older than the client answers "unknown method" rather than
// dropping the connection, and that has to read as something a user can
// act on.
func TestSearchAgainstADaemonThatDoesNotKnowIt(t *testing.T) {
	s := &Server{} // no searcher, and dispatch is all this needs

	resp := s.dispatch(&Request{Method: Method("SearchSomethingElse"), Query: "x"})
	if resp.Err == "" {
		t.Fatal("expected an error for an unknown method")
	}
	if !strings.Contains(resp.Err, "unknown method") {
		t.Errorf("error = %q, want it to say the method is unknown", resp.Err)
	}
}
