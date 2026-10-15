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
	// KindAuth is the aggregator refusing the API key.
	KindAuth
	// KindStatus is any other non-2xx reply.
	KindStatus
	// KindTimeout is every attempt running out of time.
	KindTimeout
	// KindDecode is a reply that arrived but could not be read.
	KindDecode
)

// Error is what every failure from this package is, so errors.As can
// recover the kind and the caller can report the aggregator and URL
// rather than a bare transport error naming neither.
//
// The message carries the remedy as well as the fault. Both the TUI and
// the CLI show these verbatim, and writing the advice once here is what
// keeps the two from drifting into different wording for the same
// misconfiguration.
type Error struct {
	Kind    Kind
	Backend string
	URL     string
	Status  int // KindAuth and KindStatus only
	Err     error
}

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Error() string {
	switch e.Kind {
	case KindUnreachable:
		reason := "connection refused"
		if !errors.Is(e.Err, syscall.ECONNREFUSED) {
			reason = e.Err.Error()
		}
		return fmt.Sprintf("%s: cannot reach %s (%s) - is it running, and is indexer.url right?",
			e.Backend, e.URL, reason)
	case KindAuth:
		return fmt.Sprintf("%s: %s rejected the API key (HTTP %d) - check indexer.api_key",
			e.Backend, e.URL, e.Status)
	case KindStatus:
		return fmt.Sprintf("%s: %s returned HTTP %d", e.Backend, e.URL, e.Status)
	case KindTimeout:
		return fmt.Sprintf("%s: no reply from %s: %v", e.Backend, e.URL, e.Err)
	case KindDecode:
		return fmt.Sprintf("%s: could not read the reply from %s: %v", e.Backend, e.URL, e.Err)
	}
	return fmt.Sprintf("%s: %s: %v", e.Backend, e.URL, e.Err)
}

// classify turns a transport error into an *Error.
//
// Timeout is asked of the error itself rather than compared against a
// deadline, because the two ways out are a per-attempt http.Client
// timeout (a net.Error that reports Timeout) and the caller's context
// expiring (context.DeadlineExceeded). Both mean "retry might help",
// which is the distinction the retry loop needs, and nothing else here
// does.
func (c *Client) classify(err error) *Error {
	e := &Error{Backend: c.backend.name(), URL: c.url, Err: err}

	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		e.Kind = KindTimeout
		return e
	}
	e.Kind = KindUnreachable
	return e
}
