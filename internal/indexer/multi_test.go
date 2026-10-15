package indexer

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubSource is one source in a fan-out.
type stubSource struct {
	name    string
	results []Result
	err     error
	delay   time.Duration
	calls   atomic.Int32
}

func (s *stubSource) Sources() []string { return []string{s.name} }

func (s *stubSource) Search(ctx context.Context, query string) ([]Result, []string, error) {
	s.calls.Add(1)
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.results, nil, nil
}

func res(title string, seeders int, hash string) Result {
	link := ""
	if hash != "" {
		link = "magnet:?xt=urn:btih:" + hash + "&dn=" + title
	}
	return Result{Title: title, Seeders: seeders, Link: link}
}

// One source is used as it is: a lone source needs no fan-out, and
// wrapping it would dress its own errors up as a partial failure.
func TestNewMultiUnwrapsASingleSource(t *testing.T) {
	one := &stubSource{name: "jackett"}
	if got := NewMulti(one); got != Searcher(one) {
		t.Errorf("got %T, want the source itself", got)
	}
	if NewMulti() != nil {
		t.Error("no sources should be nil, not an empty Multi")
	}
}

// Merging is the point: results from both sources interleaved by
// seeders, not one source's list after the other's.
func TestMultiMergesAndSortsAcrossSources(t *testing.T) {
	a := &stubSource{name: "a", results: []Result{res("a-big", 900, "aa"), res("a-small", 10, "bb")}}
	b := &stubSource{name: "b", results: []Result{res("b-mid", 400, "cc")}}

	got, warnings, err := NewMulti(a, b).Search(context.Background(), "x")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	var titles []string
	for _, r := range got {
		titles = append(titles, r.Title)
	}
	want := []string{"a-big", "b-mid", "a-small"}
	if strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", titles, want)
	}
}

// The sources are asked at once. Run serially, two 200ms sources take
// 400ms; the bound here fails that and passes concurrency with room to
// spare on a loaded machine.
func TestMultiSearchesConcurrently(t *testing.T) {
	a := &stubSource{name: "a", delay: 200 * time.Millisecond, results: []Result{res("a", 1, "aa")}}
	b := &stubSource{name: "b", delay: 200 * time.Millisecond, results: []Result{res("b", 2, "bb")}}

	start := time.Now()
	got, _, err := NewMulti(a, b).Search(context.Background(), "x")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if elapsed > 350*time.Millisecond {
		t.Errorf("took %s, want the sources searched at once", elapsed)
	}
}

// "This source is down" and "nothing matched" are different answers, and
// collapsing them would have a user hunting for a torrent that was never
// searched for.
func TestMultiReportsAFailedSourceWithoutLosingTheRest(t *testing.T) {
	ok := &stubSource{name: "jackett", results: []Result{res("found", 5, "aa")}}
	bad := &stubSource{name: "prowlarr", err: errors.New("connection refused")}

	got, warnings, err := NewMulti(ok, bad).Search(context.Background(), "x")
	if err != nil {
		t.Fatalf("one source failing should not fail the search: %v", err)
	}
	if len(got) != 1 || got[0].Title != "found" {
		t.Errorf("results = %+v, want the working source's", got)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one", warnings)
	}
	// Named, because "a source failed" is useless when several are
	// configured.
	if !strings.Contains(warnings[0], "prowlarr") || !strings.Contains(warnings[0], "connection refused") {
		t.Errorf("warning = %q, want it to name the source and the reason", warnings[0])
	}
}

// Every source failing is a failed search, not a thin one - otherwise an
// empty table reads as "nothing matched".
func TestMultiFailsWhenEverySourceDoes(t *testing.T) {
	a := &stubSource{name: "a", err: errors.New("down")}
	b := &stubSource{name: "b", err: errors.New("also down")}

	_, _, err := NewMulti(a, b).Search(context.Background(), "x")
	if err == nil {
		t.Fatal("expected an error when no source answered")
	}
	for _, want := range []string{"a", "b", "down"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

// Two aggregators fronting the same tracker return the same torrent. The
// infohash is the only identifier that means the same thing to both.
func TestMultiDropsDuplicatesByInfohash(t *testing.T) {
	// Same hash, different titles and case - as two indexers really do
	// report one torrent.
	a := &stubSource{name: "a", results: []Result{res("Ubuntu 24.04", 900, "AABBCC")}}
	b := &stubSource{name: "b", results: []Result{res("ubuntu-24.04.iso", 800, "aabbcc")}}

	got, _, err := NewMulti(a, b).Search(context.Background(), "x")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(got), got)
	}
	// The first source wins, so the earlier one's metadata is what shows.
	if got[0].Title != "Ubuntu 24.04" {
		t.Errorf("kept %q, want the first source's copy", got[0].Title)
	}
}

// A result with no magnet has no infohash to compare, and dropping one
// on a guess would lose a torrent nothing else offered.
func TestMultiKeepsResultsWithNoMagnet(t *testing.T) {
	a := &stubSource{name: "a", results: []Result{
		{Title: "no magnet one", Link: "http://a/one.torrent"},
		{Title: "no magnet two", Link: "http://b/two.torrent"},
	}}

	got, _, err := NewMulti(a, &stubSource{name: "b"}).Search(context.Background(), "x")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d results, want both kept: %+v", len(got), got)
	}
}

// Most sources already name themselves in their errors, so prefixing
// unconditionally reads as "prowlarr: prowlarr: cannot reach ...".
func TestMultiDoesNotRepeatANameTheErrorAlreadyCarries(t *testing.T) {
	selfNaming := &stubSource{name: "prowlarr", err: errors.New("prowlarr: cannot reach http://x")}
	anonymous := &stubSource{name: "jackett", err: errors.New("cannot reach http://y")}
	ok := &stubSource{name: "other", results: []Result{res("found", 1, "aa")}}

	_, warnings, err := NewMulti(selfNaming, anonymous, ok).Search(context.Background(), "x")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want two", warnings)
	}
	if strings.Count(warnings[0], "prowlarr") != 1 {
		t.Errorf("warning = %q, want the source named once", warnings[0])
	}
	// The one that does not name itself still has to be attributed.
	if !strings.HasPrefix(warnings[1], "jackett: ") {
		t.Errorf("warning = %q, want it to name the source", warnings[1])
	}
}

func TestMultiSourcesNamesThemInOrder(t *testing.T) {
	m := NewMulti(&stubSource{name: "jackett"}, &stubSource{name: "prowlarr"})
	got := m.Sources()
	if len(got) != 2 || got[0] != "jackett" || got[1] != "prowlarr" {
		t.Errorf("sources = %v, want [jackett prowlarr]", got)
	}
}

func TestInfoHash(t *testing.T) {
	for _, c := range []struct{ link, want string }{
		{"magnet:?xt=urn:btih:ABC123&dn=x", "abc123"},
		{"magnet:?dn=x&xt=urn:btih:abc123", "abc123"},
		{"magnet:?xt=urn:btih:abc123", "abc123"},
		{"http://example/x.torrent", ""},
		{"", ""},
	} {
		if got := infoHash(c.link); got != c.want {
			t.Errorf("infoHash(%q) = %q, want %q", c.link, got, c.want)
		}
	}
}
