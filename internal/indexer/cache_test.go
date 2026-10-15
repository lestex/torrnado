package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// cached builds a client with a controllable clock, so expiry can be
// tested without sleeping for five minutes.
func cached(t *testing.T, ttl time.Duration, now *time.Time) (*Client, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(jackettBody))
	}))
	t.Cleanup(srv.Close)

	c, err := New(Options{
		Backend: "jackett", URL: srv.URL, APIKey: "k", CacheTTL: ttl,
		now: func() time.Time { return *now },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, &hits
}

// Repeating a query is the common case - esc out of the results, search
// the same thing again - and a fan-out query is slow enough that paying
// for it twice is worth avoiding.
func TestRepeatedSearchesAreServedFromCache(t *testing.T) {
	now := time.Now()
	c, hits := cached(t, time.Minute, &now)
	ctx := context.Background()

	if _, _, err := c.Search(ctx, "ubuntu"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, _, err := c.Search(ctx, "UBUNTU  "); err != nil { // case and spacing do not matter
		t.Fatalf("second: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("made %d requests, want 1 - the second should come from cache", n)
	}

	if _, _, err := c.Search(ctx, "debian"); err != nil {
		t.Fatalf("different query: %v", err)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("made %d requests, want 2 - a different query is not a hit", n)
	}

	now = now.Add(2 * time.Minute) // past the TTL
	if _, _, err := c.Search(ctx, "ubuntu"); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	if n := hits.Load(); n != 3 {
		t.Errorf("made %d requests, want 3 - a stale entry should be refetched", n)
	}
}

func TestCacheCanBeTurnedOff(t *testing.T) {
	now := time.Now()
	c, hits := cached(t, -1, &now)

	for i := 0; i < 3; i++ {
		if _, _, err := c.Search(context.Background(), "ubuntu"); err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
	}
	if n := hits.Load(); n != 3 {
		t.Errorf("made %d requests, want 3 with the cache off", n)
	}
}

// A cached failure would keep answering instantly after the key was
// fixed, which is a baffling thing to debug.
func TestFailuresAreNeverCached(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fails once, then works - the shape of fixing a config.
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(jackettBody))
	}))
	defer srv.Close()

	c, err := New(Options{Backend: "jackett", URL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, _, err := c.Search(context.Background(), "ubuntu"); err == nil {
		t.Fatal("expected the first search to fail")
	}
	got, _, err := c.Search(context.Background(), "ubuntu")
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	if len(got) == 0 {
		t.Error("no results; the failure was cached")
	}
}

// Keyed on user input, so it has to be bounded: a daemon up for a week
// should not accumulate every query ever typed.
func TestCacheIsBounded(t *testing.T) {
	now := time.Now()
	c, _ := cached(t, time.Hour, &now)

	for i := 0; i < maxCacheEntries+10; i++ {
		// Each entry expires a little later than the last, so the one
		// dropped is predictable.
		now = now.Add(time.Second)
		if _, _, err := c.Search(context.Background(), string(rune('a'+i%26))+time.Duration(i).String()); err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
	}

	c.cache.mu.Lock()
	n := len(c.cache.entries)
	c.cache.mu.Unlock()
	if n > maxCacheEntries {
		t.Errorf("cache holds %d entries, want at most %d", n, maxCacheEntries)
	}
}
