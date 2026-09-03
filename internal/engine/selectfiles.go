package engine

import (
	"path"
	"strings"

	"github.com/anacrolix/torrent"
)

// matchesAnyPattern reports whether a file inside a torrent is picked out
// by any of the given glob patterns.
//
// A pattern containing a slash is matched against the file's whole path
// inside the torrent; one without is matched against its base name. That
// is what makes `*.mkv` mean what everybody expects: path.Match's `*`
// does not cross a separator, so matched against the full path it would
// pick out nothing in a torrent with any folder structure at all - which
// is most of them, and exactly the ones worth selecting from.
//
// A malformed pattern matches nothing rather than erroring here; the
// caller warns once, with the pattern, when a selection comes out empty.
func matchesAnyPattern(filePath string, patterns []string) bool {
	base := path.Base(filePath)
	for _, p := range patterns {
		target := base
		if strings.Contains(p, "/") {
			target = filePath
		}
		if ok, err := path.Match(p, target); err == nil && ok {
			return true
		}
	}
	return false
}

// applyFileSelection marks the files matching patterns as wanted and
// every other file as not wanted, once the metadata naming them exists.
//
// Every file is recorded as chosen, matched or not. That is what stops
// the goroutine that marks a torrent's files wanted from undoing this a
// moment later, and what makes the choice survive a restart: the session
// file carries the priorities of files that differ from normal.
//
// Returns how many files were selected.
func (e *Engine) applyFileSelection(id TorrentID, t *torrent.Torrent, patterns []string) int {
	files := filesOrNil(t)
	if files == nil {
		return 0
	}

	chosen := make(map[int]bool, len(files))
	selected := 0
	for i, f := range files {
		if matchesAnyPattern(f.Path(), patterns) {
			f.SetPriority(torrent.PiecePriorityNormal)
			selected++
		} else {
			f.SetPriority(torrent.PiecePriorityNone)
		}
		chosen[i] = true
	}

	e.mu.Lock()
	if tr, ok := e.torrents[id]; ok {
		tr.chosenFiles = chosen
	}
	e.mu.Unlock()

	return selected
}
