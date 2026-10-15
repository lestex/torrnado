package ipc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/indexer"
)

// tokenSearcher is a site plugin: its results carry a token rather than
// a magnet, and resolving one is a page load it can be made to stall.
type tokenSearcher struct {
	magnet string
	err    error

	mu      sync.Mutex
	asked   []string
	block   chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (t *tokenSearcher) Sources() []string { return []string{"rutracker"} }

func (t *tokenSearcher) Search(context.Context, string) ([]indexer.Result, []string, error) {
	return []indexer.Result{{Title: "x", Link: indexer.Token("rutracker", "6916386")}}, nil, nil
}

func (t *tokenSearcher) Resolve(_ context.Context, id string) (string, error) {
	t.mu.Lock()
	t.asked = append(t.asked, id)
	t.mu.Unlock()
	if t.entered != nil {
		t.once.Do(func() { close(t.entered) })
	}
	if t.block != nil {
		<-t.block
	}
	return t.magnet, t.err
}

const realMagnet = "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=Resolved"

// The whole point of the token: the client sends it through untouched
// and the daemon turns it into a torrent, because only the daemon has
// the session needed to read the page it names.
func TestAddBatchResolvesASourceToken(t *testing.T) {
	src := &tokenSearcher{magnet: realMagnet}
	c := startSearchDaemon(t, src)

	ids, errs, err := c.AddBatch([]string{indexer.Token("rutracker", "6916386")}, engine.AddOpts{})
	if err != nil {
		t.Fatalf("AddBatch: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if len(ids) != 1 {
		t.Fatalf("added %d torrents, want 1", len(ids))
	}
	if string(ids[0]) != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("added %q, want the infohash from the resolved magnet", ids[0])
	}
	if len(src.asked) != 1 || src.asked[0] != "6916386" {
		t.Errorf("the source was asked for %v, want the topic id", src.asked)
	}

	// And it is really in the engine, not merely reported.
	list, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Resolved" {
		t.Fatalf("List = %+v, want the resolved torrent", list)
	}
}

// A magnet must not be routed through the searcher: adding one has to
// keep working on a daemon with no indexer at all.
func TestAddBatchLeavesOrdinarySourcesAlone(t *testing.T) {
	src := &tokenSearcher{magnet: realMagnet}
	c := startSearchDaemon(t, src)

	plain := "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb&dn=Plain"
	ids, _, err := c.AddBatch([]string{plain}, engine.AddOpts{})
	if err != nil {
		t.Fatalf("AddBatch: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("added %d, want 1", len(ids))
	}
	if len(src.asked) != 0 {
		t.Errorf("the searcher was consulted about a magnet: %v", src.asked)
	}
}

// On a daemon with nothing configured, a token has to say which source
// it needed - that is the only thing that tells the user what to fix.
func TestAddBatchReportsATokenWithNoSearcher(t *testing.T) {
	c := startTestDaemon(t)

	ids, errs, err := c.AddBatch([]string{indexer.Token("rutracker", "6916386")}, engine.AddOpts{})
	if err != nil {
		t.Fatalf("AddBatch: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("added %d torrents from an unresolvable token", len(ids))
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want one message", errs)
	}
	if !strings.Contains(errs[0], "rutracker") {
		t.Errorf("message does not name the source: %q", errs[0])
	}
}

// The source's own failure has to survive the trip, or an expired cookie
// arrives as something generic the user cannot act on.
func TestAddBatchCarriesTheResolveFailureBack(t *testing.T) {
	src := &tokenSearcher{err: errors.New("the session cookie has expired - paste a fresh one")}
	c := startSearchDaemon(t, src)

	_, errs, err := c.AddBatch([]string{indexer.Token("rutracker", "1")}, engine.AddOpts{})
	if err != nil {
		t.Fatalf("AddBatch: %v", err)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "session cookie has expired") {
		t.Fatalf("errs = %v, want the source's own message", errs)
	}
}

// Resolving reads a page through a browser, so an add can take as long
// as a search. Answered in turn it would hold up every other call on the
// connection, and the interface would look frozen while it ran.
func TestASlowResolveDoesNotBlockOtherCalls(t *testing.T) {
	src := &tokenSearcher{
		magnet:  realMagnet,
		block:   make(chan struct{}),
		entered: make(chan struct{}),
	}
	c := startSearchDaemon(t, src)
	// Released however the test ends. Without this, a failure leaves the
	// resolve stalled inside the server, the daemon's own shutdown waits
	// on it, and the test hangs instead of reporting - which is how this
	// test behaves when it is the one finding the bug.
	var release sync.Once
	unblock := func() { release.Do(func() { close(src.block) }) }
	t.Cleanup(unblock)

	added := make(chan error, 1)
	go func() {
		_, _, err := c.AddBatch([]string{indexer.Token("rutracker", "1")}, engine.AddOpts{})
		added <- err
	}()

	select {
	case <-src.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the resolve never started")
	}

	done := make(chan error, 1)
	go func() { done <- c.Ping() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Ping while a resolve was in flight: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ping blocked behind an in-flight resolve")
	}

	unblock()
	if err := <-added; err != nil {
		t.Fatalf("AddBatch: %v", err)
	}
}
