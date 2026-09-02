package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent"
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
	Magnet    string `json:"magnet,omitempty"`
	SavePath  string `json:"save_path"`
	Paused    bool   `json:"paused"`
	UpLimit   int64  `json:"upload_limit,omitempty"`
	DownLimit int64  `json:"download_limit,omitempty"`
	// Uploaded/Downloaded are the lifetime totals, so a ratio survives a
	// restart. Without them every restart would put a seeding torrent
	// back at zero.
	Uploaded   int64     `json:"uploaded,omitempty"`
	Downloaded int64     `json:"downloaded,omitempty"`
	AddedAt    time.Time `json:"added_at"`
	// SeedRatio/SeedTime are this torrent's own limits, overriding the
	// configured defaults; negative means "no limit for this one".
	SeedRatio float64       `json:"seed_ratio,omitempty"`
	SeedTime  time.Duration `json:"seed_time,omitempty"`
	// CompletedAt is when the torrent was first seen finished, which a
	// seeding-time limit counts from. Persisted so a restart does not
	// restart the clock on a torrent that finished days ago.
	CompletedAt time.Time `json:"completed_at,omitempty"`
	// Label is the category this torrent is filed under. omitempty
	// because most torrents have none, and a session file full of empty
	// strings is harder to read for no gain.
	Label string `json:"label,omitempty"`
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

	// Held across the whole of this, build and write both.
	//
	// The records are gathered under e.mu and written outside it, so two
	// callers could otherwise interleave: both snapshot, then the one
	// that snapshotted first writes last, and its stale records land on
	// top of the fresher ones. The file goes backwards, and stays that
	// way until the next save. Adding a torrent is enough to hit it - the
	// goroutine waiting for metadata saves too - and what is lost is
	// whatever changed in between.
	//
	// A separate mutex from e.mu because this holds through a disk write,
	// and engine operations must not queue behind that.
	e.saveMu.Lock()
	defer e.saveMu.Unlock()

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
		Label:     tr.label,
	}
	// The library's counters plus what earlier instances moved, which is
	// the same total snapshotLocked reports.
	st := tr.t.Stats()
	rec.Downloaded = tr.baseDownloaded + st.BytesReadUsefulData.Int64()
	rec.Uploaded = tr.baseUploaded + st.BytesWrittenData.Int64()
	rec.SeedRatio = tr.seedRatio
	rec.SeedTime = tr.seedTime
	rec.CompletedAt = tr.completedAt

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
	// The torrent itself is captured here, not the tracked it hangs off:
	// a move or a purge replaces tr.t, and reading that field outside the
	// lock below would race the write that replaces it.
	e.mu.Lock()
	type pending struct {
		id TorrentID
		t  *torrent.Torrent
	}
	var todo []pending
	for id, tr := range e.torrents {
		if tr.t.Info() != nil {
			todo = append(todo, pending{id, tr.t})
		}
	}
	e.mu.Unlock()

	for _, p := range todo {
		path := e.metainfoPath(p.id)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		mi := p.t.Metainfo()
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

	// The record's own timestamp and totals, so neither an "added" column
	// nor a ratio resets to the moment of the last restart.
	e.mu.Lock()
	if tr, ok := e.torrents[id]; ok {
		if !rec.AddedAt.IsZero() {
			tr.addedAt = rec.AddedAt
		}
		tr.baseDownloaded = rec.Downloaded
		tr.baseUploaded = rec.Uploaded
		tr.seedRatio = rec.SeedRatio
		tr.seedTime = rec.SeedTime
		tr.completedAt = rec.CompletedAt
		tr.label = rec.Label
		// Already finished in an earlier run, so it is not news now.
		tr.completeLogged = !rec.CompletedAt.IsZero()
	}
	e.mu.Unlock()
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
