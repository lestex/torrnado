package indexer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sample = `{"Results":[
 {"Title":"few","Size":100,"Seeders":2,"Peers":1,"Tracker":"A","MagnetUri":null,"Link":"http://j/dl/1"},
 {"Title":"many","Size":2147483648,"Seeders":50,"Peers":3,"Tracker":"B","MagnetUri":"magnet:?xt=urn:btih:aa","Link":"http://j/dl/2"},
 {"Title":"unreported","Size":null,"Seeders":null,"Peers":null,"Tracker":null,"MagnetUri":null,"Link":null},
 {"Title":"dead","Size":5,"Seeders":0,"Peers":0,"Tracker":"C","MagnetUri":"","Link":"http://j/dl/3"}
]}`

func newFake(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Options{URL: srv.URL + "/", APIKey: "k3y"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSearchParsesAndSorts(t *testing.T) {
	var gotPath, gotKey, gotQuery string
	c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotQuery = r.URL.Path, r.URL.Query().Get("apikey"), r.URL.Query().Get("query")
		_, _ = w.Write([]byte(sample))
	})
	rs, err := c.Search(context.Background(), "ubuntu 24.04 & more")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v2.0/indexers/all/results" || gotKey != "k3y" || gotQuery != "ubuntu 24.04 & more" {
		t.Fatalf("request: path=%q key=%q query=%q", gotPath, gotKey, gotQuery)
	}
	if len(rs) != 4 {
		t.Fatalf("got %d results, want 4", len(rs))
	}
	order := []string{"many", "few", "dead", "unreported"}
	for i, want := range order {
		if rs[i].Title != want {
			t.Errorf("rank %d = %q, want %q", i, rs[i].Title, want)
		}
	}
	// A magnet beats the .torrent link; a missing one falls back.
	if !rs[0].IsMagnet() || rs[1].Link != "http://j/dl/1" || rs[1].IsMagnet() {
		t.Errorf("links: %+v %+v", rs[0], rs[1])
	}
	// null is Unknown, distinct from a reported zero.
	if rs[3].Seeders != Unknown || rs[3].Leechers != Unknown {
		t.Errorf("null counts = %d/%d, want Unknown", rs[3].Seeders, rs[3].Leechers)
	}
	if rs[2].Seeders != 0 {
		t.Errorf("reported zero = %d", rs[2].Seeders)
	}
	if rs[0].Size != 2147483648 {
		t.Errorf("size = %d", rs[0].Size)
	}
}

func TestSearchErrorKinds(t *testing.T) {
	kind := func(err error) Kind {
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("not an *Error: %v", err)
		}
		return e.Kind
	}
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   Kind
	}{
		{"auth 401", 401, "", KindAuth},
		{"auth 403", 403, "", KindAuth},
		{"server", 500, "boom", KindStatus},
		{"garbage", 200, "<html>", KindDecode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.Search(context.Background(), "x")
			if got := kind(err); got != tc.want {
				t.Fatalf("kind = %d, want %d (%v)", got, tc.want, err)
			}
		})
	}
}

func TestAuthMessageNamesTheKeyAndNeverTheSecret(t *testing.T) {
	c := newFake(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	_, err := c.Search(context.Background(), "x")
	msg := err.Error()
	if want := "search.jackett.api_key"; !contains(msg, want) {
		t.Errorf("%q should name %s", msg, want)
	}
	if contains(msg, "k3y") {
		t.Errorf("error leaks the API key: %q", msg)
	}
}

func TestUnreachableAndTimeout(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c, _ := New(Options{URL: url, APIKey: "k"})
	_, err := c.Search(context.Background(), "x")
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindUnreachable {
		t.Fatalf("closed server: %v", err)
	}
	if !contains(err.Error(), "search.jackett.url") {
		t.Errorf("no remedy in %q", err)
	}

	slow := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = slow.Search(ctx, "x")
	if !errors.As(err, &e) || e.Kind != KindTimeout {
		t.Fatalf("slow server: %v", err)
	}
}

func TestNewValidates(t *testing.T) {
	for _, o := range []Options{
		{URL: "", APIKey: "k"},
		{URL: "localhost:9117", APIKey: "k"},
		{URL: "ftp://x", APIKey: "k"},
		{URL: "http://x", APIKey: "  "},
	} {
		if _, err := New(o); err == nil {
			t.Errorf("New(%+v) should fail", o)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
