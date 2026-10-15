package indexer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// kindOf pulls the Kind out of an error, failing if it is not ours. A
// bare transport error reaching a caller would name neither the
// aggregator nor the URL, which is most of what makes these messages
// useful.
func kindOf(t *testing.T, err error) Kind {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error is %T, want *indexer.Error: %v", err, err)
	}
	return e.Kind
}

// A rejected key and a server that is down are the two things a new
// user actually hits, so each gets its own message naming the config key
// to look at.
func TestFailuresAreClassifiedAndExplained(t *testing.T) {
	t.Run("bad api key", func(t *testing.T) {
		c, hits := serve(t, "jackett", `{"error":"no"}`, http.StatusUnauthorized)

		_, _, err := c.Search(context.Background(), "x")
		if got := kindOf(t, err); got != KindAuth {
			t.Errorf("kind = %v, want KindAuth", got)
		}
		if !strings.Contains(err.Error(), "search.jackett.api_key") {
			t.Errorf("message does not say which key to fix: %v", err)
		}
		// Retrying an auth failure only triples the time to the message
		// that explains it.
		if n := hits.Load(); n != 1 {
			t.Errorf("made %d requests, want 1 - a 401 must not be retried", n)
		}
	})

	t.Run("other http status", func(t *testing.T) {
		c, hits := serve(t, "prowlarr", "upstream is sulking", http.StatusBadGateway)

		_, _, err := c.Search(context.Background(), "x")
		if got := kindOf(t, err); got != KindStatus {
			t.Errorf("kind = %v, want KindStatus", got)
		}
		if !strings.Contains(err.Error(), "502") {
			t.Errorf("message does not carry the status: %v", err)
		}
		if n := hits.Load(); n != 1 {
			t.Errorf("made %d requests, want 1", n)
		}
	})

	t.Run("nothing listening", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close() // the port is now closed

		c, err := New(Options{Backend: "jackett", URL: url, APIKey: "k", CacheTTL: -1})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_, _, err = c.Search(context.Background(), "x")
		if got := kindOf(t, err); got != KindUnreachable {
			t.Errorf("kind = %v, want KindUnreachable", got)
		}
		if !strings.Contains(err.Error(), "search.jackett.url") {
			t.Errorf("message does not suggest checking the URL: %v", err)
		}
	})

	t.Run("not the json we expect", func(t *testing.T) {
		c, _ := serve(t, "jackett", "<html>login page</html>", http.StatusOK)

		_, _, err := c.Search(context.Background(), "x")
		if got := kindOf(t, err); got != KindDecode {
			t.Errorf("kind = %v, want KindDecode", got)
		}
	})
}

// A timeout is the one failure worth retrying, and the count is exact
// because each attempt costs the user the full timeout.
func TestTimeoutsAreRetriedExactlyAttemptsTimes(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-r.Context().Done() // never answer
	}))
	defer srv.Close()

	c, err := New(Options{
		Backend: "jackett", URL: srv.URL, APIKey: "k",
		Timeout: 30 * time.Millisecond, Attempts: 2, CacheTTL: -1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = c.Search(context.Background(), "x")
	if got := kindOf(t, err); got != KindTimeout {
		t.Errorf("kind = %v, want KindTimeout", got)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("made %d attempts, want exactly 2", n)
	}
}

// The health check is what turns "no results" into "you have it
// misconfigured", so it has to tell a working server from a wrong key.
func TestHealth(t *testing.T) {
	t.Run("jackett alive answers 500 about the probe", func(t *testing.T) {
		// Not a mistake: Jackett has no health endpoint, so the check
		// asks for an indexer that cannot exist and a 500 naming it is
		// proof the server parsed and authenticated the request.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"Indexer ` + healthProbeIndexer + ` not found"}`))
		}))
		defer srv.Close()

		c, _ := New(Options{Backend: "jackett", URL: srv.URL, APIKey: "k"})
		if err := c.Health(context.Background()); err != nil {
			t.Errorf("Health() = %v, want nil - a 500 naming the probe means alive", err)
		}
	})

	t.Run("an unrelated 500 is not healthy", func(t *testing.T) {
		c, _ := serve(t, "jackett", "database is on fire", http.StatusInternalServerError)
		if err := c.Health(context.Background()); err == nil {
			t.Error("Health() = nil, want an error - only the probe's own 500 counts")
		}
	})

	t.Run("prowlarr", func(t *testing.T) {
		c, _ := serve(t, "prowlarr", `[]`, http.StatusOK)
		if err := c.Health(context.Background()); err != nil {
			t.Errorf("Health() = %v, want nil", err)
		}
	})

	t.Run("a bad key is still a bad key", func(t *testing.T) {
		c, _ := serve(t, "prowlarr", "", http.StatusUnauthorized)
		if got := kindOf(t, c.Health(context.Background())); got != KindAuth {
			t.Errorf("kind = %v, want KindAuth", got)
		}
	})
}

func TestNewRejectsAnUnknownBackend(t *testing.T) {
	_, err := New(Options{Backend: "sonarr", URL: "http://x", APIKey: "k"})
	if err == nil {
		t.Fatal("expected an error for an unknown backend")
	}
	for _, name := range Backends {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error should list %q as a choice, got: %v", name, err)
		}
	}
}

func TestSearchNeedsAQuery(t *testing.T) {
	c, hits := serve(t, "jackett", jackettBody, http.StatusOK)
	if _, _, err := c.Search(context.Background(), "   "); err == nil {
		t.Error("expected an error for a blank query")
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("made %d requests, want 0 - a blank query should not reach the server", n)
	}
}
