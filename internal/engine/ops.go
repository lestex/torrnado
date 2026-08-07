package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
)

// ErrNotFound is returned for an id no torrent answers to.
var ErrNotFound = fmt.Errorf("torrent not found")

// filesOrNil returns t.Files(), or nil if t's metadata hasn't arrived yet.
//
// This guard is not optional. (*torrent.Torrent).Files() requires Info()
// first, and calling it earlier does not return an error or an empty
// slice -- it dereferences a nil pointer and takes down the whole
// process, every torrent in the daemon along with it. A magnet can sit
// without metadata for an unbounded time, so every call site goes
// through here.
func filesOrNil(t *torrent.Torrent) []*torrent.File {
	if t.Info() == nil {
		return nil
	}
	return t.Files()
}

// AddMagnet adds a torrent from a magnet URI.
func (e *Engine) AddMagnet(uri string, opts AddOpts) (TorrentID, error) {
	spec, err := torrent.TorrentSpecFromMagnetUri(uri)
	if err != nil {
		return "", fmt.Errorf("parse magnet uri: %w", err)
	}
	return e.addSpec(spec, opts)
}

// AddTorrentFile adds a torrent from a local .torrent file.
func (e *Engine) AddTorrentFile(path string, opts AddOpts) (TorrentID, error) {
	mi, err := metainfo.LoadFromFile(path)
	if err != nil {
		return "", fmt.Errorf("load torrent file %s: %w", path, err)
	}
	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		return "", fmt.Errorf("parse torrent file %s: %w", path, err)
	}
	return e.addSpec(spec, opts)
}

func (e *Engine) addSpec(spec *torrent.TorrentSpec, opts AddOpts) (TorrentID, error) {
	// The client has a single default storage for everything, so a
	// torrent that wants its own download directory gets its own storage
	// instance -- which then has to be closed when it is removed.
	var ownStorage storage.ClientImplCloser
	savePath := e.cfg.DataDir
	if opts.SavePath != "" {
		if err := os.MkdirAll(opts.SavePath, 0o755); err != nil {
			return "", fmt.Errorf("create save dir %s: %w", opts.SavePath, err)
		}
		ownStorage = storage.NewFile(opts.SavePath)
		spec.Storage = ownStorage
		savePath = opts.SavePath
	}

	t, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		if ownStorage != nil {
			ownStorage.Close()
		}
		return "", fmt.Errorf("add torrent: %w", err)
	}
	id := TorrentID(t.InfoHash().HexString())

	e.mu.Lock()
	if _, exists := e.torrents[id]; exists {
		// Same infohash as one already tracked: the client hands back the
		// torrent it already has, so discard the storage just created.
		if ownStorage != nil {
			ownStorage.Close()
		}
	} else {
		e.torrents[id] = &tracked{
			t:          t,
			addedAt:    time.Now(),
			paused:     opts.Paused,
			savePath:   savePath,
			ownStorage: ownStorage,
		}
	}
	e.mu.Unlock()

	// A magnet carries no file list -- that metadata has to be fetched
	// from a peer first, and for a torrent with no peers it may never
	// arrive. So wait for it in the background rather than making the
	// caller wait an unbounded time.
	go func() {
		select {
		case <-t.GotInfo():
		case <-e.closeCh:
			return
		}
		if !opts.Paused {
			downloadAllFiles(t)
		}
		e.snapshotAndBroadcastNow()
	}()

	e.snapshotAndBroadcastNow()
	return id, nil
}

// downloadAllFiles marks every file in t as wanted. No-op until metadata
// has arrived.
//
// Nothing is fetched by default: a freshly added torrent sits at zero
// forever unless something says it wants the data.
//
// Deliberately not Torrent.DownloadAll(), which sets piece priorities
// directly and leaves each File's own priority untouched. File.Priority()
// only ever reports what was last passed to File.SetPriority(), so a
// torrent started that way downloads correctly while reporting every file
// as "none" forever.
func downloadAllFiles(t *torrent.Torrent) {
	for _, f := range filesOrNil(t) {
		f.SetPriority(torrent.PiecePriorityNormal)
	}
}

func (e *Engine) lookup(id TorrentID) (*tracked, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	tr, ok := e.torrents[id]
	if !ok {
		return nil, ErrNotFound
	}
	return tr, nil
}

// RemoveTorrent drops a torrent from the client. If deleteData is true,
// its downloaded files are removed from disk.
func (e *Engine) RemoveTorrent(id TorrentID, deleteData bool) error {
	e.mu.Lock()
	tr, ok := e.torrents[id]
	if !ok {
		e.mu.Unlock()
		return ErrNotFound
	}
	delete(e.torrents, id)
	e.mu.Unlock()

	// Work out the paths before dropping the torrent: afterwards the file
	// list is gone.
	files := filesOrNil(tr.t)
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, filepath.Join(tr.savePath, f.Path()))
	}

	tr.t.Drop()
	if tr.ownStorage != nil {
		tr.ownStorage.Close()
	}

	if deleteData {
		for _, p := range paths {
			os.Remove(p)
		}
		removeEmptyDirs(tr.savePath, paths)
	}

	e.snapshotAndBroadcastNow()
	return nil
}

// removeEmptyDirs removes the directories a multi-file torrent left
// behind, stopping at savePath itself -- that is usually a shared
// downloads directory and is not ours to delete.
func removeEmptyDirs(savePath string, filePaths []string) {
	seen := map[string]bool{}
	for _, p := range filePaths {
		dir := filepath.Dir(p)
		for dir != savePath && dir != "." && dir != string(filepath.Separator) && !seen[dir] {
			seen[dir] = true
			if os.Remove(dir) != nil {
				break // not empty, or already gone; stop walking up
			}
			dir = filepath.Dir(dir)
		}
	}
}

// SetPaused pauses or resumes a torrent.
//
// The library has no pause. What it has is a pair of switches for whether
// a torrent may request or serve piece data, so pausing means turning
// both off. Peer connections stay up, which is why resuming is instant
// rather than starting the discovery work again from nothing.
func (e *Engine) SetPaused(id TorrentID, paused bool) error {
	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	tr.paused = paused
	e.mu.Unlock()

	if paused {
		tr.t.DisallowDataDownload()
		tr.t.DisallowDataUpload()
	} else {
		tr.t.AllowDataDownload()
		tr.t.AllowDataUpload()
		// A torrent added paused never had its files marked wanted, and
		// would otherwise sit at zero forever after being resumed. Skip
		// it if any file already has a priority, so resuming does not
		// discard choices made while it was paused.
		if tr.t.Info() != nil && allFilesUnset(tr.t) {
			downloadAllFiles(tr.t)
		}
	}

	e.snapshotAndBroadcastNow()
	return nil
}

// allFilesUnset reports whether no file has been given a priority yet.
// Vacuously true before metadata arrives.
func allFilesUnset(t *torrent.Torrent) bool {
	for _, f := range filesOrNil(t) {
		if f.Priority() != torrent.PiecePriorityNone {
			return false
		}
	}
	return true
}

// SetFilePriority sets how badly one file's data is wanted.
//
// The library has no per-file priority of its own: a file's priority is
// really the priority of every piece the file spans, which is what
// SetPriority sets.
func (e *Engine) SetFilePriority(id TorrentID, fileIndex int, prio Priority) error {
	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	files := filesOrNil(tr.t)
	if files == nil {
		return fmt.Errorf("torrent metadata not available yet")
	}
	if fileIndex < 0 || fileIndex >= len(files) {
		return fmt.Errorf("file index %d out of range (0..%d)", fileIndex, len(files)-1)
	}
	files[fileIndex].SetPriority(toLibPriority(prio))
	e.snapshotAndBroadcastNow()
	return nil
}

// toLibPriority maps a Priority onto the library's scale, which runs
// None < Normal < High < Readahead < Next < Now.
//
// There is nothing between "not wanted" and "wanted normally", so
// PriorityLow -- which in most clients means "wanted, but after
// everything else" -- has no faithful equivalent and becomes Normal
// rather than being silently dropped to None.
func toLibPriority(p Priority) torrent.PiecePriority {
	switch p {
	case PriorityNone:
		return torrent.PiecePriorityNone
	case PriorityLow, PriorityNormal:
		return torrent.PiecePriorityNormal
	case PriorityHigh:
		return torrent.PiecePriorityHigh
	case PriorityNow:
		return torrent.PiecePriorityNow
	default:
		return torrent.PiecePriorityNormal
	}
}

// fromLibPriority maps a library piece priority back to a Priority.
//
// Now has to be distinguished from High: it can be set, so folding the
// two together would make a UI that cycles through priorities appear to
// stick at "high". Low is the one level that cannot survive the round
// trip, for the reason above.
func fromLibPriority(p torrent.PiecePriority) Priority {
	switch {
	case p == torrent.PiecePriorityNone:
		return PriorityNone
	case p >= torrent.PiecePriorityNow:
		return PriorityNow
	case p >= torrent.PiecePriorityHigh:
		return PriorityHigh
	default:
		return PriorityNormal
	}
}

// SetGlobalUploadLimit caps upload speed across every torrent, in
// bytes/sec. Zero means unlimited. Enforced exactly, by the library.
func (e *Engine) SetGlobalUploadLimit(bps int64) {
	if bps <= 0 {
		e.upLimiter.SetLimit(rate.Inf)
		return
	}
	e.upLimiter.SetLimit(rate.Limit(bps))
}

// SetGlobalDownloadLimit caps download speed across every torrent.
func (e *Engine) SetGlobalDownloadLimit(bps int64) {
	if bps <= 0 {
		e.downLimiter.SetLimit(rate.Inf)
		return
	}
	e.downLimiter.SetLimit(rate.Limit(bps))
}

// SetTorrentRateLimit caps one torrent's speed, approximately.
//
// Unlike the global limits this is not enforced by the library, which has
// no per-torrent throttle at all. It is approximated a tick at a time --
// see enforceRateLimit -- so it averages out near the cap but is bursty.
func (e *Engine) SetTorrentRateLimit(id TorrentID, uploadBps, downloadBps int64) error {
	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	tr.upLimit = uploadBps
	tr.downLimit = downloadBps
	e.mu.Unlock()

	e.snapshotAndBroadcastNow()
	return nil
}

// ForceRecheck re-verifies a torrent's on-disk data against its piece
// hashes.
func (e *Engine) ForceRecheck(id TorrentID) error {
	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	tr.checking = true
	e.mu.Unlock()
	e.snapshotAndBroadcastNow()

	go func() {
		err := tr.t.VerifyDataContext(context.Background())
		e.mu.Lock()
		tr.checking = false
		if err != nil {
			tr.lastErr = fmt.Sprintf("recheck failed: %v", err)
		}
		e.mu.Unlock()
		e.snapshotAndBroadcastNow()
	}()
	return nil
}

// MoveStorage relocates a torrent's downloaded data to a new directory.
// anacrolix/torrent has no live "move" API for the default file storage
// backend, so this pauses the torrent, moves the files on disk, points
// the torrent at a new storage.NewFile(newDir) instance, and re-verifies
// data in the background (a cheap hash check, not a re-download).
func (e *Engine) MoveStorage(id TorrentID, newDir string) error {
	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	if tr.t.Info() == nil {
		return fmt.Errorf("torrent metadata not available yet")
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", newDir, err)
	}

	wasPaused := tr.paused
	tr.t.DisallowDataDownload()
	tr.t.DisallowDataUpload()

	oldPath := tr.savePath
	for _, f := range tr.t.Files() {
		src := filepath.Join(oldPath, f.Path())
		dst := filepath.Join(newDir, f.Path())
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(dst), err)
		}
		if err := moveFile(src, dst); err != nil {
			return fmt.Errorf("move %s: %w", f.Path(), err)
		}
	}

	ih := tr.t.InfoHash()
	mi := tr.t.Metainfo()
	oldStorage := tr.ownStorage

	newStorage := storage.NewFile(newDir)
	spec, err := torrent.TorrentSpecFromMetaInfoErr(&mi)
	if err != nil {
		return fmt.Errorf("rebuild spec: %w", err)
	}
	spec.InfoHash = ih
	spec.Storage = newStorage

	tr.t.Drop()
	if oldStorage != nil {
		oldStorage.Close()
	}

	t, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		return fmt.Errorf("re-add torrent at new location: %w", err)
	}
	// The re-added Torrent is a fresh instance with every file back at
	// unset priority, so it needs the same downloadAllFiles() call
	// addSpec relies on -- which means a move also resets any custom
	// per-file priorities that had been set before it back to "wanted at
	// normal priority" rather than preserving them.
	if !wasPaused {
		downloadAllFiles(t)
	}

	e.mu.Lock()
	tr.t = t
	tr.savePath = newDir
	tr.ownStorage = newStorage
	tr.checking = true
	e.mu.Unlock()

	go func() {
		verifyErr := t.VerifyDataContext(context.Background())
		e.mu.Lock()
		tr.checking = false
		if verifyErr != nil {
			tr.lastErr = fmt.Sprintf("verify after move failed: %v", verifyErr)
		}
		if !wasPaused {
			t.AllowDataDownload()
			t.AllowDataUpload()
		}
		e.mu.Unlock()
		e.snapshotAndBroadcastNow()
	}()

	e.snapshotAndBroadcastNow()
	return nil
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// os.Rename fails across filesystems/devices; fall back to copy+remove.
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // file hasn't been downloaded yet, nothing to move
		}
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return os.Remove(src)
}

// ListTorrents returns a snapshot of every tracked torrent.
func (e *Engine) ListTorrents() []TorrentSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]TorrentSnapshot, 0, len(e.torrents))
	for id, tr := range e.torrents {
		out = append(out, e.snapshotLocked(id, tr))
	}
	return out
}

// TorrentDetail returns the files, peers and trackers for one torrent,
// alongside its snapshot.
func (e *Engine) TorrentDetail(id TorrentID) (TorrentDetail, error) {
	tr, err := e.lookup(id)
	if err != nil {
		return TorrentDetail{}, err
	}

	e.mu.Lock()
	snap := e.snapshotLocked(id, tr)
	e.mu.Unlock()

	// Everything below reads through the client's own locks, so the
	// engine lock is released first -- holding both invites a deadlock
	// and would block every other caller meanwhile.

	var files []FileInfo
	for i, f := range filesOrNil(tr.t) {
		files = append(files, FileInfo{
			Index:     i,
			Path:      f.Path(),
			Length:    f.Length(),
			Completed: f.BytesCompleted(),
			Priority:  fromLibPriority(f.Priority()),
		})
	}

	// NumPieces has the same requirement as Files: it reads through
	// metadata that may not be there, and crashes rather than failing.
	var numPieces int
	if tr.t.Info() != nil {
		numPieces = tr.t.NumPieces()
	}

	var peers []PeerInfo
	for _, pc := range tr.t.PeerConns() {
		stats := pc.Peer.Stats()
		var progress float64
		if numPieces > 0 {
			progress = float64(stats.RemotePieceCount) / float64(numPieces)
		}
		peers = append(peers, PeerInfo{
			Addr:        fmt.Sprint(pc.RemoteAddr),
			Client:      clientName(pc.PeerClientName.Load()),
			Source:      string(pc.Discovery),
			DownloadBPS: stats.DownloadRate,
			Progress:    progress,
			Encrypted:   pc.PeerPrefersEncryption,
		})
	}

	// Only the static list from the torrent's own metadata. The library
	// exposes no live announce status -- no last announce time, no error,
	// no seeder counts from the tracker's reply.
	var trackers []TrackerInfo
	mi := tr.t.Metainfo()
	for tier, urls := range mi.AnnounceList {
		for _, u := range urls {
			trackers = append(trackers, TrackerInfo{URL: u, Tier: tier})
		}
	}
	if len(trackers) == 0 && mi.Announce != "" {
		trackers = append(trackers, TrackerInfo{URL: mi.Announce, Tier: 0})
	}

	return TorrentDetail{Snapshot: snap, Files: files, Peers: peers, Trackers: trackers}, nil
}

// clientName resolves a peer's self-reported name.
//
// PeerClientName is an atomic.Value holding nothing until the peer's
// extended handshake supplies a name, so printing it directly renders the
// literal string "<nil>" as the client name.
func clientName(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
