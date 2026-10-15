package indexer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Multi searches several sources at once and merges what comes back.
//
// Concurrently, because the slowest source would otherwise set the pace
// for all of them and a search already takes seconds.
//
// A source that fails does not sink the search. Its message comes back
// as a warning beside the results the others found, which is the honest
// answer: "jackett is down" and "nothing matched" are different things,
// and collapsing them would have a user hunting for a torrent that was
// never searched for. Only every source failing is an error.
type Multi struct {
	sources []Searcher
}

// NewMulti builds a Multi. One source is returned as itself rather than
// wrapped: a lone source needs no fan-out, and unwrapping keeps its own
// errors unembellished.
func NewMulti(sources ...Searcher) Searcher {
	switch len(sources) {
	case 0:
		return nil
	case 1:
		return sources[0]
	}
	return &Multi{sources: sources}
}

// Sources names every source, in the order they were given.
func (m *Multi) Sources() []string {
	names := make([]string, 0, len(m.sources))
	for _, s := range m.sources {
		names = append(names, s.Sources()...)
	}
	return names
}

// Search asks every source and merges the answers.
func (m *Multi) Search(ctx context.Context, query string) ([]Result, []string, error) {
	type answer struct {
		results  []Result
		warnings []string
		err      error
	}

	answers := make([]answer, len(m.sources))
	var wg sync.WaitGroup
	for i, src := range m.sources {
		wg.Add(1)
		go func(i int, src Searcher) {
			defer wg.Done()
			r, w, err := src.Search(ctx, query)
			answers[i] = answer{results: r, warnings: w, err: err}
		}(i, src)
	}
	wg.Wait()

	var (
		merged   []Result
		warnings []string
		failed   int
	)
	for i, a := range answers {
		warnings = append(warnings, a.warnings...)
		if a.err != nil {
			failed++
			// Named, because "a source failed" is useless when several
			// are configured - the message has to say which one to go
			// and look at. Most already say so themselves, so prefixing
			// unconditionally would stutter: "prowlarr: prowlarr: ...".
			warnings = append(warnings, nameError(m.sources[i].Sources(), a.err))
			continue
		}
		merged = append(merged, a.results...)
	}

	// Every source failing is a failed search, not a thin one. Reported
	// as an error so a caller does not render an empty table as though
	// the query simply matched nothing.
	if failed == len(m.sources) {
		return nil, nil, errors.New("every indexer failed: " + strings.Join(warnings, "; "))
	}

	merged = dedupe(merged)
	// Best-first across the merged set, not within each source: two
	// sources' results interleaved by seeders is the point of merging.
	// Stable, so results that tie keep the order their source gave.
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Seeders > merged[j].Seeders
	})
	return merged, warnings, nil
}

// nameError makes sure a failure says which source it came from, without
// saying it twice.
func nameError(names []string, err error) string {
	msg := err.Error()
	for _, n := range names {
		if strings.HasPrefix(msg, n+":") {
			return msg
		}
	}
	return strings.Join(names, ", ") + ": " + msg
}

// dedupe drops results two sources both reported.
//
// Matched on the magnet's infohash, which is the only identifier that
// means the same thing everywhere: titles differ by tracker and sizes
// collide. A result with no magnet is always kept, since there is
// nothing to compare - better a duplicate row than a silently dropped
// one. The first copy wins, so the earlier source's metadata is what
// shows.
func dedupe(results []Result) []Result {
	seen := make(map[string]bool, len(results))
	out := results[:0]
	for _, r := range results {
		h := infoHash(r.Link)
		if h == "" {
			out = append(out, r)
			continue
		}
		if seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, r)
	}
	return out
}

// infoHash pulls the btih out of a magnet URI, lowercased. Empty for
// anything that is not a magnet.
func infoHash(link string) string {
	const marker = "xt=urn:btih:"
	i := strings.Index(strings.ToLower(link), marker)
	if i < 0 {
		return ""
	}
	h := link[i+len(marker):]
	if j := strings.IndexAny(h, "&/?"); j >= 0 {
		h = h[:j]
	}
	return strings.ToLower(h)
}

// ResolveToken finds the member that issued the token and asks it.
//
// Multi deliberately does not implement Resolver itself: Resolve takes
// an id with no source name on it, which a Multi has no way to route.
func (m *Multi) ResolveToken(ctx context.Context, source, id string) (string, error) {
	for _, s := range m.sources {
		if !slices.Contains(s.Sources(), source) {
			continue
		}
		r, ok := s.(Resolver)
		if !ok {
			return "", fmt.Errorf("%s does not use links that need resolving", source)
		}
		return r.Resolve(ctx, id)
	}
	return "", fmt.Errorf("no source named %q is configured", source)
}

// ResolveToken turns a token into something addable, whatever shape the
// searcher is: one source, or several behind a Multi.
//
// It lives here rather than on the interface so that a Searcher stays
// the two methods it was: most sources return real links and have
// nothing to resolve, and the daemon should not have to know which kind
// it is holding before it can add a result.
func ResolveToken(ctx context.Context, s Searcher, token string) (string, error) {
	source, id, ok := ParseToken(token)
	if !ok {
		return "", fmt.Errorf("%q is not a source token", token)
	}
	switch v := s.(type) {
	case nil:
		return "", fmt.Errorf("%q needs source %q, which this daemon is not searching", token, source)
	case *Multi:
		return v.ResolveToken(ctx, source, id)
	default:
		if !slices.Contains(s.Sources(), source) {
			return "", fmt.Errorf("%q needs source %q, which this daemon is not searching", token, source)
		}
		r, ok := s.(Resolver)
		if !ok {
			return "", fmt.Errorf("%s does not use links that need resolving", source)
		}
		return r.Resolve(ctx, id)
	}
}
