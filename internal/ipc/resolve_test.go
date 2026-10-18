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

func (t *tokenSearcher) timesAsked() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.asked)
}

const realMagnet = "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=Resolved"

// The daemon resolves because only the daemon has the session needed to
// read the page the token names.
func TestResolveLinkTurnsATokenIntoAMagnet(t *testing.T) {
	src := &tokenSearcher{magnet: realMagnet}
	c := startSearchDaemon(t, src)

	got, err := c.ResolveLink(indexer.Token("rutracker", "6916386"))
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if got != realMagnet {
		t.Errorf("got %q, want the resolved magnet", got)
	}
	if n := src.timesAsked(); n != 1 || src.asked[0] != "6916386" {
		t.Errorf("the source was asked %d times for %v, want once for the topic id", n, src.asked)
	}
}

// Resolving is a separate step from adding, so what comes back has to go
// through the ordinary add path and really arrive.
func TestAResolvedLinkCanThenBeAdded(t *testing.T) {
	src := &tokenSearcher{magnet: realMagnet}
	c := startSearchDaemon(t, src)

	link, err := c.ResolveLink(indexer.Token("rutracker", "1"))
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	ids, errs, err := c.AddBatch([]string{link}, engine.AddOpts{})
	if err != nil {
		t.Fatalf("AddBatch: %v", err)
	}
	if len(errs) != 0 || len(ids) != 1 {
		t.Fatalf("ids=%v errs=%v, want one added", ids, errs)
	}
	list, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Resolved" {
		t.Fatalf("List = %+v, want the resolved torrent", list)
	}
}

// The point of keeping this out of AddBatch: adding is the path every
// torrent takes, and it must not grow a step that only one source needs.
// A token sent straight to it is treated as a file - wrongly, but by the
// code that was always there - and the searcher is never consulted.
func TestAddBatchKnowsNothingAboutTokens(t *testing.T) {
	src := &tokenSearcher{magnet: realMagnet}
	c := startSearchDaemon(t, src)

	_, errs, err := c.AddBatch([]string{indexer.Token("rutracker", "6916386")}, engine.AddOpts{})
	if err != nil {
		t.Fatalf("AddBatch: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want one failure", errs)
	}
	if n := src.timesAsked(); n != 0 {
		t.Errorf("AddBatch consulted the searcher %d times; resolving belongs in ResolveLink", n)
	}
}

// A magnet is not a token, and asking about one is a mistake worth
// reporting rather than answering.
func TestResolveLinkRejectsWhatIsAlreadyAddable(t *testing.T) {
	src := &tokenSearcher{magnet: realMagnet}
	c := startSearchDaemon(t, src)

	if _, err := c.ResolveLink(realMagnet); err == nil {
		t.Error("resolving a magnet should be an error, not a no-op")
	}
	if n := src.timesAsked(); n != 0 {
		t.Errorf("the source was consulted about a magnet (%d times)", n)
	}
}

// On a daemon with nothing configured, the message has to say so: the
// fix is to configure a source, and nothing else would suggest it.
func TestResolveLinkWithNoSearcher(t *testing.T) {
	c := startTestDaemon(t)

	_, err := c.ResolveLink(indexer.Token("rutracker", "6916386"))
	if err == nil {
		t.Fatal("expected an error with no searcher configured")
	}
	if !strings.Contains(err.Error(), "search") {
		t.Errorf("message does not point at the search config: %v", err)
	}
}

// The source's own failure has to survive the trip, or an expired cookie
// arrives as something generic the user cannot act on.
func TestResolveLinkCarriesTheSourcesFailureBack(t *testing.T) {
	src := &tokenSearcher{err: errors.New("the session cookie has expired - paste a fresh one")}
	c := startSearchDaemon(t, src)

	_, err := c.ResolveLink(indexer.Token("rutracker", "1"))
	if err == nil {
		t.Fatal("expected the source's error")
	}
	if !strings.Contains(err.Error(), "session cookie has expired") {
		t.Errorf("error lost the source's own message: %v", err)
	}
}

// Resolving reads a page through a browser, so it takes as long as a
// search. Answered in turn it would hold up every other call on the
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

	done := make(chan error, 1)
	go func() {
		_, err := c.ResolveLink(indexer.Token("rutracker", "1"))
		done <- err
	}()

	select {
	case <-src.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the resolve never started")
	}

	pinged := make(chan error, 1)
	go func() { pinged <- c.Ping() }()
	select {
	case err := <-pinged:
		if err != nil {
			t.Fatalf("Ping while a resolve was in flight: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ping blocked behind an in-flight resolve")
	}

	unblock()
	if err := <-done; err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
}

// ResolveLinks is what both clients call before expanding their
// arguments, so it has to leave every ordinary source alone - and
// touching them would mean a round trip per argument for nothing.
func TestResolveLinksLeavesOrdinarySourcesAlone(t *testing.T) {
	src := &tokenSearcher{magnet: realMagnet}
	c := startSearchDaemon(t, src)

	args := []string{
		"magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"/tmp/some.torrent",
		"~/torrents/*.torrent",
		"https://host/x.torrent",
		"magnets.txt",
	}
	got, err := c.ResolveLinks(args)
	if err != nil {
		t.Fatalf("ResolveLinks: %v", err)
	}
	if len(got) != len(args) {
		t.Fatalf("got %d args back, want %d", len(got), len(args))
	}
	for i := range args {
		if got[i] != args[i] {
			t.Errorf("arg %d became %q, want it untouched (%q)", i, got[i], args[i])
		}
	}
	if n := src.timesAsked(); n != 0 {
		t.Errorf("the daemon was asked %d times about arguments that need no resolving", n)
	}
}

// And it does replace a token, in place, leaving its neighbours be.
func TestResolveLinksReplacesOnlyTheToken(t *testing.T) {
	src := &tokenSearcher{magnet: realMagnet}
	c := startSearchDaemon(t, src)

	plain := "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	got, err := c.ResolveLinks([]string{plain, indexer.Token("rutracker", "99"), plain})
	if err != nil {
		t.Fatalf("ResolveLinks: %v", err)
	}
	want := []string{plain, realMagnet, plain}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
	if n := src.timesAsked(); n != 1 {
		t.Errorf("made %d resolve calls, want 1", n)
	}
}
