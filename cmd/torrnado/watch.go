package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The watch folder: a directory the daemon adds .torrent files from as
// they appear.
//
// This is the input half of running headless. Everything else about a
// box nobody logs in to was already there - the daemon survives the
// terminal, systemd restarts it, on_complete runs afterwards - but the
// only ways to get a torrent *in* were `torrnado add` over SSH and the
// TUI. With a watched directory, anything that can write a file can add
// a torrent: rsync, a synced download folder, a file manager on a share,
// a cron job.

const (
	// watchInterval is how often the directory is read.
	//
	// Polling rather than fsnotify: it is one ReadDir of a directory that
	// is nearly always empty, which costs nothing next to what the
	// torrent client is doing, and it avoids a dependency and the
	// platform-specific corners that come with watching a filesystem over
	// NFS or a Samba share - which is exactly where a watch folder tends
	// to live.
	watchInterval = 2 * time.Second

	// addedSuffix marks a file the daemon has taken, and failedSuffix one
	// it could not.
	//
	// Renamed rather than deleted. The .torrent dropped in may be the only
	// copy, and deleting someone's file is not a thing to do by default.
	// The new name is also what stops it being added twice: the marker is
	// the record, so nothing has to be remembered across a restart.
	addedSuffix  = ".added"
	failedSuffix = ".failed"
)

// watcher adds .torrent files appearing in a directory.
type watcher struct {
	dir string
	// add is the engine call, injected so the scan can be tested without
	// a torrent client.
	add func(path string) error
	log *slog.Logger

	// sizes is what each candidate measured on the previous scan, which
	// is how a half-written file is told from a finished one.
	sizes map[string]int64
}

func newWatcher(dir string, add func(string) error, log *slog.Logger) *watcher {
	return &watcher{dir: dir, add: add, log: log, sizes: map[string]int64{}}
}

// run scans until done is closed.
func (w *watcher) run(done <-chan struct{}) {
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			w.scan()
		}
	}
}

// scan reads the directory once and adds whatever has settled.
//
// A file is only taken when its size is unchanged since the previous
// scan. A .torrent arriving over a network share is written in pieces,
// and one read halfway through is not a corrupt torrent so much as half
// of a good one - it would fail to parse, get marked failed, and the file
// that was about to be perfectly good would need renaming back by hand.
// Waiting one interval costs nothing and removes the whole class.
func (w *watcher) scan() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		// Logged at debug: a watch directory on a share that is not
		// mounted yet is a normal state at boot, and a line a second
		// about it would drown the log.
		w.log.Debug("watch directory could not be read", "dir", w.dir, "err", err)
		return
	}

	seen := make(map[string]int64, len(entries))
	for _, ent := range entries {
		// The same rule `torrnado add <dir>` uses: .torrent files
		// directly inside, never a recursive walk. The suffixes this
		// leaves behind are excluded by it for free, since ".added" is
		// not ".torrent".
		if ent.IsDir() || !strings.EqualFold(filepath.Ext(ent.Name()), ".torrent") {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue // vanished between the read and the stat
		}
		path := filepath.Join(w.dir, ent.Name())
		size := info.Size()
		seen[path] = size

		previous, measured := w.sizes[path]
		if !measured || previous != size {
			continue // new, or still being written
		}
		w.take(path)
		delete(seen, path)
	}
	// Rebuilt each scan so a file that was taken or removed stops being
	// remembered.
	w.sizes = seen
}

// take adds one file and renames it out of the way.
//
// The rename happens whichever way the add went. A file left in place
// would be retried every couple of seconds forever, which for a torrent
// that will never parse means the same error in the log until someone
// notices.
func (w *watcher) take(path string) {
	if err := w.add(path); err != nil {
		w.log.Error("a watched .torrent could not be added",
			"path", path, "err", err)
		w.rename(path, failedSuffix)
		return
	}
	w.log.Info("added from the watch directory", "path", path)
	w.rename(path, addedSuffix)
}

func (w *watcher) rename(path, suffix string) {
	if err := os.Rename(path, path+suffix); err != nil {
		// Worth saying loudly: without the rename the file is picked up
		// again on the next scan, and the daemon would add it forever.
		w.log.Error("could not mark a watched file, it may be added again",
			"path", path, "err", err)
	}
}
