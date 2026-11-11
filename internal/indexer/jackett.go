package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// jackett holds the Jackett-specific halves of a search: the request and
// the JSON. Jackett authenticates by query parameter, which is its
// documented form, and aggregates every configured tracker behind the
// "all" pseudo-indexer.
type jackett struct{}

func (jackett) searchRequest(ctx context.Context, base, apiKey, query string) (*http.Request, error) {
	q := url.Values{"apikey": {apiKey}, "query": {query}}
	return http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/v2.0/indexers/all/results?"+q.Encode(), nil)
}

type jackettEnvelope struct {
	Results []jackettResult `json:"Results"`
}

// Pointer fields throughout, because Jackett sends an explicit null for
// anything a tracker does not publish. Decoding null into a plain int is
// a silent no-op in encoding/json - the field keeps its zero value - so a
// missing seeder count would arrive indistinguishable from a real zero.
// See Unknown.
type jackettResult struct {
	Title     *string      `json:"Title"`
	Size      *json.Number `json:"Size"`
	Seeders   *json.Number `json:"Seeders"`
	Peers     *json.Number `json:"Peers"`
	Tracker   *string      `json:"Tracker"`
	MagnetUri *string      `json:"MagnetUri"`
	Link      *string      `json:"Link"`
}

func (jackett) parse(body []byte) ([]Result, error) {
	var env jackettEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("not Jackett's usual JSON: %w", err)
	}
	out := make([]Result, 0, len(env.Results))
	for _, r := range env.Results {
		out = append(out, Result{
			Title:    str(r.Title),
			Size:     numInt64(r.Size),
			Seeders:  numCount(r.Seeders),
			Leechers: numCount(r.Peers),
			Tracker:  str(r.Tracker),
			Link:     firstNonEmpty(r.MagnetUri, r.Link),
		})
	}
	return out, nil
}
