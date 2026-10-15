package flaresolverr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// Kind classifies why a request failed, because the fixes differ and the
// message has to point at the right one. Mirrors internal/indexer's
// classification, for the same reason and in the same shape.
type Kind int

const (
	// KindUnreachable is nothing answering at all.
	KindUnreachable Kind = iota
	// KindTimeout is a server that is there and took too long. A
	// different problem from being unreachable, and a different fix:
	// more time, not a corrected address.
	KindTimeout
	// KindCanceled is the caller giving up - a daemon shutting down
	// mid-search, usually. Nothing is wrong, so nothing is advised.
	KindCanceled
	// KindServer is FlareSolverr reporting its own failure.
	KindServer
	// KindBadReply is something answering that is not FlareSolverr.
	KindBadReply
)

// Error is every failure from this package.
//
// The message carries the remedy as well as the fault, because these
// reach a user through a search that simply did not work, and the
// message is the only place they will be told what to do about it.
type Error struct {
	Kind Kind
	// URL is the FlareSolverr server, not the page.
	URL string
	// Status is FlareSolverr's own HTTP status, when there was one.
	Status int
	// Msg is what went wrong, usually FlareSolverr's own words.
	Msg string
	// Timeout is how long was waited, for KindTimeout's message.
	Timeout time.Duration
	Err     error
}

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Error() string {
	switch e.Kind {
	case KindUnreachable:
		return fmt.Sprintf("flaresolverr: cannot reach %s (%v) - is it running, and is the url right?",
			e.URL, e.Err)

	case KindTimeout:
		// Deliberately not "is it running": it is, and saying otherwise
		// sends someone to restart a healthy server instead of giving
		// the challenge more time.
		return fmt.Sprintf("flaresolverr: no reply from %s after %s - a hard challenge can take "+
			"longer, try raising the timeout", e.URL, e.Timeout)

	case KindCanceled:
		return fmt.Sprintf("flaresolverr: %s: cancelled", e.URL)

	case KindServer:
		if e.Msg == "" {
			// Something went wrong and it would not say what. The
			// status is then the only fact left worth reporting, and
			// dropping it would leave a message with nothing in it.
			return fmt.Sprintf("flaresolverr: %s: failed without saying why (HTTP %d)", e.URL, e.Status)
		}
		return fmt.Sprintf("flaresolverr: %s: %s", e.URL, e.Msg)

	case KindBadReply:
		return fmt.Sprintf("flaresolverr: %s: %s (HTTP %d): %v", e.URL, e.Msg, e.Status, e.Err)
	}

	if e.Msg != "" {
		return fmt.Sprintf("flaresolverr: %s: %s: %v", e.URL, e.Msg, e.Err)
	}
	return fmt.Sprintf("flaresolverr: %s: %v", e.URL, e.Err)
}

// classify turns a transport failure into an *Error.
//
// A server being slow and a server being absent look the same to a
// caller that only sees "the search failed", so the distinction has to
// be drawn here. Timeout is asked of the error rather than compared
// against a clock, because the two ways out - our own deadline and the
// caller's context - report it differently.
func (c *Client) classify(err error, waited time.Duration) *Error {
	e := &Error{URL: c.url, Err: err, Timeout: waited}

	var netErr net.Error
	switch {
	case errors.Is(err, context.Canceled):
		e.Kind = KindCanceled
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		e.Kind = KindTimeout
	default:
		e.Kind = KindUnreachable
	}
	return e
}
