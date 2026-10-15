package indexer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A token travels through the add path beside magnets and file paths, so
// nothing that is already addable may be mistaken for one.
func TestParseTokenRejectsEverythingAddable(t *testing.T) {
	for _, s := range []string{
		"magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"http://localhost:9117/dl/abc",
		"https://host/x.torrent",
		"/home/me/x.torrent",
		"./x.torrent",
		"C:\\torrents\\x.torrent",
		"x.torrent",
		"rutracker:",
		":6916386",
		"",
		"rutracker:6916386/../etc",
		"RUTRACKER:6916386",
		"rutracker:6916386 ubuntu",
		strings.Repeat("a", 33) + ":1",
		"rutracker:" + strings.Repeat("1", 129),
	} {
		if _, _, ok := ParseToken(s); ok {
			t.Errorf("ParseToken(%q) accepted it as a token", s)
		}
	}
}

func TestParseTokenAcceptsATokenAndRoundTrips(t *testing.T) {
	src, id, ok := ParseToken(Token("rutracker", "6916386"))
	if !ok {
		t.Fatal("a token this package built was not recognised")
	}
	if src != "rutracker" || id != "6916386" {
		t.Errorf("got %q / %q", src, id)
	}
}

// resolverSource is a Searcher that issues tokens, like a site plugin.
type resolverSource struct {
	name   string
	magnet string
	err    error
	asked  []string
}

func (r *resolverSource) Sources() []string { return []string{r.name} }
func (r *resolverSource) Search(context.Context, string) ([]Result, []string, error) {
	return nil, nil, nil
}
func (r *resolverSource) Resolve(_ context.Context, id string) (string, error) {
	r.asked = append(r.asked, id)
	return r.magnet, r.err
}

// plainSource is an aggregator: its results carry real links, so it has
// nothing to resolve.
type plainSource struct{ name string }

func (p plainSource) Sources() []string { return []string{p.name} }
func (p plainSource) Search(context.Context, string) ([]Result, []string, error) {
	return nil, nil, nil
}

func TestResolveTokenRoutesToTheSourceThatIssuedIt(t *testing.T) {
	rt := &resolverSource{name: "rutracker", magnet: "magnet:?xt=urn:btih:abc"}
	other := &resolverSource{name: "elsewhere", magnet: "magnet:?xt=urn:btih:wrong"}
	multi := NewMulti(plainSource{"jackett"}, other, rt)

	got, err := ResolveToken(context.Background(), multi, "rutracker:6916386")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if got != rt.magnet {
		t.Errorf("got %q, want the magnet from the source named in the token", got)
	}
	if len(rt.asked) != 1 || rt.asked[0] != "6916386" {
		t.Errorf("rutracker was asked for %v, want just the id", rt.asked)
	}
	if len(other.asked) != 0 {
		t.Errorf("the wrong source was asked: %v", other.asked)
	}
}

func TestResolveTokenWorksWithASingleSource(t *testing.T) {
	rt := &resolverSource{name: "rutracker", magnet: "magnet:?xt=urn:btih:abc"}
	// NewMulti returns a lone source unwrapped, so this is the other
	// shape the daemon can be holding.
	got, err := ResolveToken(context.Background(), NewMulti(rt), "rutracker:1")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if got != rt.magnet {
		t.Errorf("got %q", got)
	}
}

// Naming the source is what makes this message useful: the fix is to
// configure that source, and nothing else in the message would say so.
func TestResolveTokenNamesAnUnconfiguredSource(t *testing.T) {
	for _, s := range []Searcher{nil, NewMulti(plainSource{"jackett"}), plainSource{"jackett"}} {
		_, err := ResolveToken(context.Background(), s, "rutracker:6916386")
		if err == nil {
			t.Fatalf("searcher %T: expected an error", s)
		}
		if !strings.Contains(err.Error(), "rutracker") {
			t.Errorf("searcher %T: error does not name the source: %v", s, err)
		}
	}
}

func TestResolveTokenRejectsWhatIsNotAToken(t *testing.T) {
	rt := &resolverSource{name: "rutracker"}
	if _, err := ResolveToken(context.Background(), NewMulti(rt), "magnet:?xt=urn:btih:abc"); err == nil {
		t.Error("a magnet is not a token and should not be resolved")
	}
}

// A source that cannot resolve says so rather than being skipped, or an
// unconfigured source and a misconfigured one would read the same.
func TestResolveTokenReportsTheSourcesOwnFailure(t *testing.T) {
	rt := &resolverSource{name: "rutracker", err: errors.New("the session cookie has expired")}
	_, err := ResolveToken(context.Background(), NewMulti(rt, plainSource{"jackett"}), "rutracker:1")
	if err == nil {
		t.Fatal("expected the source's error")
	}
	if !strings.Contains(err.Error(), "session cookie has expired") {
		t.Errorf("error lost the source's own message: %v", err)
	}
}
