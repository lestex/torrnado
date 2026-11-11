package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/engine"
)

// idWith builds a full id that starts with prefix, so a test can say which
// torrents share a front without spelling out forty characters each time.
func idWith(prefix string) engine.TorrentID {
	return engine.TorrentID(prefix + strings.Repeat("0", idLen-len(prefix)))
}

func snap(prefix, name string) engine.TorrentSnapshot {
	return engine.TorrentSnapshot{ID: idWith(prefix), Name: name}
}

// fakeLister stands in for a daemon, and counts how often it is asked.
type fakeLister struct {
	snaps []engine.TorrentSnapshot
	err   error
	calls int
}

func (f *fakeLister) List() ([]engine.TorrentSnapshot, error) {
	f.calls++
	return f.snaps, f.err
}

// A full id is already exact. Looking it up anyway would cost a round trip
// per script invocation for nothing, and would make an id the daemon has
// never heard of fail here instead of where it always failed.
func TestAFullIDIsTakenAsGivenWithoutALookup(t *testing.T) {
	l := &fakeLister{}
	full := string(idWith("abcd1234"))

	got := resolveArgs(l, []string{full, strings.ToUpper(full)})

	if l.calls != 0 {
		t.Errorf("a full id asked the daemon for a list %d time(s)", l.calls)
	}
	for i, r := range got {
		if r.Err != nil || r.ID != engine.TorrentID(full) {
			t.Errorf("arg %d resolved to %q, %v; want %q (hex is case-insensitive, ids are lower-case)",
				i, r.ID, r.Err, full)
		}
	}
}

func TestAnIDPrefixPicksTheTorrent(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("6a66e9f7", "debian"), snap("d134b832", "ubuntu")}

	for _, arg := range []string{"6a66", "6A66E9F7", "6a66e9f7000"} {
		id, name, err := resolveID(snaps, arg)
		if err != nil || id != idWith("6a66e9f7") || name != "debian" {
			t.Errorf("%q resolved to %q %q, %v; want the debian torrent", arg, id, name, err)
		}
	}
}

func TestAnAmbiguousPrefixListsTheCandidates(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("ab120001", "Twin One"), snap("ab120002", "Twin Two"), snap("ff00", "Other")}

	_, _, err := resolveID(snaps, "ab12")

	var amb *ambiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("got %v, want an ambiguity error", err)
	}
	msg := err.Error()
	for _, want := range []string{"2 torrents", "Twin One", "Twin Two", string(snaps[0].ID), string(snaps[1].ID)} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not mention %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Other") {
		t.Errorf("the error lists a torrent that did not match:\n%s", msg)
	}
}

// Three characters of hex are as likely to be a word as a hash, and match
// too much of any real list to be a choice. Below the floor it is a name or
// nothing.
func TestTooShortAHexStringIsNotAnIDPrefix(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("abc00000", "Debian")}

	if _, _, err := resolveID(snaps, "abc"); err == nil {
		t.Fatal("three hex characters picked a torrent by its id")
	}

	snaps = append(snaps, snap("11112222", "Abcde"))
	id, _, err := resolveID(snaps, "abc")
	if err != nil || id != idWith("11112222") {
		t.Errorf("abc resolved to %q, %v; want the torrent named Abcde", id, err)
	}
}

// A torrent really called "beef" has to stay reachable even though beef is
// valid hex: the id step only decides when some hash starts that way.
func TestAHexLookingNameStillResolvesWhenNoIDHasThatPrefix(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("12345678", "beef"), snap("87654321", "other")}

	id, _, err := resolveID(snaps, "beef")
	if err != nil || id != idWith("12345678") {
		t.Errorf("beef resolved to %q, %v; want the torrent named beef", id, err)
	}
}

// And the other way round, which is why the id step goes first: when a
// hash does start that way, that is what was meant.
func TestAnIDBeatsAName(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("dead0001", "something"), snap("99990000", "deadline")}

	id, _, err := resolveID(snaps, "dead")
	if err != nil || id != idWith("dead0001") {
		t.Errorf("dead resolved to %q, %v; want the torrent whose id starts dead", id, err)
	}
}

func TestAnExactNameBeatsAPartialOne(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("1111", "ubuntu"), snap("2222", "ubuntu-server")}

	for arg, want := range map[string]engine.TorrentID{
		"ubuntu":        idWith("1111"),
		"UBUNTU":        idWith("1111"),
		"ubuntu-s":      idWith("2222"),
		"Ubuntu-Server": idWith("2222"),
	} {
		id, _, err := resolveID(snaps, arg)
		if err != nil || id != want {
			t.Errorf("%q resolved to %q, %v; want %q", arg, id, err, want)
		}
	}
}

// Two matches is a question, not a coin toss. This is the case that would
// have `remove ubuntu` take the wrong one of two.
func TestAnAmbiguousNameIsNeverGuessed(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("1111", "ubuntu-desktop"), snap("2222", "ubuntu-server")}

	_, _, err := resolveID(snaps, "ubun")

	var amb *ambiguousError
	if !errors.As(err, &amb) || len(amb.hits) != 2 {
		t.Fatalf("got %v, want an ambiguity error naming both", err)
	}
}

func TestNoMatchSaysSo(t *testing.T) {
	_, _, err := resolveID([]engine.TorrentSnapshot{snap("1111", "debian")}, "fedora")
	if err == nil || !strings.Contains(err.Error(), `"fedora"`) {
		t.Errorf("got %v, want an error naming what was not found", err)
	}
}

func TestAnEmptyArgumentIsRefused(t *testing.T) {
	// Substring-matching "" would match every torrent, which is exactly
	// the wrong thing for a command that may be about to remove one.
	for _, arg := range []string{"", "   "} {
		if _, _, err := resolveID([]engine.TorrentSnapshot{snap("1111", "x")}, arg); err == nil {
			t.Errorf("%q resolved to a torrent", arg)
		}
	}
}

func TestAnAmbiguityErrorIsCapped(t *testing.T) {
	var snaps []engine.TorrentSnapshot
	for i := 0; i < maxCandidates+4; i++ {
		snaps = append(snaps, snap(fmt.Sprintf("%04x", i+0x1000), "same"))
	}

	_, _, err := resolveID(snaps, "same")

	if err == nil || !strings.Contains(err.Error(), "and 4 more") {
		t.Errorf("got %v, want the overflow summarised as 4 more", err)
	}
	if got := strings.Count(err.Error(), "\n  "); got != maxCandidates+1 {
		t.Errorf("the error has %d detail lines, want %d candidates plus the summary", got, maxCandidates)
	}
}

func TestATorrentWithNoNameYetIsStillListed(t *testing.T) {
	// A magnet with no metadata has no name until a peer supplies one.
	snaps := []engine.TorrentSnapshot{snap("ab120001", ""), snap("ab120002", "")}

	_, _, err := resolveID(snaps, "ab12")
	if err == nil || !strings.Contains(err.Error(), "(no name yet)") {
		t.Errorf("got %v, want unnamed torrents shown as such", err)
	}
}

// One listing serves every argument: a command given ten names does not
// ask the daemon ten times.
func TestOneListingServesEveryArgument(t *testing.T) {
	l := &fakeLister{snaps: []engine.TorrentSnapshot{snap("1111", "a one"), snap("2222", "b two"), snap("3333", "c three")}}

	got := resolveArgs(l, []string{"a one", "b two", "c three"})

	if l.calls != 1 {
		t.Errorf("asked the daemon for a list %d times, want 1", l.calls)
	}
	for i, r := range got {
		if r.Err != nil {
			t.Errorf("arg %d: %v", i, r.Err)
		}
	}
}

// A command given several ids should still act on the ones that resolved:
// one bad argument is reported, not fatal to the rest.
func TestOneBadArgumentDoesNotSpoilTheOthers(t *testing.T) {
	l := &fakeLister{snaps: []engine.TorrentSnapshot{snap("1111", "debian")}}

	got := resolveArgs(l, []string{"debian", "nothing-like-it", string(idWith("2222"))})

	if got[0].Err != nil || got[0].ID != idWith("1111") {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Err == nil {
		t.Errorf("second resolved: %+v", got[1])
	}
	if got[2].Err != nil || got[2].ID != idWith("2222") {
		t.Errorf("third: %+v", got[2])
	}
}

// If the daemon cannot be asked, a name cannot be resolved - but a full
// id never needed asking, so it still goes through.
func TestWhenTheListFailsAFullIDStillGoes(t *testing.T) {
	l := &fakeLister{err: errors.New("daemon went away")}

	got := resolveArgs(l, []string{"debian", string(idWith("2222"))})

	if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), "daemon went away") {
		t.Errorf("the name should fail with the cause, got %v", got[0].Err)
	}
	if got[1].Err != nil || got[1].ID != idWith("2222") {
		t.Errorf("the full id should not have needed the list: %+v", got[1])
	}
}

func TestAnnounceSaysWhatWasPicked(t *testing.T) {
	var b strings.Builder
	resolution{Arg: "ubuntu", ID: idWith("d134b832ac06"), Name: "ubuntu-24.04.iso"}.announce(&b)

	want := "ubuntu -> d134b832ac06  ubuntu-24.04.iso\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

// A full id resolved to itself, so there is nothing to explain, and a
// script that passes them should see no extra output at all.
func TestAnnounceIsSilentForAFullIDAndForAFailure(t *testing.T) {
	var b strings.Builder
	id := idWith("d134b832ac06")
	resolution{Arg: string(id), ID: id}.announce(&b)
	resolution{Arg: strings.ToUpper(string(id)), ID: id}.announce(&b)
	resolution{Arg: "nope", Err: errors.New("no match")}.announce(&b)

	if b.Len() != 0 {
		t.Errorf("announced %q, want nothing", b.String())
	}
}
