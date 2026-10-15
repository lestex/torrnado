package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The two backends describe the same four torrents in their own field
// names. Both fixtures are deliberately the same data, so the test that
// normalizes them can assert they come out identical - which is what
// keeps a third backend honest when someone adds one.
//
// Row 2 is the case that matters most: an aggregator sends an explicit
// null for a count no tracker published, and a plain int field would
// read that as a real zero, which the list renders as a dead torrent.
const jackettBody = `{"Results":[
 {"Title":"ubuntu-24.04.iso","Size":5825419264,"Seeders":1243,"Peers":87,
  "Tracker":"ubuntu-official","MagnetUri":"magnet:?xt=urn:btih:aaa","Link":"http://jack/dl/aaa"},
 {"Title":null,"Size":null,"Seeders":null,"Peers":null,"Tracker":null,
  "MagnetUri":null,"Link":"http://jack/dl/bbb"},
 {"Title":"no magnet here","Size":1024,"Seeders":4,"Peers":1,
  "Tracker":"someplace","MagnetUri":"","Link":"http://jack/dl/ccc"},
 {"Title":"nothing to fetch","Size":2048,"Seeders":0,"Peers":0,
  "Tracker":"deadsite","MagnetUri":null,"Link":null}
]}`

const prowlarrBody = `[
 {"title":"ubuntu-24.04.iso","size":5825419264,"seeders":1243,"leechers":87,
  "indexer":"ubuntu-official","magnetUrl":"magnet:?xt=urn:btih:aaa","downloadUrl":"http://jack/dl/aaa"},
 {"title":null,"size":null,"seeders":null,"leechers":null,"indexer":null,
  "magnetUrl":null,"downloadUrl":"http://jack/dl/bbb"},
 {"title":"no magnet here","size":1024,"seeders":4,"leechers":1,
  "indexer":"someplace","magnetUrl":"","downloadUrl":"http://jack/dl/ccc"},
 {"title":"nothing to fetch","size":2048,"seeders":0,"leechers":0,
  "indexer":"deadsite","magnetUrl":null,"downloadUrl":null}
]`

// serve starts a server answering every request with body and status,
// and returns a client pointed at it plus a counter of requests made.
func serve(t *testing.T, backend, body string, status int) (*Client, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c, err := New(Options{Backend: backend, URL: srv.URL, APIKey: "k", CacheTTL: -1})
	if err != nil {
		t.Fatalf("New(%s): %v", backend, err)
	}
	return c, &hits
}

// The whole point of the package: whatever the backend called them, the
// caller sees one shape. Both fixtures describe the same torrents, so
// both must normalize to exactly the same slice.
func TestBothBackendsNormalizeToTheSameResults(t *testing.T) {
	want := []Result{
		{Title: "ubuntu-24.04.iso", Size: 5825419264, Seeders: 1243, Leechers: 87,
			Tracker: "ubuntu-official", Link: "magnet:?xt=urn:btih:aaa"},
		{Title: "no magnet here", Size: 1024, Seeders: 4, Leechers: 1,
			Tracker: "someplace", Link: "http://jack/dl/ccc"},
		{Title: "nothing to fetch", Size: 2048, Seeders: 0, Leechers: 0,
			Tracker: "deadsite", Link: ""},
		// Unknown sorts last, below a genuine zero. Both are at the
		// bottom, which is where an unrankable result belongs.
		{Title: "", Size: 0, Seeders: Unknown, Leechers: Unknown,
			Tracker: "", Link: "http://jack/dl/bbb"},
	}

	for _, tc := range []struct{ backend, body string }{
		{"jackett", jackettBody},
		{"prowlarr", prowlarrBody},
	} {
		t.Run(tc.backend, func(t *testing.T) {
			c, _ := serve(t, tc.backend, tc.body, http.StatusOK)

			got, err := c.Search(context.Background(), "ubuntu")
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(got) != len(want) {
				t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("result %d:\n got %+v\nwant %+v", i, got[i], want[i])
				}
			}
		})
	}
}

// A count nobody published is not a count of zero. Spelled out on its
// own because every other assertion here would still pass if Unknown
// quietly became 0, and a healthy torrent on a quiet tracker would then
// look dead.
func TestAbsentCountsAreUnknownNotZero(t *testing.T) {
	for _, tc := range []struct{ backend, body string }{
		{"jackett", jackettBody},
		{"prowlarr", prowlarrBody},
	} {
		t.Run(tc.backend, func(t *testing.T) {
			c, _ := serve(t, tc.backend, tc.body, http.StatusOK)
			got, err := c.Search(context.Background(), "ubuntu")
			if err != nil {
				t.Fatalf("Search: %v", err)
			}

			var found bool
			for _, r := range got {
				if r.Link == "http://jack/dl/bbb" {
					found = true
					if r.Seeders != Unknown || r.Leechers != Unknown {
						t.Errorf("seeders = %d, leechers = %d, want both Unknown (%d)",
							r.Seeders, r.Leechers, Unknown)
					}
				}
			}
			if !found {
				t.Error("the all-null row was dropped; it should still be listed")
			}
		})
	}
}

// Not every tracker publishes a magnet, so a result can arrive with only
// a URL - and an empty string means as little as a null does.
func TestLinkFallsBackToTheDownloadURL(t *testing.T) {
	for _, tc := range []struct{ backend, body string }{
		{"jackett", jackettBody},
		{"prowlarr", prowlarrBody},
	} {
		t.Run(tc.backend, func(t *testing.T) {
			c, _ := serve(t, tc.backend, tc.body, http.StatusOK)
			got, err := c.Search(context.Background(), "ubuntu")
			if err != nil {
				t.Fatalf("Search: %v", err)
			}

			by := map[string]Result{}
			for _, r := range got {
				by[r.Title] = r
			}
			if l := by["ubuntu-24.04.iso"].Link; l != "magnet:?xt=urn:btih:aaa" {
				t.Errorf("link = %q, want the magnet to win when both are present", l)
			}
			if !by["ubuntu-24.04.iso"].IsMagnet() {
				t.Error("IsMagnet() = false for a magnet link")
			}
			if l := by["no magnet here"].Link; l != "http://jack/dl/ccc" {
				t.Errorf("link = %q, want the URL when the magnet is an empty string", l)
			}
			if l := by["nothing to fetch"].Link; l != "" {
				t.Errorf("link = %q, want empty when neither is usable", l)
			}
		})
	}
}

// An aggregator returns its own relevance ranking, which routinely puts
// dead torrents on top. Sorting once here means the TUI and the CLI
// cannot disagree about what "best" means.
func TestResultsComeBackSeedersFirst(t *testing.T) {
	c, _ := serve(t, "jackett", jackettBody, http.StatusOK)

	got, err := c.Search(context.Background(), "ubuntu")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Seeders < got[i].Seeders {
			t.Fatalf("not sorted by seeders: %d before %d", got[i-1].Seeders, got[i].Seeders)
		}
	}
}

func TestSearchRequestShape(t *testing.T) {
	for _, tc := range []struct {
		backend  string
		body     string
		wantPath string
	}{
		{"jackett", jackettBody, "/api/v2.0/indexers/all/results"},
		{"prowlarr", prowlarrBody, "/api/v1/search"},
	} {
		t.Run(tc.backend, func(t *testing.T) {
			var gotPath, gotQuery, gotKeyParam, gotKeyHeader, gotIndexers string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotQuery = r.URL.Query().Get("query")
				gotKeyParam = r.URL.Query().Get("apikey")
				gotIndexers = r.URL.Query().Get("indexerIds")
				gotKeyHeader = r.Header.Get("X-Api-Key")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			// A trailing slash on the configured URL must not become a
			// double slash in the path.
			c, err := New(Options{Backend: tc.backend, URL: srv.URL + "/", APIKey: "secret", CacheTTL: -1})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			// A query with a space and an & is the one that breaks if
			// anything is concatenated rather than encoded.
			if _, err := c.Search(context.Background(), "tom & jerry"); err != nil {
				t.Fatalf("Search: %v", err)
			}

			if gotPath != tc.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tc.wantPath)
			}
			if gotQuery != "tom & jerry" {
				t.Errorf("query = %q, want it decoded back to what was asked", gotQuery)
			}

			// Jackett documents the key as a parameter; Prowlarr wants a
			// header, which also keeps it out of any proxy's access log.
			if tc.backend == "jackett" {
				if gotKeyParam != "secret" {
					t.Errorf("apikey param = %q, want the key", gotKeyParam)
				}
			} else {
				if gotKeyHeader != "secret" {
					t.Errorf("X-Api-Key = %q, want the key", gotKeyHeader)
				}
				if gotKeyParam != "" {
					t.Errorf("apikey param = %q, want the key only in the header", gotKeyParam)
				}
				// Without this a Prowlarr holding usenet indexers returns
				// rows whose download URL is an .nzb.
				if gotIndexers != torrentIndexers {
					t.Errorf("indexerIds = %q, want %q so usenet results stay out",
						gotIndexers, torrentIndexers)
				}
			}
		})
	}
}
