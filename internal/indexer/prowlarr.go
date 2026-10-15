package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// prowlarr talks to a Prowlarr server.
type prowlarr struct{}

func (prowlarr) name() string { return "prowlarr" }

// torrentIndexers is Prowlarr's id for "every torrent indexer".
//
// Prowlarr aggregates usenet as well as torrents, and a usenet result
// has no magnet and an .nzb behind its download URL - nothing torrnado
// can act on. Asking for -2 keeps them out at the server rather than
// listing rows that fail only once chosen.
const torrentIndexers = "-2"

func (prowlarr) searchRequest(ctx context.Context, base, apiKey, query string) (*http.Request, error) {
	q := url.Values{"query": {query}, "indexerIds": {torrentIndexers}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	setProwlarrKey(req, apiKey)
	return req, nil
}

func (prowlarr) healthRequest(ctx context.Context, base, apiKey string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/health", nil)
	if err != nil {
		return nil, err
	}
	setProwlarrKey(req, apiKey)
	return req, nil
}

// setProwlarrKey puts the key in a header rather than the query string.
//
// A header is what Prowlarr documents for /api/v1, and it keeps the key
// out of anything that logs URLs - a reverse proxy's access log in front
// of the server, most obviously. Prowlarr also accepts ?apikey=, so this
// is a choice rather than a requirement.
func setProwlarrKey(req *http.Request, apiKey string) {
	req.Header.Set("X-Api-Key", apiKey)
}

// healthOK: /api/v1/health is a real endpoint, so 2xx means 2xx. (The
// body lists warnings about the user's indexers, which are their
// business and not a reason to call the server unhealthy.)
func (prowlarr) healthOK(status int, _ []byte) bool {
	return status >= 200 && status <= 299
}

// Pointer fields for the same reason as Jackett's: Prowlarr sends an
// explicit null for counts an indexer does not publish, and a plain int
// would silently read that as zero.
type prowlarrResult struct {
	Title       *string      `json:"title"`
	Size        *json.Number `json:"size"`
	Seeders     *json.Number `json:"seeders"`
	Leechers    *json.Number `json:"leechers"`
	Indexer     *string      `json:"indexer"`
	MagnetURL   *string      `json:"magnetUrl"`
	DownloadURL *string      `json:"downloadUrl"`
}

func (prowlarr) parse(body []byte) ([]Result, error) {
	// A bare array, where Jackett wraps its results in an object.
	var raw []prowlarrResult
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("prowlarr sent something that is not its usual JSON: %w", err)
	}

	out := make([]Result, 0, len(raw))
	for _, r := range raw {
		out = append(out, Result{
			Title:    str(r.Title),
			Size:     numInt64(r.Size),
			Seeders:  numCount(r.Seeders),
			Leechers: numCount(r.Leechers),
			Tracker:  str(r.Indexer),
			Link:     firstNonEmpty(r.MagnetURL, r.DownloadURL),
		})
	}
	return out, nil
}
