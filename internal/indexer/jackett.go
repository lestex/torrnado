package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// jackett talks to a Jackett server.
//
// Jackett authenticates by query parameter, which is its documented
// form, and aggregates every configured tracker behind the "all"
// pseudo-indexer.
type jackett struct{}

func (jackett) name() string { return "jackett" }

func (jackett) searchRequest(ctx context.Context, base, apiKey, query string) (*http.Request, error) {
	q := url.Values{"apikey": {apiKey}, "query": {query}}
	return http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/v2.0/indexers/all/results?"+q.Encode(), nil)
}

// healthRequest asks for an indexer that cannot exist.
//
// Jackett has no health endpoint. Querying a made-up indexer is the
// stand-in: a server that is running and accepts the key answers 500
// complaining about the name, while a wrong key is still a 401 and a
// dead server still fails to connect - which is everything the check
// needs to tell apart. See healthOK, which is where it looks backwards.
func (jackett) healthRequest(ctx context.Context, base, apiKey string) (*http.Request, error) {
	q := url.Values{"apikey": {apiKey}}
	return http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/v2.0/indexers/"+healthProbeIndexer+"/results?"+q.Encode(), nil)
}

// healthProbeIndexer is the indexer name healthRequest asks for and
// healthOK looks for in the reply, so the two cannot drift apart.
const healthProbeIndexer = "torrnado_health_probe"

// healthOK treats the complaint as the good news.
//
// Do not "fix" this to require 2xx: a 500 naming the indexer we made up
// is proof the server parsed the request and authenticated it. Matching
// on the name rather than on 500 alone keeps an unrelated server error
// from reading as healthy.
func (jackett) healthOK(status int, body []byte) bool {
	return status == http.StatusInternalServerError &&
		bytes.Contains(body, []byte(healthProbeIndexer))
}

type jackettEnvelope struct {
	Results []jackettResult `json:"Results"`
}

// Pointer fields throughout, because Jackett sends an explicit null for
// anything a tracker does not publish. Decoding null into a plain int is
// a silent no-op in encoding/json - the field keeps its zero value and
// nothing reports a problem - so a missing seeder count would arrive
// indistinguishable from a real zero. See Unknown.
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
		return nil, fmt.Errorf("jackett sent something that is not its usual JSON: %w", err)
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
