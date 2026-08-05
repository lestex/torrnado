package batch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const magnetA = "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const magnetB = "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// write puts a file in dir and returns its path.
func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return path
}

func TestExpandMagnetPassesThrough(t *testing.T) {
	got, err := Expand([]string{magnetA})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 || got[0] != magnetA {
		t.Errorf("got %v, want just the magnet", got)
	}
}

func TestExpandTorrentFile(t *testing.T) {
	path := write(t, t.TempDir(), "x.torrent", "d4:infod4:name1:aee")

	got, err := Expand([]string{path})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 || got[0] != path {
		t.Errorf("got %v, want just the path", got)
	}
}

// A directory means the .torrent files directly inside it -- not a
// recursive walk, and not the other files that happen to be there.
func TestExpandDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.torrent", "d4:infod4:name1:aee")
	write(t, dir, "b.TORRENT", "d4:infod4:name1:bee") // extension match is case-insensitive
	write(t, dir, "notes.txt", "ignore me")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Expand([]string{dir})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want the two .torrent files", got)
	}
}

func TestExpandEmptyDirectoryIsAnError(t *testing.T) {
	if _, err := Expand([]string{t.TempDir()}); err == nil {
		t.Error("a directory with no .torrent files should be an error")
	}
}

func TestExpandGlob(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.torrent", "d4:infod4:name1:aee")
	write(t, dir, "b.torrent", "d4:infod4:name1:bee")
	write(t, dir, "c.other", "no")

	got, err := Expand([]string{filepath.Join(dir, "*.torrent")})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want two matches", got)
	}
}

// A glob that matches nothing is an error, not an empty result: silently
// adding no torrents looks identical to success.
func TestExpandGlobMatchingNothingIsAnError(t *testing.T) {
	pattern := filepath.Join(t.TempDir(), "*.torrent")
	if _, err := Expand([]string{pattern}); err == nil {
		t.Error("a glob matching nothing should be an error")
	}
}

func TestExpandMagnetListFile(t *testing.T) {
	body := strings.Join([]string{
		"# a comment",
		"",
		magnetA,
		"   " + magnetB + "   ", // surrounding space is trimmed
		"",
	}, "\n")
	path := write(t, t.TempDir(), "list.txt", body)

	got, err := Expand([]string{path})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 2 || got[0] != magnetA || got[1] != magnetB {
		t.Errorf("got %v, want both magnets in order", got)
	}
}

// A line that is not a magnet is a mistake worth reporting, with the line
// quoted so it can be found.
func TestExpandMagnetListRejectsJunkLines(t *testing.T) {
	path := write(t, t.TempDir(), "list.txt", magnetA+"\nnot a magnet\n")

	_, err := Expand([]string{path})
	if err == nil {
		t.Fatal("a non-magnet line should be an error")
	}
	if !strings.Contains(err.Error(), "not a magnet") {
		t.Errorf("error should quote the bad line, got: %v", err)
	}
}

func TestExpandUnknownArgumentIsAnError(t *testing.T) {
	if _, err := Expand([]string{"/definitely/not/here"}); err == nil {
		t.Error("a path that does not exist should be an error")
	}
}

// Several arguments flatten into one list, in the order given.
func TestExpandCombinesArguments(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.torrent", "d4:infod4:name1:aee")

	got, err := Expand([]string{magnetA, dir})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 2 || got[0] != magnetA {
		t.Errorf("got %v, want the magnet then the file", got)
	}
}
