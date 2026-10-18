package indexer

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

// fakeBrowser stands in for FlareSolverr. It answers with whatever page
// the test chose and records what it was asked for, including the
// cookies - a search that forgets to carry the login is the failure this
// source exists to avoid.
type fakeBrowser struct {
	pages    map[string]string // url substring -> body
	fallback string
	gets     []string
	posts    []url.Values
	cookies  []map[string]string
	err      error
}

func (f *fakeBrowser) Get(_ context.Context, pageURL string, cookies map[string]string) (string, error) {
	f.gets = append(f.gets, pageURL)
	f.cookies = append(f.cookies, cookies)
	if f.err != nil {
		return "", f.err
	}
	for frag, body := range f.pages {
		if strings.Contains(pageURL, frag) {
			return body, nil
		}
	}
	return f.fallback, nil
}

func (f *fakeBrowser) Post(_ context.Context, _ string, form url.Values, _ map[string]string) (string, error) {
	f.posts = append(f.posts, form)
	if f.err != nil {
		return "", f.err
	}
	return f.fallback, nil
}

func rutrackerWith(t *testing.T, b Fetcher, o RutrackerOptions) *Rutracker {
	t.Helper()
	if o.Session == "" && o.Username == "" {
		o.Session = "cookie-value"
	}
	r, err := NewRutracker(b, o)
	if err != nil {
		t.Fatalf("NewRutracker: %v", err)
	}
	return r
}

// The fixture is the real search page's result rows, so this is the
// parser working against the markup as it actually ships.
func TestRutrackerParsesTheSearchPage(t *testing.T) {
	b := &fakeBrowser{fallback: fixture(t, "rutracker_search.html")}
	r := rutrackerWith(t, b, RutrackerOptions{})

	got, warnings, err := r.Search(context.Background(), "ubuntu")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(got) != 6 {
		t.Fatalf("got %d results, want the fixture's 6 rows", len(got))
	}

	// The cookie has to be on the request, or the page comes back as the
	// login page and every row is lost.
	if len(b.cookies) != 1 || b.cookies[0][sessionCookie] != "cookie-value" {
		t.Errorf("the search did not carry the session cookie: %v", b.cookies)
	}
	if len(b.gets) != 1 || !strings.Contains(b.gets[0], "tracker.php?nm=ubuntu") {
		t.Errorf("fetched %v, want the search page for the query", b.gets)
	}

	for i, r := range got {
		if r.Title == "" {
			t.Errorf("result %d has no title", i)
		}
		if r.Tracker != RutrackerName {
			t.Errorf("result %d tracker = %q", i, r.Tracker)
		}
		src, id, ok := ParseToken(r.Link)
		if !ok || src != RutrackerName || id == "" {
			t.Errorf("result %d link = %q, want a %s token", i, r.Link, RutrackerName)
		}
		if r.Size <= 0 {
			t.Errorf("result %d size = %d, want the exact byte count", i, r.Size)
		}
		if r.Leechers == Unknown {
			t.Errorf("result %d has no leecher count", i)
		}
	}

	// Leechers are the one count the page prints without a sort value
	// beside it, so they are read from the cell text - and a parser that
	// read the missing attribute instead would return a plausible zero
	// for every row rather than failing.
	var peers int
	for _, r := range got {
		peers += r.Leechers
	}
	if peers == 0 {
		t.Error("every result has 0 leechers, which is what reading the wrong attribute looks like")
	}
}

// The size is taken from the sort attribute, not the "3.16 GB" the page
// shows, so it is exact rather than rounded to three digits.
func TestRutrackerSizeIsExactBytes(t *testing.T) {
	b := &fakeBrowser{fallback: fixture(t, "rutracker_search.html")}
	r := rutrackerWith(t, b, RutrackerOptions{})

	got, _, err := r.Search(context.Background(), "ubuntu")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var found bool
	for _, res := range got {
		// 3.16 GB rounded would be a multiple of 10 MB; the real value
		// is not.
		if res.Size == 3388492241 {
			found = true
		}
	}
	if !found {
		var sizes []int64
		for _, res := range got {
			sizes = append(sizes, res.Size)
		}
		t.Errorf("no result carried the exact byte count 3388492241; got %v", sizes)
	}
}

// A torrent with no seeders shows how long it has been that way instead
// of a number. That is zero, and it must not arrive as Unknown: Unknown
// renders as "-", which reads as "the tracker is quiet about it" rather
// than "this will never finish".
func TestRutrackerReadsADeadTorrentAsZeroNotUnknown(t *testing.T) {
	b := &fakeBrowser{fallback: fixture(t, "rutracker_search.html")}
	r := rutrackerWith(t, b, RutrackerOptions{})

	got, _, err := r.Search(context.Background(), "x")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	var zero, unknown int
	for _, res := range got {
		switch res.Seeders {
		case 0:
			zero++
		case Unknown:
			unknown++
		}
	}
	if zero != 1 {
		t.Errorf("%d results have 0 seeders, want exactly the fixture's one dead row", zero)
	}
	if unknown != 0 {
		t.Errorf("%d results came back Unknown; rutracker always reports a count", unknown)
	}

	// Best-first, so the dead one is last.
	if got[len(got)-1].Seeders != 0 {
		t.Errorf("last result has %d seeders, want the dead row sorted to the bottom", got[len(got)-1].Seeders)
	}
}

func TestRutrackerResolvesATokenToAMagnet(t *testing.T) {
	b := &fakeBrowser{pages: map[string]string{
		"viewtopic.php?t=6916386": fixture(t, "rutracker_topic.html"),
	}}
	r := rutrackerWith(t, b, RutrackerOptions{})

	magnet, err := r.Resolve(context.Background(), "6916386")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.HasPrefix(magnet, "magnet:?xt=urn:btih:") {
		t.Errorf("Resolve returned %q, want a magnet", magnet)
	}
	if !strings.Contains(strings.ToUpper(magnet), "18B545207F06C786A04144C6CA8161C920FC8CEC") {
		t.Errorf("magnet does not carry the topic's infohash: %q", magnet)
	}
}

func TestRutrackerRejectsATokenThatIsNotATopicID(t *testing.T) {
	r := rutrackerWith(t, &fakeBrowser{}, RutrackerOptions{})
	if _, err := r.Resolve(context.Background(), "not-a-number"); err == nil {
		t.Error("expected an error for a non-numeric topic id")
	}
}

// An expired cookie returns the login page. Reporting that as zero
// results would read as "nothing matched", which is the wrong thing to
// go and fix.
func TestRutrackerSaysWhenTheSessionHasExpired(t *testing.T) {
	b := &fakeBrowser{fallback: fixture(t, "rutracker_login.html")}
	r := rutrackerWith(t, b, RutrackerOptions{Session: "stale"})

	_, _, err := r.Search(context.Background(), "ubuntu")
	if err == nil {
		t.Fatal("expected an error, got the login page parsed as results")
	}
	if !strings.Contains(err.Error(), "search.rutracker.session") {
		t.Errorf("error does not name the key to fix: %v", err)
	}
}

// With credentials, an expired session is re-established rather than
// reported.
func TestRutrackerLogsInAgainWhenTheSessionExpires(t *testing.T) {
	login := fixture(t, "rutracker_login.html")
	search := fixture(t, "rutracker_search.html")

	// The first fetch answers with the login page; the retry after
	// logging in answers with the real one.
	b2 := &stagedBrowser{bodies: []string{login, search}, post: search}
	r := rutrackerWith(t, b2, RutrackerOptions{Username: "u", Password: "p", Session: "stale"})

	got, _, err := r.Search(context.Background(), "ubuntu")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) == 0 {
		t.Error("no results after logging in again")
	}
	if len(b2.posts) != 1 {
		t.Errorf("made %d login posts, want 1", len(b2.posts))
	}
	if b2.posts[0].Get("login_username") != "u" {
		t.Errorf("login form = %v, want the configured username", b2.posts[0])
	}
}

// A captcha cannot be answered from here, so the message has to point at
// the way through rather than just failing.
func TestRutrackerExplainsACaptcha(t *testing.T) {
	b := &stagedBrowser{bodies: []string{"<html>login_username</html>"}, post: `<html><img src="cap_sid"></html>`}
	r := rutrackerWith(t, b, RutrackerOptions{Username: "u", Password: "p"})

	_, _, err := r.Search(context.Background(), "ubuntu")
	if err == nil {
		t.Fatal("expected a captcha error")
	}
	if !strings.Contains(err.Error(), "search.rutracker.session") {
		t.Errorf("the message should say to paste a cookie instead: %v", err)
	}
	if !strings.Contains(err.Error(), sessionCookie) {
		t.Errorf("the message should name the cookie: %v", err)
	}
}

func TestNewRutrackerNeedsAFetcherAndCredentials(t *testing.T) {
	if _, err := NewRutracker(nil, RutrackerOptions{Session: "s"}); err == nil {
		t.Error("expected an error without a fetcher")
	} else if !strings.Contains(err.Error(), "flaresolverr.url") {
		t.Errorf("error should name the key: %v", err)
	}

	if _, err := NewRutracker(&fakeBrowser{}, RutrackerOptions{}); err == nil {
		t.Error("expected an error with neither a session nor credentials")
	}
	if _, err := NewRutracker(&fakeBrowser{}, RutrackerOptions{Username: "u"}); err == nil {
		t.Error("a username without a password should not be enough")
	}
}

// A page load here is a browser solving a challenge, so repeating a
// query must not pay for it twice.
func TestRutrackerCachesResults(t *testing.T) {
	b := &fakeBrowser{fallback: fixture(t, "rutracker_search.html")}
	now := time.Now()
	r := rutrackerWith(t, b, RutrackerOptions{
		CacheTTL: time.Minute,
		now:      func() time.Time { return now },
	})

	for i := 0; i < 3; i++ {
		if _, _, err := r.Search(context.Background(), "UBUNTU "); err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
	}
	if len(b.gets) != 1 {
		t.Errorf("made %d page loads, want 1 - the rest should be cached", len(b.gets))
	}

	now = now.Add(2 * time.Minute)
	if _, _, err := r.Search(context.Background(), "ubuntu"); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	if len(b.gets) != 2 {
		t.Errorf("made %d page loads, want 2 - a stale entry should be refetched", len(b.gets))
	}
}

func TestRutrackerSearchNeedsAQuery(t *testing.T) {
	b := &fakeBrowser{fallback: fixture(t, "rutracker_search.html")}
	r := rutrackerWith(t, b, RutrackerOptions{})
	if _, _, err := r.Search(context.Background(), "  "); err == nil {
		t.Error("expected an error for a blank query")
	}
	if len(b.gets) != 0 {
		t.Errorf("made %d page loads, want 0 for a blank query", len(b.gets))
	}
}

// stagedBrowser answers each Get with the next body in turn, so a login
// retry can be tested.
type stagedBrowser struct {
	bodies []string
	post   string
	gets   []string
	posts  []url.Values
}

func (s *stagedBrowser) Get(_ context.Context, pageURL string, _ map[string]string) (string, error) {
	s.gets = append(s.gets, pageURL)
	if len(s.bodies) == 0 {
		return "", nil
	}
	body := s.bodies[0]
	if len(s.bodies) > 1 {
		s.bodies = s.bodies[1:]
	}
	return body, nil
}

func (s *stagedBrowser) Post(_ context.Context, _ string, form url.Values, _ map[string]string) (string, error) {
	s.posts = append(s.posts, form)
	return s.post, nil
}
