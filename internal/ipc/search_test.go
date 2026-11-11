package ipc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lestex/torrnado/internal/indexer"
)

// fakeSearcher answers from a function, so a test decides what a search
// does - succeed, fail, or block.
type fakeSearcher func(ctx context.Context, q string) ([]indexer.Result, error)

func (f fakeSearcher) Search(ctx context.Context, q string) ([]indexer.Result, error) {
	return f(ctx, q)
}

func TestSearchNotConfigured(t *testing.T) {
	_, err := startTestDaemon(t).Search("ubuntu")
	if err == nil || err.Error() != NotConfiguredMsg {
		t.Fatalf("err = %v, want exactly %q", err, NotConfiguredMsg)
	}
}

func TestSearchRoundTrip(t *testing.T) {
	want := []indexer.Result{
		{Title: "a", Size: 1 << 33, Seeders: 9, Leechers: indexer.Unknown, Tracker: "T", Link: "magnet:?xt=urn:btih:aa"},
		{Title: "b", Seeders: 0},
	}
	var gotQuery string
	c := startTestDaemon(t, WithSearcher(fakeSearcher(func(_ context.Context, q string) ([]indexer.Result, error) {
		gotQuery = q
		return want, nil
	})))
	got, err := c.Search("  ubuntu 24.04 ")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "ubuntu 24.04" {
		t.Errorf("searcher saw %q, want the trimmed query", gotQuery)
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSearchErrorPassesThroughUnprefixed(t *testing.T) {
	c := startTestDaemon(t, WithSearcher(fakeSearcher(func(context.Context, string) ([]indexer.Result, error) {
		return nil, errors.New("jackett: rejected the API key - check search.jackett.api_key")
	})))
	_, err := c.Search("x")
	if err == nil || err.Error() != "jackett: rejected the API key - check search.jackett.api_key" {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.Search("   "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("blank query: %v", err)
	}
}

// The reason search is answered off the connection goroutine: a search
// that takes a minute must not freeze the list the TUI redraws from.
func TestSlowSearchDoesNotBlockOtherCalls(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	c := startTestDaemon(t, WithSearcher(fakeSearcher(func(ctx context.Context, _ string) ([]indexer.Result, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return []indexer.Result{{Title: "late"}}, nil
	})))

	done := make(chan error, 1)
	go func() { _, err := c.Search("slow"); done <- err }()
	<-started

	listed := make(chan error, 1)
	go func() { _, err := c.List(); listed <- err }()
	select {
	case err := <-listed:
		if err != nil {
			t.Fatalf("List during a search: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("List was blocked behind a running search")
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Search: %v", err)
	}
}

// Closing the client abandons the search rather than leaving the daemon
// asking Jackett for a reply nobody will read.
func TestSearchStopsWhenClientDisconnects(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	c := startTestDaemon(t, WithSearcher(fakeSearcher(func(ctx context.Context, _ string) ([]indexer.Result, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})))
	go func() { _, _ = c.Search("x") }()
	<-started
	c.Close()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("search was not cancelled when its client went away")
	}
}

func TestOldDaemonIsReadable(t *testing.T) {
	// What call() returns when a pre-search daemon answers the method it
	// does not know.
	got := searchError(errors.New(`Search: unknown method "Search"`)).Error()
	if !strings.Contains(got, "too old") || !strings.Contains(got, "torrnado stop") {
		t.Errorf("got %q", got)
	}
	if got := searchError(errors.New("Search: timed out after 2m0s")).Error(); got != "timed out after 2m0s" {
		t.Errorf("got %q", got)
	}
}
