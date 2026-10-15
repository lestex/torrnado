package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/config"
	"github.com/lestex/torrnado/internal/indexer"
)

func findFixtures() []indexer.Result {
	return []indexer.Result{
		{Title: "ubuntu-24.04.iso", Size: 5825419264, Seeders: 1243, Leechers: 87,
			Tracker: "ubuntu-official", Link: "magnet:?xt=urn:btih:aaa"},
		{Title: "quiet tracker", Size: 1024, Seeders: indexer.Unknown, Leechers: indexer.Unknown,
			Tracker: "someplace", Link: "http://x/dl/bbb"},
		{Title: "nothing to fetch", Size: 0, Seeders: 0, Leechers: 0,
			Tracker: "deadsite", Link: ""},
	}
}

// Two different things must not look alike: a tracker that publishes no
// counts, and a torrent with no seeders that will never finish.
func TestFindTableDistinguishesUnknownFromZero(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFindTable(&buf, findFixtures(), 25); err != nil {
		t.Fatalf("writeFindTable: %v", err)
	}
	got := buf.String()

	lines := strings.Split(strings.TrimSpace(got), "\n")
	if !strings.Contains(lines[0], "TITLE") || !strings.Contains(lines[0], "SEEDS") {
		t.Errorf("no header row, got: %q", lines[0])
	}

	var unknownRow, zeroRow string
	for _, l := range lines {
		if strings.Contains(l, "quiet tracker") {
			unknownRow = l
		}
		if strings.Contains(l, "nothing to fetch") {
			zeroRow = l
		}
	}
	if !strings.Contains(unknownRow, "-") {
		t.Errorf("an unreported count should render as -, got: %q", unknownRow)
	}
	if !strings.Contains(zeroRow, "0") {
		t.Errorf("a real zero should render as 0, got: %q", zeroRow)
	}
	// A size of zero is unreported too, not a zero-byte torrent.
	if !strings.Contains(zeroRow, "-") {
		t.Errorf("an unreported size should render as -, got: %q", zeroRow)
	}
	// Listed so the count matches the indexer, but flagged as unusable.
	if !strings.Contains(zeroRow, "no link") {
		t.Errorf("a result with no link should say so, got: %q", zeroRow)
	}
}

// A magnet would run past 200 characters and push every other column off
// the screen, so the table carries none. --link and --json are the way out.
func TestFindTableOmitsTheLink(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFindTable(&buf, findFixtures(), 25); err != nil {
		t.Fatalf("writeFindTable: %v", err)
	}
	if strings.Contains(buf.String(), "magnet:?xt=") {
		t.Error("the table printed a magnet link")
	}
}

func TestFindTableLimit(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFindTable(&buf, findFixtures(), 1); err != nil {
		t.Fatalf("writeFindTable: %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "quiet tracker") {
		t.Error("--limit 1 printed a second result")
	}
	// Silently truncating would leave the user thinking that was all
	// there was.
	if !strings.Contains(got, "1 of 3 shown") {
		t.Errorf("a truncated table should say so, got:\n%s", got)
	}

	buf.Reset()
	if err := writeFindTable(&buf, findFixtures(), 0); err != nil {
		t.Fatalf("writeFindTable: %v", err)
	}
	if !strings.Contains(buf.String(), "nothing to fetch") {
		t.Error("--limit 0 should show everything")
	}
}

func TestFindTableSaysWhenThereIsNothing(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFindTable(&buf, nil, 25); err != nil {
		t.Fatalf("writeFindTable: %v", err)
	}
	if !strings.Contains(buf.String(), "no results") {
		t.Errorf("want a plain statement, got: %q", buf.String())
	}
}

// The JSON shape is a public contract: scripts pipe it into jq.
func TestFindJSONRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFindJSON(&buf, findFixtures(), 25); err != nil {
		t.Fatalf("writeFindJSON: %v", err)
	}

	var got []indexer.Result
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	want := findFixtures()
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
	if !strings.Contains(buf.String(), `"link"`) {
		t.Error("JSON output must carry the link; it is the point of --json")
	}
}

// jq on "null" is a worse error than jq on an empty array.
func TestFindJSONEmptyIsAnArray(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFindJSON(&buf, nil, 25); err != nil {
		t.Fatalf("writeFindJSON: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("got %q, want []", got)
	}
}

func TestFindLink(t *testing.T) {
	var buf bytes.Buffer
	// Numbered from 1, matching the table's # column, because that is
	// where the number is read off.
	if err := writeFindLink(&buf, findFixtures(), 1); err != nil {
		t.Fatalf("writeFindLink: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "magnet:?xt=urn:btih:aaa" {
		t.Errorf("got %q, want the first result's magnet alone", got)
	}

	if err := writeFindLink(&bytes.Buffer{}, findFixtures(), 9); err == nil {
		t.Error("asking past the end should be an error, not empty output")
	}
	if err := writeFindLink(&bytes.Buffer{}, findFixtures(), 3); err == nil {
		t.Error("a result with no link should be an error, not an empty line")
	}
}

// Nothing configured is the normal state, not a failure: newSearcher
// says so by returning nil rather than an error, and the daemon answers
// a search by naming the keys to set.
func TestNewSearcherWithNothingConfigured(t *testing.T) {
	s, err := newSearcher(config.Config{})
	if err != nil {
		t.Fatalf("an empty config is not an error: %v", err)
	}
	if s != nil {
		t.Errorf("searcher = %v, want nil when nothing is configured", s)
	}
}

func TestNewSearcherBuildsOneSource(t *testing.T) {
	cfg := config.Config{Search: config.Search{
		Prowlarr: config.Aggregator{URL: "http://localhost:9696", APIKey: "k"},
	}}
	s, err := newSearcher(cfg)
	if err != nil {
		t.Fatalf("newSearcher: %v", err)
	}
	// One source is used as it is rather than wrapped - a lone source
	// needs no fan-out.
	if got := s.Sources(); len(got) != 1 || got[0] != "prowlarr" {
		t.Errorf("sources = %v, want [prowlarr]", got)
	}
}

// Both configured is what the merge exists for, and the order reported
// is the order they are searched.
func TestNewSearcherBuildsEverySource(t *testing.T) {
	cfg := config.Config{Search: config.Search{
		Jackett:  config.Aggregator{URL: "http://localhost:9117", APIKey: "jk"},
		Prowlarr: config.Aggregator{URL: "http://localhost:9696", APIKey: "pk"},
	}}
	s, err := newSearcher(cfg)
	if err != nil {
		t.Fatalf("newSearcher: %v", err)
	}
	got := s.Sources()
	if len(got) != 2 || got[0] != "jackett" || got[1] != "prowlarr" {
		t.Errorf("sources = %v, want [jackett prowlarr]", got)
	}
}

// What the daemon tells a client when nothing is set up has to name the
// keys, since having no indexer is the ordinary state rather than a
// mistake.
func TestFindSourcesSaysWhatToConfigure(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFindSources(&buf, nil); err != nil {
		t.Fatalf("writeFindSources: %v", err)
	}
	for _, want := range []string{"search.jackett.url", "search.jackett.api_key", "prowlarr"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("should mention %q, got: %q", want, buf.String())
		}
	}

	buf.Reset()
	if err := writeFindSources(&buf, []string{"jackett", "prowlarr"}); err != nil {
		t.Fatalf("writeFindSources: %v", err)
	}
	if !strings.Contains(buf.String(), "jackett, prowlarr") {
		t.Errorf("should name both sources, got: %q", buf.String())
	}
}
