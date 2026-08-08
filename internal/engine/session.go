package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Session persistence: what the daemon needs to come back as it was after
// a restart.
//
// The downloaded bytes and the piece-completion database already survive
// a restart on their own. What does not is the knowledge that a torrent
// exists at all - the engine's torrent map is built empty every start -
// along with everything the user chose about it: paused, save path, rate
// limits, per-file priorities. Without this file a box that reboots comes
// back idle, with the data still on disk and nothing downloading.
//
// Two things are written, under StateDir:
//
//	torrents/<infohash>.torrent   the metainfo, once it has arrived
//	session.json                  one record per torrent
//
// The metainfo is kept because re-adding from it is instant, where a
// magnet has to find a peer and fetch it again. The magnet URI is kept
// anyway, for torrents whose metadata never arrived before the restart.

// sessionVersion guards against reading a file written by a future
// version whose fields mean something else.
const sessionVersion = 1

// torrentRecord is one torrent's persisted state.
type torrentRecord struct {
	InfoHash string `json:"info_hash"`
	Name     string `json:"name,omitempty"`
	// Magnet is what the torrent was added from, when it was added from
	// one. It is the only way back for a torrent whose metadata never
	// arrived, since there is no metainfo file to re-add from.
	Magnet    string    `json:"magnet,omitempty"`
	SavePath  string    `json:"save_path"`
	Paused    bool      `json:"paused"`
	UpLimit   int64     `json:"upload_limit,omitempty"`
	DownLimit int64     `json:"download_limit,omitempty"`
	AddedAt   time.Time `json:"added_at"`
	// FilePriorities holds only the files that differ from normal, which
	// is nearly all of them nearly all of the time.
	FilePriorities []filePriorityRecord `json:"file_priorities,omitempty"`
}

type filePriorityRecord struct {
	Index    int    `json:"index"`
	Priority string `json:"priority"`
}

// sessionFile is the whole of session.json.
type sessionFile struct {
	Version  int             `json:"version"`
	SavedAt  time.Time       `json:"saved_at"`
	Torrents []torrentRecord `json:"torrents"`
}

func (e *Engine) sessionPath() string {
	return filepath.Join(e.cfg.StateDir, "session.json")
}

func (e *Engine) metainfoPath(id TorrentID) string {
	return filepath.Join(e.cfg.StateDir, "torrents", string(id)+".torrent")
}

// persist saves the session, reporting a failure to the log rather than
// to the caller.
//
// Every call site is an operation that has already succeeded: the torrent
// was added, the pause did happen. Failing those after the fact because
// the state directory is unwritable would be a worse answer than carrying
// on with a stale file and saying so.
func (e *Engine) persist() {
	e.mu.Lock()
	suppressed := e.restoring
	e.mu.Unlock()
	// A restore adds torrents through the same operations as anything
	// else, so without this it would rewrite the file once per torrent -
	// and a failure halfway through would leave a file listing only the
	// ones restored so far, quietly losing the rest. Saved once at the
	// end instead.
	if suppressed {
		return
	}
	if err := e.SaveSession(); err != nil {
		e.log.Error("saving the session failed", "err", err)
	}
}

// SaveSession writes the current torrent list to disk. A no-op when no
// state directory is configured, which is how an embedder (or a test)
// opts out of persistence entirely.
//
// Called after every operation that changes something rather than on a
// timer: those are rare - a handful a minute at worst - so rewriting
// the whole file each time costs less than tracking which parts changed,
// and leaves no window where the file is behind reality.
func (e *Engine) SaveSession() error {
	if e.cfg.StateDir == "" {
		return nil
	}

	e.mu.Lock()
	records := make([]torrentRecord, 0, len(e.torrents))
	for id, tr := range e.torrents {
		records = append(records, e.recordLocked(id, tr))
	}
	e.mu.Unlock()

	if err := os.MkdirAll(filepath.Join(e.cfg.StateDir, "torrents"), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	// Written outside the lock: this touches the disk, and holding the
	// engine lock through a write would stall every other operation.
	e.saveMetainfo()

	data, err := json.MarshalIndent(sessionFile{
		Version:  sessionVersion,
		SavedAt:  time.Now(),
		Torrents: records,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	return writeFileAtomic(e.sessionPath(), append(data, '\n'))
}

// recordLocked builds one torrent's record. Callers must hold e.mu.
func (e *Engine) recordLocked(id TorrentID, tr *tracked) torrentRecord {
	rec := torrentRecord{
		InfoHash:  string(id),
		Magnet:    tr.magnet,
		SavePath:  tr.savePath,
		Paused:    tr.paused,
		UpLimit:   tr.upLimit,
		DownLimit: tr.downLimit,
		AddedAt:   tr.addedAt,
	}
	// Both of these read through metadata that a magnet may not have yet.
	if tr.t.Info() != nil {
		rec.Name = tr.t.Name()
		for i, f := range tr.t.Files() {
			prio := fromLibPriority(f.Priority())
			if prio == PriorityNormal {
				continue
			}
			rec.FilePriorities = append(rec.FilePriorities,
				filePriorityRecord{Index: i, Priority: prio.String()})
		}
	}
	return rec
}

// saveMetainfo writes a .torrent for every torrent whose metadata has
// arrived and that does not have one on disk already. The metainfo of a
// given infohash never changes, so an existing file is never rewritten.
func (e *Engine) saveMetainfo() {
	e.mu.Lock()
	type pending struct {
		id TorrentID
		tr *tracked
	}
	var todo []pending
	for id, tr := range e.torrents {
		if tr.t.Info() != nil {
			todo = append(todo, pending{id, tr})
		}
	}
	e.mu.Unlock()

	for _, p := range todo {
		path := e.metainfoPath(p.id)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		mi := p.tr.t.Metainfo()
		f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
		if err != nil {
			e.log.Error("saving metainfo failed", "id", p.id, "err", err)
			continue
		}
		err = mi.Write(f)
		f.Close()
		if err != nil {
			os.Remove(f.Name())
			e.log.Error("saving metainfo failed", "id", p.id, "err", err)
			continue
		}
		if err := os.Rename(f.Name(), path); err != nil {
			os.Remove(f.Name())
			e.log.Error("saving metainfo failed", "id", p.id, "err", err)
		}
	}
}

// removeMetainfo drops a torrent's saved metainfo, so a removed torrent
// does not leave a file behind forever.
func (e *Engine) removeMetainfo(id TorrentID) {
	if e.cfg.StateDir == "" {
		return
	}
	if err := os.Remove(e.metainfoPath(id)); err != nil && !os.IsNotExist(err) {
		e.log.Error("removing saved metainfo failed", "id", id, "err", err)
	}
}

// RestoreSession re-adds the torrents saved by a previous run and returns
// how many came back.
//
// Nothing here is fatal. A session file that is missing, truncated,
// half-written or written by a newer version leaves the daemon starting
// with fewer torrents (or none) and a message saying so - a server that
// refuses to boot because one record is malformed is worse than one that
// comes back incomplete, and the operator can see the difference in the
// log either way.
func (e *Engine) RestoreSession() (int, error) {
	if e.cfg.StateDir == "" {
		return 0, nil
	}

	data, err := os.ReadFile(e.sessionPath())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // first run
		}
		return 0, fmt.Errorf("read session: %w", err)
	}

	var sf sessionFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return 0, fmt.Errorf("parse session %s: %w", e.sessionPath(), err)
	}
	if sf.Version > sessionVersion {
		return 0, fmt.Errorf("session %s was written by a newer version (%d)", e.sessionPath(), sf.Version)
	}

	e.mu.Lock()
	e.restoring = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.restoring = false
		e.mu.Unlock()
		e.persist()
	}()

	restored := 0
	for _, rec := range sf.Torrents {
		if err := e.restoreOne(rec); err != nil {
			e.log.Error("restoring a torrent failed", "id", rec.InfoHash, "name", rec.Name, "err", err)
			continue
		}
		restored++
	}
	return restored, nil
}

func (e *Engine) restoreOne(rec torrentRecord) error {
	opts := AddOpts{Paused: rec.Paused}
	// Only when it differs from the default: passing the default would
	// give this torrent its own storage instance for no reason.
	if rec.SavePath != "" && rec.SavePath != e.cfg.DataDir {
		opts.SavePath = rec.SavePath
	}

	var (
		id  TorrentID
		err error
	)
	switch {
	case fileExists(e.metainfoPath(TorrentID(rec.InfoHash))):
		// Preferred: no peer needs to be found before this torrent knows
		// what it is made of.
		id, err = e.AddTorrentFile(e.metainfoPath(TorrentID(rec.InfoHash)), opts)
	case rec.Magnet != "":
		id, err = e.AddMagnet(rec.Magnet, opts)
	default:
		return fmt.Errorf("no metainfo file and no magnet uri to re-add from")
	}
	if err != nil {
		return err
	}

	if rec.UpLimit != 0 || rec.DownLimit != 0 {
		if err := e.SetTorrentRateLimit(id, rec.UpLimit, rec.DownLimit); err != nil {
			return fmt.Errorf("restore rate limits: %w", err)
		}
	}
	for _, fp := range rec.FilePriorities {
		prio, ok := ParsePriority(fp.Priority)
		if !ok {
			e.log.Warn("skipping an unknown saved priority", "id", id, "file", fp.Index, "priority", fp.Priority)
			continue
		}
		// Best effort: the file list can have changed under us, and one
		// unsettable priority is not a reason to drop the torrent.
		if err := e.SetFilePriority(id, fp.Index, prio); err != nil {
			e.log.Warn("restoring a file priority failed", "id", id, "file", fp.Index, "err", err)
		}
	}

	// The record's own timestamp, so an "added" column does not reset to
	// the moment of the last restart.
	if !rec.AddedAt.IsZero() {
		e.mu.Lock()
		if tr, ok := e.torrents[id]; ok {
			tr.addedAt = rec.AddedAt
		}
		e.mu.Unlock()
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writeFileAtomic writes to a temporary file in the same directory and
// renames it over the target.
//
// A rename within one filesystem is atomic, so a reader sees either the
// old file or the new one. Writing in place would leave a half-written
// session behind if the machine lost power mid-write - exactly the
// moment this file has to be readable.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file next to %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("rename onto %s: %w", path, err)
	}
	return nil
}
