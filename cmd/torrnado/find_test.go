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

// An unconfigured indexer is the normal state, so the error has to name
// the keys to set rather than merely refusing.
func TestNewSearcherExplainsAnEmptyConfig(t *testing.T) {
	_, err := newSearcher(config.Config{})
	if err == nil {
		t.Fatal("expected an error with no indexer configured")
	}
	for _, want := range []string{"indexer.url", "indexer.api_key", "jackett", "prowlarr"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestNewSearcherBuildsAClient(t *testing.T) {
	cfg := config.Config{Indexer: config.Indexer{
		Backend: "prowlarr", URL: "http://localhost:9696", APIKey: "k",
	}}
	s, err := newSearcher(cfg)
	if err != nil {
		t.Fatalf("newSearcher: %v", err)
	}
	if s.Name() != "prowlarr" {
		t.Errorf("Name() = %q, want prowlarr", s.Name())
	}
}

// config.IndexerBackends and indexer.New are two hand-written lists of
// the same thing, in packages that cannot import each other. This is the
// only place that imports both, so it is the only place the two can be
// held in agreement - the same job keymap_test does for KnownActions.
func TestEveryConfiguredBackendIsBuildable(t *testing.T) {
	for _, name := range config.IndexerBackends {
		if _, err := indexer.New(indexer.Options{Backend: name, URL: "http://x", APIKey: "k"}); err != nil {
			t.Errorf("config accepts backend %q but indexer.New rejects it: %v", name, err)
		}
	}
	if _, err := indexer.New(indexer.Options{Backend: "sonarr", URL: "http://x", APIKey: "k"}); err == nil {
		t.Error("indexer.New accepted a backend config would reject")
	}
	if len(config.IndexerBackends) != len(indexer.Backends) {
		t.Errorf("config lists %v but indexer lists %v", config.IndexerBackends, indexer.Backends)
	}
}
