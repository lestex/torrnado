package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// completionDir builds a directory to complete against and returns it.
func completionDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p := filepath.Join(dir, n)
		if strings.HasSuffix(n, "/") {
			if err := os.MkdirAll(strings.TrimSuffix(p, "/"), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", n, err)
			}
			continue
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
	return dir
}

func TestCompletePath(t *testing.T) {
	dir := completionDir(t, "alpha.torrent", "alps.torrent", "beta.torrent", "shows/", ".hidden")

	for _, c := range []struct {
		name     string
		line     string
		want     string
		wantMany bool
	}{
		{
			name: "a unique file completes",
			line: "add " + dir + "/be",
			want: "add " + filepath.Join(dir, "beta.torrent"),
		},
		{
			// The separator is the point: it says "keep going" rather
			// than leaving the user to guess whether this is the end.
			name: "a directory gains a separator",
			line: "add " + dir + "/sh",
			want: "add " + filepath.Join(dir, "shows") + "/",
		},
		{
			name:     "several extend to their common prefix",
			line:     "add " + dir + "/al",
			want:     "add " + filepath.Join(dir, "alp"),
			wantMany: true,
		},
		{
			name: "no match leaves the line alone",
			line: "add " + dir + "/zzz",
			want: "add " + dir + "/zzz",
		},
		{
			// Tab on a bare directory lists it, the way a shell does.
			name:     "a bare directory lists its contents",
			line:     "add " + dir + "/",
			want:     "add " + dir + "/",
			wantMany: true,
		},
		{
			name: "and appear once the dot is typed",
			line: "add " + dir + "/.hi",
			want: "add " + filepath.Join(dir, ".hidden"),
		},
		{
			// A stray Tab mid-magnet must not mangle it.
			name: "a magnet is not a path",
			line: "add magnet:?xt=urn:btih:abc",
			want: "add magnet:?xt=urn:btih:abc",
		},
		{
			name: "a url is not a path",
			line: "add https://example.com/x.torrent",
			want: "add https://example.com/x.torrent",
		},
		{
			// Completing a torrent id or a theme name would be a
			// different feature with different rules.
			name: "commands that take no path are left alone",
			line: "theme dra",
			want: "theme dra",
		},
		{
			name: "the command itself is not completed",
			line: "ad",
			want: "ad",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, candidates := completePath(c.line)
			if got != c.want {
				t.Errorf("line = %q, want %q", got, c.want)
			}
			if (len(candidates) > 1) != c.wantMany {
				t.Errorf("candidates = %v, wantMany = %v", candidates, c.wantMany)
			}
		})
	}
}

// Otherwise every completion in a home directory is buried in
// configuration the user was not looking for.
func TestCompletionHidesDotfilesUntilAsked(t *testing.T) {
	dir := completionDir(t, "visible.torrent", ".hidden")

	_, candidates := completePath("add " + dir + "/")
	for _, c := range candidates {
		if strings.HasPrefix(c, ".") {
			t.Errorf("dotfile %q offered without being asked for", c)
		}
	}

	line, _ := completePath("add " + dir + "/.hi")
	if line != "add "+filepath.Join(dir, ".hidden") {
		t.Errorf("line = %q, want the dotfile completed once the dot is typed", line)
	}
}

// A completed name with a space in it has to come back quoted, or
// splitArgs would read it as two arguments and the add would fail on a
// path that was completed correctly.
func TestCompletionQuotesANameWithASpace(t *testing.T) {
	dir := completionDir(t, "Big Buck Bunny.torrent")

	line, _ := completePath("add " + dir + "/Big")
	args := splitArgs(line)
	if len(args) != 2 {
		t.Fatalf("completed line splits into %d args (%q), want 2", len(args), args)
	}
	if args[1] != filepath.Join(dir, "Big Buck Bunny.torrent") {
		t.Errorf("arg = %q, want the full path", args[1])
	}
}

// Something completed from inside quotes goes back inside them, rather
// than gaining a second pair.
func TestCompletionStaysInsideExistingQuotes(t *testing.T) {
	dir := completionDir(t, "one two.torrent")

	line, _ := completePath("add '" + dir + "/one")
	if strings.Count(line, "'") != 2 {
		t.Errorf("line = %q, want exactly one pair of quotes", line)
	}
	args := splitArgs(line)
	if len(args) != 2 || args[1] != filepath.Join(dir, "one two.torrent") {
		t.Errorf("args = %q, want the full path as one argument", args)
	}
}

// A path begun with ~ stays begun with ~: jumping to an absolute path
// halfway through typing is disorienting, and both forms work.
func TestCompletionKeepsATildePrefix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	line, _ := completePath("add ~/Down")
	if line != "add ~/Downloads/" {
		t.Errorf("line = %q, want %q", line, "add ~/Downloads/")
	}
}

// Tab completing a path and typing the same path by hand must reach the
// daemon as the same thing.
func TestTypedTildePathsAreExpanded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := expandPathArgs([]string{"~/x.torrent", "magnet:?xt=urn:btih:abc", "/abs/path"})
	if got[0] != filepath.Join(home, "x.torrent") {
		t.Errorf("tilde not expanded: %q", got[0])
	}
	if got[1] != "magnet:?xt=urn:btih:abc" {
		t.Errorf("magnet was touched: %q", got[1])
	}
	if got[2] != "/abs/path" {
		t.Errorf("absolute path was touched: %q", got[2])
	}
}

// The candidates go beside the cursor because in command mode the footer
// is the prompt - a status message has nowhere to appear.
func TestPromptShowsCandidatesBesideTheCursor(t *testing.T) {
	m := testModel()
	m.mode = modeCommand
	m.commandBuf = "add /tmp/al"
	m.completions = []string{"album.torrent", "alpha.torrent"}

	got := m.renderPrompt(":", m.commandBuf, 100, m.completions...)
	for _, want := range []string{"add /tmp/al", "album.torrent", "alpha.torrent"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt does not show %q:\n%s", want, got)
		}
	}
}

// The input is the thing that must never be squeezed off its own line, so
// a narrow footer drops the hint rather than the text being typed.
func TestNarrowPromptDropsTheHintNotTheInput(t *testing.T) {
	m := testModel()
	typed := "add /tmp/al"

	got := m.renderPrompt(":", typed, len(typed)+6, "album.torrent", "alpha.torrent")
	if strings.Contains(got, "album.torrent") {
		t.Errorf("hint drawn with no room for it:\n%s", got)
	}
	if !strings.Contains(got, typed) {
		t.Errorf("what was typed got squeezed out:\n%s", got)
	}
}

// A long list says how much it is not showing, rather than running off
// the end of the line.
func TestCandidateListIsTruncatedWithACount(t *testing.T) {
	many := make([]string, 20)
	for i := range many {
		many[i] = "file" + string(rune('a'+i)) + ".torrent"
	}
	got := candidateList(many)
	if !strings.Contains(got, "12 more") {
		t.Errorf("candidateList = %q, want it to say how many were left out", got)
	}
}
