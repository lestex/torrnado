// Package indexer searches a self-hosted torrent aggregator - Jackett or
// Prowlarr - for torrents matching a query.
//
// torrnado never talks to a tracker. An aggregator is a service the user
// runs themselves, usually on localhost, holding the per-tracker
// configuration and credentials; this package makes one HTTP GET against
// it and the aggregator fans the query out. So the only traffic here is
// to the user's own machine, which is why searching is not gated on the
// vpn.required guard the way transfers are: the request that reaches a
// tracker is the aggregator's, over the aggregator's route, and we can
// neither see it nor hold it back.
//
// The package has no internal dependencies and uses only the standard
// library, which keeps it in the same tier as format, engine and vpn.
// It deliberately does not import internal/config: the caller translates
// configuration into Options, so validating a config file - which
// `torrnado daemon` does on every start - never links an HTTP client for
// an indexer the daemon will never call.
package indexer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// Result is one torrent an aggregator returned, normalized out of the
// backends' different field names.
//
// The json tags are a public contract: `torrnado find --json` emits this
// shape for scripts to consume.
type Result struct {
	Title    string `json:"title"`
	Size     int64  `json:"size"`    // bytes; 0 when not reported
	Seeders  int    `json:"seeders"` // Unknown when not reported
	Leechers int    `json:"leechers"`
	Tracker  string `json:"tracker"`

	// Link is a magnet: URI or an http(s) URL to a .torrent file -
	// whichever the aggregator gave, since not every tracker publishes a
	// magnet. One field rather than two, because a result has exactly one
	// way to be fetched and a pair would model four states of which three
	// are invalid. internal/batch already dispatches on the prefix, so
	// this goes to the add path as-is.
	//
	// Empty when the aggregator gave neither. Such a result is still
	// listed: dropping it would make the count disagree with what the
	// aggregator said it found, which is harder to explain than a row
	// that declines to be added.
	Link string `json:"link"`
}

// IsMagnet reports whether Link is a magnet URI rather than a URL that
// has to be fetched first.
func (r Result) IsMagnet() bool { return strings.HasPrefix(r.Link, "magnet:") }

// NotConfiguredMsg is the one answer every client gives when the daemon
// is holding no searcher at all.
//
// A single constant rather than a sentence in each client, because there
// were three copies and they drifted: the TUI's still named the keys of
// the [indexer] table that [search.*] replaced, so pressing the find key
// sent the user off to write a config file the loader would then reject
// for naming keys that no longer exist.
//
// It names Jackett first deliberately. An aggregator is the way in - it
// holds the per-tracker configuration and fans one query out to all of
// them - and a built-in source is the exception to that, so the answer
// everyone sees should point at the route almost everyone wants.
const NotConfiguredMsg = "search is not configured: set search.jackett.url and " +
	"search.jackett.api_key in config.toml (or search.prowlarr.*), which fans one " +
	"query out to every tracker configured in it"

// Searcher is what the daemon holds: one source, or several behind a
// Multi. An interface so a fake can stand in for it, and so "nothing is
// configured" can be a nil interface rather than a flag beside a client.
type Searcher interface {
	// Search returns results ordered best-first (seeders descending),
	// and one message per source that failed while others succeeded.
	// An error means the whole search failed, not that a source did.
	Search(ctx context.Context, query string) (results []Result, warnings []string, err error)
	// Sources names what this will search, for a client to report
	// without having to run a search to find out.
	Sources() []string
}

// backend is the per-aggregator half: which paths, which JSON. Everything
// else - retries, timeouts, caching, error classification - lives on
// Client, so adding a third aggregator is one file and one case in New
// rather than a second copy of the machinery.
//
// It builds a whole *http.Request rather than returning a URL, because
// the two backends authenticate differently: Jackett takes the key as a
// query parameter and Prowlarr wants an X-Api-Key header. A URL alone
// cannot express that.
type backend interface {
	name() string
	searchRequest(ctx context.Context, base, apiKey, query string) (*http.Request, error)
	healthRequest(ctx context.Context, base, apiKey string) (*http.Request, error)
	parse(body []byte) ([]Result, error)
	healthOK(status int, body []byte) bool
}

// Tunables. Not configurable in v1: three config keys (backend, url,
// api_key) is the surface worth documenting, and these are only
// interesting to a test.
const (
	// DefaultTimeout bounds one attempt, not the whole call.
	DefaultTimeout = 10 * time.Second
	// DefaultCacheTTL is how long a result set stays fresh.
	DefaultCacheTTL = 5 * time.Minute

	defaultAttempts = 3
	baseBackoff     = 500 * time.Millisecond
	maxCacheEntries = 64
)

// Options configures a Client. The zero value of every tunable means
// "the default", so a caller sets only Backend, URL and APIKey.
type Options struct {
	Backend string // "jackett" or "prowlarr"
	URL     string // the aggregator's base URL
	APIKey  string

	Timeout time.Duration // per attempt; 0 means DefaultTimeout
	// Attempts is how many times a timed-out request is tried. The TUI
	// passes 2 rather than the default 3: it cannot cancel an in-flight
	// search, so the worst case is time the user can only wait out.
	Attempts int
	// CacheTTL is how long to reuse a result set. 0 means
	// DefaultCacheTTL; negative turns the cache off.
	CacheTTL time.Duration

	// HTTP overrides the client, for tests. nil builds one from Timeout.
	HTTP *http.Client
	// now overrides the clock, for cache-expiry tests.
	now func() time.Time
}

// Backends are the names New accepts.
var Backends = []string{"jackett", "prowlarr"}

// Client searches one aggregator.
type Client struct {
	backend  backend
	url      string
	apiKey   string
	http     *http.Client
	attempts int
	ttl      time.Duration
	now      func() time.Time

	cache cache
}

// New builds a Client for one of Backends.
func New(o Options) (*Client, error) {
	var b backend
	switch strings.ToLower(strings.TrimSpace(o.Backend)) {
	case "jackett":
		b = jackett{}
	case "prowlarr":
		b = prowlarr{}
	default:
		return nil, fmt.Errorf("unknown indexer backend %q (known: %s)",
			o.Backend, strings.Join(Backends, ", "))
	}

	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	httpClient := o.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	attempts := o.Attempts
	if attempts <= 0 {
		attempts = defaultAttempts
	}
	ttl := o.CacheTTL
	if ttl == 0 {
		ttl = DefaultCacheTTL
	}
	now := o.now
	if now == nil {
		now = time.Now
	}

	return &Client{
		backend: b,
		// Trimmed here so a URL written with or without a trailing slash
		// builds the same path rather than one with "//" in it.
		url:      strings.TrimRight(strings.TrimSpace(o.URL), "/"),
		apiKey:   o.APIKey,
		http:     httpClient,
		attempts: attempts,
		ttl:      ttl,
		now:      now,
	}, nil
}

func (c *Client) Name() string { return c.backend.name() }

// Sources is one name: a Client is one aggregator. Several are a Multi.
func (c *Client) Sources() []string { return []string{c.backend.name()} }

// Search queries the aggregator, newest cache entry first.
//
// The warnings return is always nil here: one source either answers or
// fails. It exists so a Client and a Multi are the same shape to a
// caller, which is what lets the daemon hold either.
func (c *Client) Search(ctx context.Context, query string) ([]Result, []string, error) {
	key := strings.ToLower(strings.TrimSpace(query))
	if key == "" {
		return nil, nil, errors.New("find: needs a query")
	}
	if hit, ok := c.cache.get(key, c.now()); ok {
		return hit, nil, nil
	}

	body, status, err := c.do(ctx, func(ctx context.Context) (*http.Request, error) {
		return c.backend.searchRequest(ctx, c.url, c.apiKey, query)
	})
	if err != nil {
		return nil, nil, err
	}
	if status < 200 || status > 299 {
		return nil, nil, &Error{Kind: KindStatus, Backend: c.backend.name(), URL: c.url, Status: status}
	}

	results, err := c.backend.parse(body)
	if err != nil {
		return nil, nil, &Error{Kind: KindDecode, Backend: c.backend.name(), URL: c.url, Err: err}
	}

	// Best-first here rather than in each caller: an aggregator returns
	// its own relevance guess, which routinely puts dead torrents at the
	// top, and every caller wants the healthy ones first. Stable, so
	// results that tie keep the aggregator's ranking.
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Seeders > results[j].Seeders
	})

	// Only successes are cached. A cached 401 would keep answering
	// instantly after the key was fixed, which is a confusing bug to be
	// on the wrong end of.
	c.cache.put(key, results, c.now().Add(c.ttl), c.ttl)
	return results, nil, nil
}

// Health reports whether the aggregator answers and accepts the key.
//
// What counts as healthy is the backend's to say, because Jackett has no
// health endpoint and the stand-in for one answers 500 when it is
// working. That inversion must not reach Search, where a 500 is a plain
// failure - which is why do reports the status rather than judging it.
func (c *Client) Health(ctx context.Context) error {
	body, status, err := c.do(ctx, func(ctx context.Context) (*http.Request, error) {
		return c.backend.healthRequest(ctx, c.url, c.apiKey)
	})
	if err != nil {
		return err
	}
	if !c.backend.healthOK(status, body) {
		return &Error{Kind: KindStatus, Backend: c.backend.name(), URL: c.url, Status: status}
	}
	return nil
}

// do runs a request with the retry policy, returning the body and the
// status for the caller to judge. Every failure is an *Error.
//
// Only a timeout is retried. A 401 will still be a 401 on the third try,
// and retrying it only triples the time to the message that says what is
// actually wrong. An authentication failure is answered here rather than
// by the caller because it means the same thing on every endpoint.
func (c *Client) do(ctx context.Context, build func(context.Context) (*http.Request, error)) ([]byte, int, error) {
	var last error

	for attempt := 0; attempt < c.attempts; attempt++ {
		if attempt > 0 {
			// Exponential: 500ms, then 1s. Abandoned if the caller gave
			// up while we were waiting.
			select {
			case <-ctx.Done():
				return nil, 0, c.classify(ctx.Err())
			case <-time.After(baseBackoff << (attempt - 1)):
			}
		}

		req, err := build(ctx)
		if err != nil {
			return nil, 0, &Error{Kind: KindUnreachable, Backend: c.backend.name(), URL: c.url, Err: err}
		}

		body, status, err := c.roundTrip(req)
		if err != nil {
			e := c.classify(err)
			if e.Kind != KindTimeout {
				return nil, 0, e
			}
			last = e
			continue
		}

		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return nil, status, &Error{Kind: KindAuth, Backend: c.backend.name(), URL: c.url, Status: status}
		}
		return body, status, nil
	}

	return nil, 0, last
}

func (c *Client) roundTrip(req *http.Request) ([]byte, int, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	// Capped: a misconfigured URL pointing at something that streams
	// would otherwise read until memory ran out.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// maxBodyBytes is generous for a result set of a few hundred rows and
// still far short of trouble.
const maxBodyBytes = 32 << 20
