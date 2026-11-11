// Package indexer searches a Jackett server for torrents matching a query.
//
// torrnado never talks to a tracker. Jackett is a service the user runs
// themselves, usually on localhost, holding the per-tracker configuration
// and credentials; this package makes one HTTP GET against it and Jackett
// fans the query out. The only traffic here is to the user's own machine,
// which is why searching is not gated on the vpn.required guard the way
// transfers are: the request that reaches a tracker is Jackett's, over
// Jackett's route, and we can neither see it nor hold it back.
//
// The package has no internal dependencies and uses only the standard
// library. It does not import internal/config: the daemon translates
// configuration into Options.
package indexer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Unknown is the seeder or leecher count of a result whose tracker did
// not report one.
//
// It is not zero, and the difference matters: zero seeders means a dead
// torrent that will never finish, which is exactly the thing a user
// scans this list to avoid. Rendering "not reported" as "0" would
// condemn a healthy torrent on a tracker that simply keeps its counts to
// itself. Use format.Count to render it.
const Unknown = -1

// Result is one torrent Jackett returned.
//
// The json tags are a public contract: `torrnado find --json` emits this
// shape for scripts to consume, and the daemon sends the same struct over
// the socket.
type Result struct {
	Title    string `json:"title"`
	Size     int64  `json:"size"`    // bytes; 0 when not reported
	Seeders  int    `json:"seeders"` // Unknown when not reported
	Leechers int    `json:"leechers"`
	Tracker  string `json:"tracker"`

	// Link is a magnet: URI or an http(s) URL to a .torrent file -
	// whichever Jackett gave, since not every tracker publishes a magnet.
	// One field rather than two, because a result has exactly one way to
	// be fetched. internal/batch already dispatches on the prefix, so this
	// goes to the add path as-is. Empty when Jackett gave neither; such a
	// result is still listed so the count matches what Jackett found.
	Link string `json:"link"`
}

// IsMagnet reports whether Link is a magnet URI rather than a URL that
// has to be fetched first.
func (r Result) IsMagnet() bool { return strings.HasPrefix(r.Link, "magnet:") }

// DefaultTimeout bounds one search. Jackett queries every configured
// tracker and answers when the slowest has, so this is generous; a search
// that outruns it reports a timeout rather than hanging a client.
const DefaultTimeout = 30 * time.Second

// maxBody caps what is read from Jackett. A search over many trackers is
// a few MB at the outside; a runaway reply must not exhaust the daemon.
const maxBody = 32 << 20

// Options configures a Client.
type Options struct {
	URL    string // Jackett's base URL
	APIKey string
	// Timeout bounds one search; zero means DefaultTimeout.
	Timeout time.Duration
	// HTTP overrides the transport, for tests.
	HTTP *http.Client
}

// Client is a Jackett search client. Safe for concurrent use.
type Client struct {
	url    string
	apiKey string
	http   *http.Client
}

// New validates o and returns a Client. It makes no request.
func New(o Options) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(o.URL), "/")
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("search.jackett.url %q is not an http(s) URL", o.URL)
	}
	if strings.TrimSpace(o.APIKey) == "" {
		return nil, errors.New("search.jackett.api_key is empty")
	}
	hc := o.HTTP
	if hc == nil {
		t := o.Timeout
		if t <= 0 {
			t = DefaultTimeout
		}
		hc = &http.Client{Timeout: t}
	}
	return &Client{url: base, apiKey: strings.TrimSpace(o.APIKey), http: hc}, nil
}

// Search asks Jackett's "all" pseudo-indexer for query and returns the
// results best-first (seeders descending, unknown counts last). A single
// attempt: a retry would only delay the message telling the user what to
// fix.
func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	req, err := jackett{}.searchRequest(ctx, c.url, c.apiKey, query)
	if err != nil {
		return nil, &Error{Kind: KindUnreachable, URL: c.url, Err: err}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, c.classify(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, c.classify(err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, &Error{Kind: KindAuth, URL: c.url, Status: resp.StatusCode}
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, &Error{Kind: KindStatus, URL: c.url, Status: resp.StatusCode}
	}

	results, err := jackett{}.parse(body)
	if err != nil {
		return nil, &Error{Kind: KindDecode, URL: c.url, Err: err}
	}
	sortResults(results)
	return results, nil
}

// sortResults orders by seeders descending, stable so Jackett's own order
// breaks ties. Unknown is -1, so unreported counts sort below a real zero
// without any special case - which is why Unknown must stay negative.
func sortResults(rs []Result) {
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Seeders > rs[j].Seeders })
}
