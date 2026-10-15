package indexer

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// RutrackerName is how the source names itself, in config, in find
// --check, and as the prefix of every token it issues.
const RutrackerName = "rutracker"

const (
	rutrackerBase   = "https://rutracker.org/forum/"
	rutrackerSearch = rutrackerBase + "tracker.php?nm="
	rutrackerTopic  = rutrackerBase + "viewtopic.php?t="
	rutrackerLogin  = rutrackerBase + "login.php"
	// sessionCookie is the login rutracker hands a browser.
	sessionCookie = "bb_session"
)

// Fetcher is the browser this source reads pages through - in practice
// *flaresolverr.Client, which drives a real Chrome.
//
// An interface, and declared here rather than imported, for two reasons.
// It keeps indexer a stdlib-only leaf, which is what lets ipc carry its
// types without pulling a transport into every client. And a site behind
// Cloudflare cannot be fetched by any Go HTTP client at all: clearance
// is bound to the TLS fingerprint of whoever earned it, so a page has to
// be read by something with a JavaScript engine. That makes the fetcher
// a collaborator the daemon supplies, not a detail this file owns.
type Fetcher interface {
	// Get fetches a page, injecting the named cookies, and returns the
	// HTML after script has run.
	Get(ctx context.Context, pageURL string, cookies map[string]string) (string, error)
	// Post submits a form the same way.
	Post(ctx context.Context, pageURL string, form url.Values, cookies map[string]string) (string, error)
}

// RutrackerOptions configures the source. Either Session alone, or
// Username and Password together.
type RutrackerOptions struct {
	// Session is a bb_session cookie copied from a browser already
	// logged in. It is the fallback for an account the automated login
	// cannot reach, and the only route when the login form answers with
	// a captcha.
	Session string
	// Username and Password log in through the browser on first use.
	Username string
	Password string

	// CacheTTL is how long a result set stays fresh; 0 means
	// DefaultCacheTTL, negative turns the cache off.
	CacheTTL time.Duration

	now func() time.Time // tests
}

// Rutracker searches rutracker.org, a site that needs a login and sits
// behind Cloudflare.
//
// It is deliberately one site rather than a framework. An aggregator
// plugin is ninety lines of JSON mapping; a site plugin is selectors, a
// login, session expiry and a browser - which is worth writing once, for
// a site chosen on purpose, and is not worth doing fifty times. That is
// Jackett's job, and this does not try to replace it.
//
// None of the selectors below come from Jackett: it is GPL-2.0 and this
// is MIT. They were written against the live page.
type Rutracker struct {
	fetch Fetcher
	opts  RutrackerOptions
	cache cache

	mu      sync.Mutex
	session string // the bb_session in use, once established
}

// NewRutracker builds the source. It does not log in: that waits for the
// first search, so a daemon start never blocks on a tracker.
func NewRutracker(f Fetcher, o RutrackerOptions) (*Rutracker, error) {
	if f == nil {
		return nil, fmt.Errorf("%s needs a flaresolverr server: set flaresolverr.url", RutrackerName)
	}
	if o.Session == "" && (o.Username == "" || o.Password == "") {
		return nil, fmt.Errorf("%s needs search.%s.session, or both search.%s.username and search.%s.password",
			RutrackerName, RutrackerName, RutrackerName, RutrackerName)
	}
	if o.now == nil {
		o.now = time.Now
	}
	return &Rutracker{
		fetch:   f,
		opts:    o,
		cache:   cache{entries: map[string]cacheEntry{}},
		session: o.Session,
	}, nil
}

func (r *Rutracker) Sources() []string { return []string{RutrackerName} }

// Search runs one query and parses the result table.
func (r *Rutracker) Search(ctx context.Context, query string) ([]Result, []string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil, fmt.Errorf("%s: search needs a query", RutrackerName)
	}
	ttl := r.opts.CacheTTL
	if ttl == 0 {
		ttl = DefaultCacheTTL
	}
	key := strings.ToLower(query)
	if hit, ok := r.cache.get(key, r.opts.now()); ok {
		return hit, nil, nil
	}

	doc, err := r.page(ctx, rutrackerSearch+url.QueryEscape(query))
	if err != nil {
		return nil, nil, err
	}
	results, err := parseRutracker(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", RutrackerName, err)
	}
	sortResults(results)
	r.cache.put(key, results, r.opts.now().Add(ttl), ttl)
	return results, nil, nil
}

// Resolve turns a topic id into a magnet by reading the topic page,
// which is the only place rutracker publishes one.
func (r *Rutracker) Resolve(ctx context.Context, id string) (string, error) {
	if _, err := strconv.Atoi(id); err != nil {
		return "", fmt.Errorf("%s: %q is not a topic id", RutrackerName, id)
	}
	doc, err := r.page(ctx, rutrackerTopic+id)
	if err != nil {
		return "", err
	}
	magnet := findMagnet(doc)
	if magnet == "" {
		return "", fmt.Errorf("%s: topic %s has no magnet link", RutrackerName, id)
	}
	return magnet, nil
}

// page fetches one page as the logged-in user, logging in first if there
// is no session yet, and once more if the session has expired.
func (r *Rutracker) page(ctx context.Context, pageURL string) (*html.Node, error) {
	body, err := r.fetchAs(ctx, pageURL)
	if err != nil {
		return nil, err
	}
	if loggedOut(body) {
		// A session that worked until now has expired. Logging in again
		// is only possible with a username and password; a pasted cookie
		// has to be replaced by hand, so say which key.
		if r.opts.Username == "" {
			return nil, fmt.Errorf("%s: the session cookie has expired - paste a fresh one into search.%s.session",
				RutrackerName, RutrackerName)
		}
		if err := r.login(ctx); err != nil {
			return nil, err
		}
		if body, err = r.fetchAs(ctx, pageURL); err != nil {
			return nil, err
		}
		if loggedOut(body) {
			return nil, fmt.Errorf("%s: logged in but the next page was still the login page", RutrackerName)
		}
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: parsing the page failed: %w", RutrackerName, err)
	}
	return doc, nil
}

func (r *Rutracker) fetchAs(ctx context.Context, pageURL string) (string, error) {
	r.mu.Lock()
	session := r.session
	r.mu.Unlock()

	if session == "" {
		if err := r.login(ctx); err != nil {
			return "", err
		}
		r.mu.Lock()
		session = r.session
		r.mu.Unlock()
	}
	body, err := r.fetch.Get(ctx, pageURL, map[string]string{sessionCookie: session})
	if err != nil {
		return "", fmt.Errorf("%s: %w", RutrackerName, err)
	}
	return body, nil
}

// login submits the form in the browser and keeps the cookie it gets.
func (r *Rutracker) login(ctx context.Context) error {
	if r.opts.Username == "" {
		return fmt.Errorf("%s: no session and no credentials - set search.%s.session or search.%s.username and .password",
			RutrackerName, RutrackerName, RutrackerName)
	}
	form := url.Values{
		"login_username": {r.opts.Username},
		"login_password": {r.opts.Password},
		"login":          {"вход"},
	}
	body, err := r.fetch.Post(ctx, rutrackerLogin, form, nil)
	if err != nil {
		return fmt.Errorf("%s: logging in failed: %w", RutrackerName, err)
	}
	if hasCaptcha(body) {
		// The captcha is bound to the page it was served on and cannot be
		// answered from here. A cookie from a browser that already
		// answered one is the way through, which is why that option
		// exists at all.
		return fmt.Errorf("%s: the login form is asking for a captcha, which this cannot answer - "+
			"log in with a browser and copy its %s cookie into search.%s.session",
			RutrackerName, sessionCookie, RutrackerName)
	}
	if loggedOut(body) {
		return fmt.Errorf("%s: the login was rejected - check search.%s.username and search.%s.password",
			RutrackerName, RutrackerName, RutrackerName)
	}
	return nil
}

// loggedOut reports whether a page is the login page rather than the one
// that was asked for.
//
// Both halves are needed. A logged-in page carries a login.php link too
// - that is the log-out link - so its presence alone says nothing. What
// settles it is the username the header shows when there is a session.
func loggedOut(body string) bool {
	return !strings.Contains(body, "logged-in-username")
}

func hasCaptcha(body string) bool {
	return strings.Contains(body, "cap_sid") || strings.Contains(body, "captcha")
}
