package indexer

import (
	"context"
	"strings"
)

// A source whose search page does not carry a magnet issues a token
// instead of a link: "rutracker:6916386".
//
// rutracker's search page has no magnet and no infohash anywhere on it.
// It offers a .torrent download that needs the login cookie and arrives
// as binary - which cannot come back through FlareSolverr, since that
// returns the page as text - and a link to the topic page, which does
// carry exactly one magnet. Resolving a result therefore costs a page
// load, about two seconds, and a search returns fifty of them: resolving
// every row eagerly would turn one search into two minutes of requests
// against the tracker for results the user will mostly not pick.
//
// So the token travels in Link, the daemon resolves it at add time, and
// `torrnado find --link 1 | torrnado add` keeps working the way it is
// documented - the token is addable, it just has to reach the daemon
// first, which is where the session lives.
const tokenSep = ":"

// Token builds the Link for a result that has to be resolved later.
func Token(source, id string) string { return source + tokenSep + id }

// ParseToken splits a token into the source that issued it and the id
// that source understands.
//
// The shape is checked strictly, because this runs against every source
// handed to the add path: a magnet URI and an http(s) URL both contain a
// colon, and neither must ever be mistaken for a token. An id of "//x"
// or "?xt=urn:btih:..." fails the character test, so "http://..." and
// "magnet:?..." are rejected on their own terms rather than by a list of
// schemes to exclude, which would need an entry for every scheme anyone
// adds later.
//
// Whether the source exists is the caller's question, not this
// function's: only the daemon knows what it has configured.
func ParseToken(s string) (source, id string, ok bool) {
	name, rest, found := strings.Cut(s, tokenSep)
	if !found || name == "" || rest == "" {
		return "", "", false
	}
	if len(name) > 32 || len(rest) > 128 {
		return "", "", false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return "", "", false
		}
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		alnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !alnum && c != '.' && c != '_' && c != '-' {
			return "", "", false
		}
	}
	return name, rest, true
}

// Resolver is a Searcher that issues tokens and can turn one back into
// something addable. A source whose results carry real links does not
// implement it, and the daemon reports a token nobody claims rather than
// handing the engine a string it cannot parse.
type Resolver interface {
	// Resolve turns one of this source's own tokens into a magnet URI.
	// The id is the token's second half, already split.
	Resolve(ctx context.Context, id string) (string, error)
}
