package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/engine"
)

func TestCompletionOffersFullIDsDescribedByName(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("6a66e9f7", "debian"), snap("d134b832", "ubuntu")}

	got := completionCandidates(snaps, nil, "")

	want := []string{string(idWith("6a66e9f7")) + "\tdebian", string(idWith("d134b832")) + "\tubuntu"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCompletionFiltersByWhatIsTyped(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("6a66e9f7", "debian"), snap("d134b832", "ubuntu")}

	for _, typed := range []string{"6a", "6A66"} {
		got := completionCandidates(snaps, nil, typed)
		if len(got) != 1 || !strings.HasPrefix(got[0], "6a66e9f7") {
			t.Errorf("%q gave %q, want only the debian torrent", typed, got)
		}
	}
	if got := completionCandidates(snaps, nil, "ffff"); len(got) != 0 {
		t.Errorf("ffff gave %q, want nothing", got)
	}
}

// `pause <id> <TAB>` should not offer the torrent already named.
func TestCompletionLeavesOutWhatIsAlreadyGiven(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("6a66e9f7", "debian"), snap("d134b832", "ubuntu")}

	got := completionCandidates(snaps, []string{string(idWith("6a66e9f7"))}, "")

	if len(got) != 1 || !strings.HasPrefix(got[0], "d134b832") {
		t.Errorf("got %q, want only the torrent not yet named", got)
	}
}

// A shell reads "value<TAB>description" one line at a time, so a name with
// a tab or a newline in it would split a suggestion in two - and torrent
// names are whatever a stranger put in a .torrent file.
func TestCompletionDescriptionsStayOnOneLine(t *testing.T) {
	snaps := []engine.TorrentSnapshot{snap("6a66e9f7", "two\tparts\nand a break"), snap("d134b832", "")}

	got := completionCandidates(snaps, nil, "")

	if got[0] != string(idWith("6a66e9f7"))+"\ttwo parts and a break" {
		t.Errorf("first: %q", got[0])
	}
	if !strings.HasSuffix(got[1], "\t(no name yet)") {
		t.Errorf("an unnamed torrent: %q", got[1])
	}
	for _, c := range got {
		if strings.Count(c, "\t") != 1 || strings.ContainsAny(c, "\n\r") {
			t.Errorf("%q is not a single value-and-description line", c)
		}
	}
}

func TestACompletionDescriptionIsCapped(t *testing.T) {
	long := strings.Repeat("x", 200)

	got := describe(long)

	if n := len([]rune(got)); n != 80 || !strings.HasSuffix(got, "…") {
		t.Errorf("got %d runes %q, want 80 ending in an ellipsis", n, got)
	}
}

// Without this, a new command that takes a torrent would simply have no
// completion, and nothing would say so. It finds commands by the way they
// name their argument, and insists on finding a sensible number of them,
// so renaming the placeholder cannot quietly turn this into a test of
// nothing.
func TestEveryCommandThatTakesATorrentCompletesOne(t *testing.T) {
	var found int
	for _, c := range newRootCmd().Commands() {
		if !strings.Contains(c.Use, "<torrent-id>") && !strings.Contains(c.Use, "<id...>") {
			continue
		}
		found++
		if c.ValidArgsFunction == nil {
			t.Errorf("%q takes a torrent but offers no completion for it", c.Name())
		}
		if !strings.Contains(c.Long, "A torrent is named by") {
			t.Errorf("%q takes a torrent but its help does not say how to name one", c.Name())
		}
	}
	if found < 10 {
		t.Errorf("found only %d commands taking a torrent, want at least 10 - did the argument placeholder change?", found)
	}
}

// The flag that takes a torrent completes too.
func TestTheLimitTorrentFlagCompletes(t *testing.T) {
	var limit *cobra.Command
	for _, c := range newRootCmd().Commands() {
		if c.Name() == "limit" {
			limit = c
		}
	}
	if limit == nil {
		t.Fatal("no limit command")
	}
	fn, ok := limit.GetFlagCompletionFunc("torrent")
	if !ok || fn == nil {
		t.Error("--torrent has no completion function")
	}
}
