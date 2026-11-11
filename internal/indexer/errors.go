package indexer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
)

// Kind classifies why a search failed, so a caller can react to the
// category without matching on message text.
type Kind int

const (
	// KindUnreachable is a transport failure: nothing answered at all.
	KindUnreachable Kind = iota
	// KindAuth is Jackett refusing the API key.
	KindAuth
	// KindStatus is any other non-2xx reply.
	KindStatus
	// KindTimeout is the search running out of time.
	KindTimeout
	// KindDecode is a reply that arrived but could not be read.
	KindDecode
)

// Error is what every failure from this package is, so errors.As can
// recover the kind. The message carries the remedy as well as the fault:
// the TUI and the CLI show it verbatim, and writing the advice once here
// keeps them from drifting.
type Error struct {
	Kind   Kind
	URL    string
	Status int // KindAuth and KindStatus only
	Err    error
}

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Error() string {
	switch e.Kind {
	case KindUnreachable:
		reason := "connection refused"
		if !errors.Is(e.Err, syscall.ECONNREFUSED) {
			reason = e.Err.Error()
		}
		return fmt.Sprintf("jackett: cannot reach %s (%s) - is it running, and is search.jackett.url right?",
			e.URL, reason)
	case KindAuth:
		return fmt.Sprintf("jackett: %s rejected the API key (HTTP %d) - check search.jackett.api_key",
			e.URL, e.Status)
	case KindStatus:
		return fmt.Sprintf("jackett: %s returned HTTP %d", e.URL, e.Status)
	case KindTimeout:
		return fmt.Sprintf("jackett: no reply from %s in time: %v", e.URL, e.Err)
	case KindDecode:
		return fmt.Sprintf("jackett: could not read the reply from %s: %v", e.URL, e.Err)
	}
	return fmt.Sprintf("jackett: %s: %v", e.URL, e.Err)
}

// classify turns a transport error into an *Error. Timeout is asked of
// the error itself because the two ways out are the http.Client timeout (a
// net.Error that reports Timeout) and the caller's context expiring.
func (c *Client) classify(err error) *Error {
	e := &Error{URL: c.url, Err: err}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		e.Kind = KindTimeout
		return e
	}
	e.Kind = KindUnreachable
	return e
}
