package flaresolverr

import "fmt"

// Error is every failure from this package, so a caller can tell a
// server that is not running from one that could not fetch a page.
//
// The distinction matters because they need different fixes, and the
// messages say which: one is "start it or correct the address", the
// other is about the site being fetched.
type Error struct {
	// URL is the FlareSolverr server, not the page.
	URL string
	// Unreachable is set when nothing answered at all.
	Unreachable bool
	// Status is FlareSolverr's own HTTP status, when there was one.
	Status int
	// Msg is what went wrong, usually FlareSolverr's own words.
	Msg string
	Err error
}

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Error() string {
	switch {
	case e.Unreachable:
		return fmt.Sprintf("flaresolverr: cannot reach %s (%v) - is it running, and is the url right?",
			e.URL, e.Err)
	case e.Msg != "" && e.Err != nil:
		return fmt.Sprintf("flaresolverr: %s: %s: %v", e.URL, e.Msg, e.Err)
	case e.Msg != "":
		return fmt.Sprintf("flaresolverr: %s: %s", e.URL, e.Msg)
	}
	return fmt.Sprintf("flaresolverr: %s: %v", e.URL, e.Err)
}
