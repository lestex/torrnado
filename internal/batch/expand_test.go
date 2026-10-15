package batch

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const magnetA = "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const magnetB = "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// write puts a file in dir and returns its path.
func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return path
}

func TestExpandMagnetPassesThrough(t *testing.T) {
	got, _, err := Expand([]string{magnetA})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 || got[0] != magnetA {
		t.Errorf("got %v, want just the magnet", got)
	}
}

func TestExpandTorrentFile(t *testing.T) {
	path := write(t, t.TempDir(), "x.torrent", "d4:infod4:name1:aee")

	got, _, err := Expand([]string{path})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 || got[0] != path {
		t.Errorf("got %v, want just the path", got)
	}
}

// A directory means the .torrent files directly inside it - not a
// recursive walk, and not the other files that happen to be there.
func TestExpandDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.torrent", "d4:infod4:name1:aee")
	write(t, dir, "b.TORRENT", "d4:infod4:name1:bee") // extension match is case-insensitive
	write(t, dir, "notes.txt", "ignore me")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, _, err := Expand([]string{dir})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want the two .torrent files", got)
	}
}

func TestExpandEmptyDirectoryIsAnError(t *testing.T) {
	if _, _, err := Expand([]string{t.TempDir()}); err == nil {
		t.Error("a directory with no .torrent files should be an error")
	}
}

func TestExpandGlob(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.torrent", "d4:infod4:name1:aee")
	write(t, dir, "b.torrent", "d4:infod4:name1:bee")
	write(t, dir, "c.other", "no")

	got, _, err := Expand([]string{filepath.Join(dir, "*.torrent")})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want two matches", got)
	}
}

// A glob that matches nothing is an error, not an empty result: silently
// adding no torrents looks identical to success.
func TestExpandGlobMatchingNothingIsAnError(t *testing.T) {
	pattern := filepath.Join(t.TempDir(), "*.torrent")
	if _, _, err := Expand([]string{pattern}); err == nil {
		t.Error("a glob matching nothing should be an error")
	}
}

func TestExpandMagnetListFile(t *testing.T) {
	body := strings.Join([]string{
		"# a comment",
		"",
		magnetA,
		"   " + magnetB + "   ", // surrounding space is trimmed
		"",
	}, "\n")
	path := write(t, t.TempDir(), "list.txt", body)

	got, _, err := Expand([]string{path})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 2 || got[0] != magnetA || got[1] != magnetB {
		t.Errorf("got %v, want both magnets in order", got)
	}
}

// A line that is not a magnet is a mistake worth reporting, with the line
// quoted so it can be found.
func TestExpandMagnetListRejectsJunkLines(t *testing.T) {
	path := write(t, t.TempDir(), "list.txt", magnetA+"\nnot a magnet\n")

	_, _, err := Expand([]string{path})
	if err == nil {
		t.Fatal("a non-magnet line should be an error")
	}
	if !strings.Contains(err.Error(), "not a magnet") {
		t.Errorf("error should quote the bad line, got: %v", err)
	}
}

func TestExpandUnknownArgumentIsAnError(t *testing.T) {
	if _, _, err := Expand([]string{"/definitely/not/here"}); err == nil {
		t.Error("a path that does not exist should be an error")
	}
}

// Several arguments flatten into one list, in the order given.
func TestExpandCombinesArguments(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.torrent", "d4:infod4:name1:aee")

	got, _, err := Expand([]string{magnetA, dir})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 2 || got[0] != magnetA {
		t.Errorf("got %v, want the magnet then the file", got)
	}
}

// The URL check has to come before the glob check: query strings are full
// of "?" and "&", so a URL reaches the glob branch first if the order is
// wrong and is reported as a pattern matching nothing.
func TestExpandURLIsNotTreatedAsGlob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("d4:infod4:name1:aee"))
	}))
	defer srv.Close()

	got, cleanup, err := Expand([]string{srv.URL + "/x.torrent?token=1&v=2"})
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %v, want one downloaded file", got)
	}
	// The result is a local path, not the URL: the daemon reads a file.
	if !strings.HasSuffix(got[0], ".torrent") || strings.HasPrefix(got[0], "http") {
		t.Errorf("got %q, want a local .torrent path", got[0])
	}
	body, err := os.ReadFile(got[0])
	if err != nil || string(body) != "d4:infod4:name1:aee" {
		t.Errorf("downloaded file is wrong: %q, %v", body, err)
	}
}

// A server that says no must fail here, rather than handing the daemon an
// HTML error page to parse as a torrent.
func TestExpandURLReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if _, _, err := Expand([]string{srv.URL + "/missing.torrent"}); err == nil {
		t.Error("a 404 should be an error")
	}
}

// An indexer's download link for a release with no .torrent of its own is
// a redirect to the magnet, which is most of what `torrnado find` hands
// back. Following it is net/http's default and fails with "unsupported
// protocol scheme", so the link has to be recognised rather than chased.
func TestExpandURLRedirectingToAMagnetYieldsTheMagnet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, magnetA, http.StatusFound)
	}))
	defer srv.Close()

	got, cleanup, err := Expand([]string{srv.URL + "/dl/abc?jackett_apikey=k"})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	t.Cleanup(cleanup)
	if len(got) != 1 || got[0] != magnetA {
		t.Errorf("got %v, want the magnet it redirected to", got)
	}
}

// A redirect to a page that redirects to the magnet is just as valid, and
// indexers behind a reverse proxy do exactly that.
func TestExpandFollowsARedirectChainToAMagnet(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, magnetB, http.StatusFound)
			return
		}
		http.Redirect(w, r, srv.URL+"/hop", http.StatusMovedPermanently)
	}))
	defer srv.Close()

	got, cleanup, err := Expand([]string{srv.URL + "/dl/abc"})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	t.Cleanup(cleanup)
	if len(got) != 1 || got[0] != magnetB {
		t.Errorf("got %v, want the magnet at the end of the chain", got)
	}
}

// Supplying a CheckRedirect replaces net/http's own ten-redirect limit
// rather than adding to it, so a link that redirects to itself would spin
// until the fetch timeout - 30s of looking hung - without an explicit
// bound.
func TestExpandBoundsTheRedirectChain(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/round-again", http.StatusFound)
	}))
	defer srv.Close()

	_, _, err := Expand([]string{srv.URL + "/dl/abc"})
	if err == nil {
		t.Fatal("a redirect loop should be an error")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("error should say it stopped at the redirects, got: %v", err)
	}
	if hits > maxRedirects+1 {
		t.Errorf("made %d requests, want at most %d", hits, maxRedirects+1)
	}
}

// The daemon keeps its own copy of the metainfo, so a downloaded
// .torrent is litter in /tmp the moment it has been read. Only downloads
// are cleaned: a path the user typed is theirs.
func TestCleanupRemovesDownloadsAndNothingElse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("d4:infod4:name1:aee"))
	}))
	defer srv.Close()
	mine := write(t, t.TempDir(), "mine.torrent", "d4:infod4:name1:bee")

	got, cleanup, err := Expand([]string{srv.URL + "/x.torrent", mine})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want the download and the local file", got)
	}
	cleanup()

	if _, err := os.Stat(got[0]); !os.IsNotExist(err) {
		t.Errorf("downloaded %s still exists after cleanup (err %v)", got[0], err)
	}
	if _, err := os.Stat(mine); err != nil {
		t.Errorf("cleanup removed the user's own file: %v", err)
	}
}

// A later argument failing must not leave the earlier download behind:
// the caller has no list to clean up from, because Expand returned none.
func TestAFailedExpandCleansUpAfterItself(t *testing.T) {
	// A body unique to this test, so a leftover from an earlier run
	// cannot be mistaken for the file this one is looking for.
	const body = "d4:infod4:name24:failed-expand-leak-probeee"
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	_, cleanup, err := Expand([]string{srv.URL + "/x.torrent", "/definitely/not/here"})
	if err == nil {
		t.Fatal("expected the second argument to fail")
	}
	if cleanup == nil {
		t.Fatal("cleanup is nil on the error path; a deferred call would panic")
	}
	cleanup() // must be safe to call anyway

	// The temp file is named by os.CreateTemp, so find it by the pattern
	// rather than by a path we never saw.
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "torrnado-*.torrent"))
	for _, m := range matches {
		if got, err := os.ReadFile(m); err == nil && string(got) == body {
			path = m
			break
		}
	}
	if path != "" {
		os.Remove(path)
		t.Errorf("a download was left behind at %s after Expand failed", path)
	}
}
