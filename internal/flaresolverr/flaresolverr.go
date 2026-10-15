// Package flaresolverr fetches pages through a FlareSolverr server.
//
// FlareSolverr drives a real browser. That matters for exactly one
// reason: Cloudflare's managed challenge cannot be passed by looking
// like a browser, only by being one. A Go client presenting Chrome's TLS
// fingerprint, header order and User-Agent is still served "Just a
// moment..." - the challenge runs JavaScript, and passing it means
// executing it.
//
// So this package asks FlareSolverr for a page and uses the HTML it
// returns. It deliberately does not take the cookies FlareSolverr earned
// and re-fetch the page itself, which is what the C# client Jackett uses
// does (FlareSolverrSharp's ClearanceHandler, which discards
// Solution.Response and refetches with .NET). That cannot work against a
// modern challenge: Cloudflare binds clearance to the client's TLS and
// HTTP/2 fingerprint, so replaying a valid cf_clearance from a different
// client - even with the same User-Agent, from the same address - is
// challenged again. Measured, not assumed.
//
// Stdlib only, and no internal dependencies.
package flaresolverr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout is how long FlareSolverr is allowed to spend on one
// page. Generous because that budget covers launching a browser, loading
// the page and waiting out a challenge, and the cost of being too mean
// is a failure that looks like the site being down.
const DefaultTimeout = 60 * time.Second

// clientSlack is how much longer we wait than FlareSolverr does, so a
// server working right up to its own deadline reports its own error
// rather than having the connection pulled from under it. Applied in
// send, so it holds whichever http.Client is in use.
const clientSlack = 15 * time.Second

// Cookie is one cookie, going in or coming out.
//
// Only the fields that mean anything to a caller: FlareSolverr also
// reports expiry, httpOnly, secure and sameSite, which matter to the
// browser holding them and not to us.
//
// Leave Domain empty when injecting one. The browser then binds the
// cookie to the page being fetched, which is almost always what is
// wanted, and a domain that does not match the URL fails the whole
// request - not the cookie, the request - with a browser stacktrace
// about a "Cookie 'domain' mismatch" that says nothing about which
// cookie. Setting it correctly works; setting it carelessly is a
// confusing way to lose a search. All three cases measured.
type Cookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain,omitempty"`
	Path   string `json:"path,omitempty"`
}

// Page is a fetched page.
type Page struct {
	// URL is where the browser ended up, after any redirect.
	URL string
	// Status is the page's own HTTP status, not FlareSolverr's.
	Status int
	// HTML is the document after the browser finished with it - script
	// having run, and any challenge having been passed.
	HTML string
	// Cookies is everything the browser holds for the site, including
	// the clearance it earned. Useful to inspect; useless to replay
	// from another client, which is the whole point of this package.
	Cookies []Cookie
	// UserAgent is the browser's, which a caller may need to match if
	// it ever talks to the same site another way.
	UserAgent string
}

// Cookie returns the named cookie's value, and whether it was there.
func (p *Page) Cookie(name string) (string, bool) {
	for _, c := range p.Cookies {
		if c.Name == name {
			return c.Value, true
		}
	}
	return "", false
}

// Options are the per-request knobs.
type Options struct {
	// Session reuses a browser kept alive by NewSession. Without one,
	// FlareSolverr starts and tears down a browser per request, which
	// costs seconds and loses any login the last one had.
	Session string
	// Cookies are injected into the browser before the page loads.
	//
	// This is how a site login is carried in: a session cookie from
	// your own browser is an ordinary application cookie, and unlike
	// Cloudflare clearance it transfers perfectly well. The two
	// problems separate - the browser earns the clearance itself, and
	// we supply the login.
	//
	// Leave each Domain empty unless you mean it; see Cookie.
	Cookies []Cookie
}

// Client talks to one FlareSolverr server.
type Client struct {
	url     string
	timeout time.Duration
	slack   time.Duration
	http    *http.Client
}

// Config configures a Client.
type Config struct {
	// URL is the server's base address, e.g. http://localhost:8191.
	URL string
	// Timeout is how long FlareSolverr may spend per page. 0 means
	// DefaultTimeout.
	Timeout time.Duration
	// HTTP overrides the transport, for tests.
	HTTP *http.Client
	// slack overrides clientSlack, so a test can make the whole bound
	// milliseconds rather than waiting out a quarter of a minute.
	slack time.Duration
}

// New builds a Client.
func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if base == "" {
		return nil, fmt.Errorf("flaresolverr: no url configured")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("flaresolverr: %q must be an http(s) URL, e.g. http://localhost:8191", cfg.URL)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	slack := cfg.slack
	if slack <= 0 {
		slack = clientSlack
	}
	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout + slack}
	}
	return &Client{url: base, timeout: timeout, slack: slack, http: httpClient}, nil
}

// URL is the configured server address, for error messages.
func (c *Client) URL() string { return c.url }

// Get fetches a page.
func (c *Client) Get(ctx context.Context, pageURL string, o Options) (*Page, error) {
	return c.do(ctx, command{
		Cmd:        "request.get",
		URL:        pageURL,
		Session:    o.Session,
		Cookies:    o.Cookies,
		MaxTimeout: c.timeout.Milliseconds(),
	})
}

// Post submits a form.
//
// FlareSolverr takes the body already encoded, and only
// application/x-www-form-urlencoded: it drives a browser, and that is
// what a browser submitting a form sends.
func (c *Client) Post(ctx context.Context, pageURL string, form url.Values, o Options) (*Page, error) {
	return c.do(ctx, command{
		Cmd:        "request.post",
		URL:        pageURL,
		PostData:   form.Encode(),
		Session:    o.Session,
		Cookies:    o.Cookies,
		MaxTimeout: c.timeout.Milliseconds(),
	})
}

// NewSession starts a browser that outlives one request and returns its
// id, which Options.Session reuses.
//
// Worth holding for a site that needs logging in: the login happens
// once and every later page load is already authenticated, rather than
// each one starting a fresh browser that knows nothing.
func (c *Client) NewSession(ctx context.Context) (string, error) {
	resp, err := c.send(ctx, command{Cmd: "sessions.create"})
	if err != nil {
		return "", err
	}
	if resp.Session == "" {
		return "", &Error{Kind: KindServer, URL: c.url, Msg: "created a session but got no id back"}
	}
	return resp.Session, nil
}

// DestroySession frees the browser a session was holding. Not doing so
// leaves a browser running until FlareSolverr restarts.
func (c *Client) DestroySession(ctx context.Context, session string) error {
	_, err := c.send(ctx, command{Cmd: "sessions.destroy", Session: session})
	return err
}

// command is the request body. One flat struct for every cmd, because
// the API is one endpoint that switches on Cmd.
type command struct {
	Cmd        string   `json:"cmd"`
	URL        string   `json:"url,omitempty"`
	Session    string   `json:"session,omitempty"`
	MaxTimeout int64    `json:"maxTimeout,omitempty"`
	Cookies    []Cookie `json:"cookies,omitempty"`
	PostData   string   `json:"postData,omitempty"`
}

// response is FlareSolverr's reply.
type response struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Session  string `json:"session"`
	Solution *struct {
		URL       string   `json:"url"`
		Status    int      `json:"status"`
		Response  string   `json:"response"`
		Cookies   []Cookie `json:"cookies"`
		UserAgent string   `json:"userAgent"`
	} `json:"solution"`
}

// do runs a page-fetching command and unpacks the solution.
func (c *Client) do(ctx context.Context, cmd command) (*Page, error) {
	resp, err := c.send(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if resp.Solution == nil {
		return nil, &Error{Kind: KindServer, URL: c.url, Msg: "no page came back: " + resp.Message}
	}
	return &Page{
		URL:       resp.Solution.URL,
		Status:    resp.Solution.Status,
		HTML:      resp.Solution.Response,
		Cookies:   resp.Solution.Cookies,
		UserAgent: resp.Solution.UserAgent,
	}, nil
}

// send posts one command and checks the reply.
func (c *Client) send(ctx context.Context, cmd command) (*response, error) {
	body, err := json.Marshal(cmd)
	if err != nil {
		return nil, &Error{Kind: KindBadReply, URL: c.url, Msg: "could not encode the request", Err: err}
	}

	// The deadline is set here rather than left to the http.Client,
	// because a caller may supply its own - to share a transport - and
	// a client with no Timeout would otherwise wait forever on a server
	// that accepts the connection and never answers. Longer than the
	// budget given to FlareSolverr, so a server working right up to its
	// own deadline gets to report its own error rather than having the
	// connection pulled from under it.
	wait := c.timeout + c.slack
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/v1", bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Kind: KindBadReply, URL: c.url, Msg: "could not build the request", Err: err}
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(req)
	if err != nil {
		return nil, c.classify(err, wait)
	}
	defer httpResp.Body.Close()

	var parsed response
	if err := json.NewDecoder(httpResp.Body).Decode(&parsed); err != nil {
		// Something answered but it was not FlareSolverr - a reverse
		// proxy's error page, or another service on that port.
		return nil, &Error{Kind: KindBadReply, URL: c.url, Status: httpResp.StatusCode,
			Msg: "could not read the reply", Err: err}
	}

	// FlareSolverr reports its own failures in the body with HTTP 500,
	// so the status alone does not say what went wrong - the message
	// does, and it is the one worth showing.
	if parsed.Status != "ok" {
		return nil, &Error{Kind: KindServer, URL: c.url, Status: httpResp.StatusCode, Msg: parsed.Message}
	}
	return &parsed, nil
}
