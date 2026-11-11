package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lestex/torrnado/internal/engine"
)

// A torrent's id is its 40-character info hash, which is exactly right
// for a machine and exactly wrong for a person typing at a prompt. So
// everywhere a command takes an id it also takes whatever is enough to
// pick one torrent out of the list: the front of its id, or its name.
//
// Resolved here, in the client, against a List. Not in the daemon: that
// way it works against any daemon already running, including one that
// predates it, and the wire protocol - and the TUI, which always has a
// full id in hand - stays out of it.

// minIDPrefix is the shortest hex string read as the front of an id.
// Shorter than this matches too much of any real list to be a choice
// anyone meant, and four is where git stops accepting them too.
const minIDPrefix = 4

// idLen is the length of an id: a v1 info hash, in hex.
const idLen = 40

// maxCandidates caps how many torrents an ambiguity error lists. Enough
// to see what the argument matched, not a page of them.
const maxCandidates = 8

// torrentLister is the one thing resolution asks of the daemon. An
// interface so a test does not need one running.
type torrentLister interface {
	List() ([]engine.TorrentSnapshot, error)
}

// resolution is one argument's outcome, kept per argument because a
// command given several should still act on the ones that resolved.
type resolution struct {
	Arg  string           // as typed
	ID   engine.TorrentID // zero when Err is set
	Name string           // the torrent's name, when it was looked up
	Err  error
}

// announce says what an argument turned into, when it turned into
// something - a prefix or a name is only useful to type if it is clear
// what it picked, and `remove` in particular should never leave anyone
// wondering which torrent it just took. Silent for a full id, which
// resolved to itself and has nothing to explain.
//
// To the writer given - stderr, in practice - so a script reading stdout
// never sees it.
func (r resolution) announce(w io.Writer) {
	if r.Err != nil || string(r.ID) == strings.ToLower(r.Arg) {
		return
	}
	short := string(r.ID)
	if len(short) > 12 {
		short = short[:12]
	}
	fmt.Fprintf(w, "%s -> %s  %s\n", r.Arg, short, r.Name)
}

// resolveArgs resolves every argument against one listing.
//
// A full id is taken as given, without consulting the list at all: it is
// already exact, so a script passing one pays nothing and behaves as it
// always did, including when the id names nothing and the daemon is the
// one to say so.
func resolveArgs(l torrentLister, args []string) []resolution {
	out := make([]resolution, len(args))

	var (
		snaps   []engine.TorrentSnapshot
		listErr error
		listed  bool
	)
	for i, arg := range args {
		if isFullID(arg) {
			// Hex is case-insensitive, ids are stored lower-case.
			out[i] = resolution{Arg: arg, ID: engine.TorrentID(strings.ToLower(arg))}
			continue
		}
		if !listed {
			snaps, listErr = l.List()
			listed = true
		}
		if listErr != nil {
			out[i] = resolution{Arg: arg, Err: fmt.Errorf("cannot look up %q: %w", arg, listErr)}
			continue
		}
		id, name, err := resolveID(snaps, arg)
		out[i] = resolution{Arg: arg, ID: id, Name: name, Err: err}
	}
	return out
}

// resolveOne is for the commands that take a single torrent: it resolves,
// says what it picked, and turns a failure into an error to return.
func resolveOne(l torrentLister, arg string) (engine.TorrentID, error) {
	r := resolveArgs(l, []string{arg})[0]
	if r.Err != nil {
		return "", r.Err
	}
	r.announce(os.Stderr)
	return r.ID, nil
}

// resolveID picks the one torrent arg refers to, trying in order:
//
//  1. the front of an id, when arg is at least minIDPrefix hex characters
//  2. a name that is exactly arg
//  3. a name that contains arg
//
// The first step that matches anything decides, and it has to be exactly
// one torrent. Never a guess: two matches is an error that says what they
// were, because acting on the wrong torrent is the one failure that cannot
// be shrugged off, and `remove` takes arguments from the same place.
//
// An id is tried first so that "cafe" means the torrent whose hash starts
// that way even when some other torrent happens to be named for it - and
// it falls through to the names when no hash does, so a torrent really
// called "beef" is still reachable.
func resolveID(snaps []engine.TorrentSnapshot, arg string) (engine.TorrentID, string, error) {
	q := strings.TrimSpace(arg)
	if q == "" {
		return "", "", errors.New("an empty torrent name or id")
	}
	lq := strings.ToLower(q)

	steps := []func(engine.TorrentSnapshot) bool{
		func(s engine.TorrentSnapshot) bool {
			return strings.HasPrefix(strings.ToLower(string(s.ID)), lq)
		},
		func(s engine.TorrentSnapshot) bool { return strings.EqualFold(s.Name, q) },
		func(s engine.TorrentSnapshot) bool {
			return strings.Contains(strings.ToLower(s.Name), lq)
		},
	}
	for i, match := range steps {
		if i == 0 && !(len(q) >= minIDPrefix && isHex(q)) {
			continue
		}
		var hits []engine.TorrentSnapshot
		for _, s := range snaps {
			if match(s) {
				hits = append(hits, s)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0].ID, hits[0].Name, nil
		default:
			return "", "", &ambiguousError{arg: arg, hits: hits}
		}
	}
	return "", "", fmt.Errorf("no torrent matches %q", arg)
}

// ambiguousError is an argument that matched more than one torrent.
type ambiguousError struct {
	arg  string
	hits []engine.TorrentSnapshot
}

func (e *ambiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d torrents - name one of them:", e.arg, len(e.hits))
	for i, h := range e.hits {
		if i == maxCandidates {
			fmt.Fprintf(&b, "\n  ... and %d more", len(e.hits)-maxCandidates)
			break
		}
		name := h.Name
		if name == "" {
			name = "(no name yet)"
		}
		fmt.Fprintf(&b, "\n  %s  %s", h.ID, name)
	}
	return b.String()
}

func isFullID(s string) bool { return len(s) == idLen && isHex(s) }

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
