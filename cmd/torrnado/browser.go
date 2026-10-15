package main

import (
	"context"
	"net/url"

	"github.com/lestex/torrnado/internal/flaresolverr"
	"github.com/lestex/torrnado/internal/indexer"
)

// browser adapts a FlareSolverr client to the fetcher a site plugin
// asks for.
//
// The adapter lives here, in the one package that imports everything,
// because neither side may import the other: internal/indexer is a
// stdlib-only leaf so that internal/ipc can carry its types without
// dragging a transport into every client, and internal/flaresolverr is
// one for the same reason. The daemon is where they meet.
//
// It also owns the browser session. FlareSolverr without one starts and
// tears down a Chrome per request, which costs seconds and loses the
// login that the last one had - and a search here is already a page
// load through a challenge, so paying that twice is the difference
// between two seconds and twenty.
type browser struct {
	fs      *flaresolverr.Client
	session string
}

// newBrowser creates the long-lived browser session. A failure to create
// one is not fatal: requests still work without a session, just slower,
// and refusing to start the daemon over it would be worse.
func newBrowser(ctx context.Context, fs *flaresolverr.Client) *browser {
	b := &browser{fs: fs}
	if s, err := fs.NewSession(ctx); err == nil {
		b.session = s
	}
	return b
}

// close releases the browser, so a daemon shutdown does not leave a
// Chrome running inside FlareSolverr until it times out.
func (b *browser) close(ctx context.Context) {
	if b.session != "" {
		_ = b.fs.DestroySession(ctx, b.session)
		b.session = ""
	}
}

func (b *browser) options(cookies map[string]string) flaresolverr.Options {
	o := flaresolverr.Options{Session: b.session}
	for name, value := range cookies {
		// Domain deliberately left empty: one that does not match the
		// page fails the whole request rather than just the cookie.
		o.Cookies = append(o.Cookies, flaresolverr.Cookie{Name: name, Value: value})
	}
	return o
}

func (b *browser) Get(ctx context.Context, pageURL string, cookies map[string]string) (string, error) {
	page, err := b.fs.Get(ctx, pageURL, b.options(cookies))
	if err != nil {
		return "", err
	}
	return page.HTML, nil
}

func (b *browser) Post(ctx context.Context, pageURL string, form url.Values, cookies map[string]string) (string, error) {
	page, err := b.fs.Post(ctx, pageURL, form, b.options(cookies))
	if err != nil {
		return "", err
	}
	return page.HTML, nil
}

// compile-time check that the adapter is what the plugin asked for.
var _ indexer.Fetcher = (*browser)(nil)
