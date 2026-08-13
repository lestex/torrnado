package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
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
// slice - it dereferences a nil pointer and takes down the whole
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
	return e.addSpec(spec, opts, uri)
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
	// No magnet URI: a torrent added from a file is re-added from the
	// copy of its metainfo the session keeps.
	return e.addSpec(spec, opts, "")
}

// addSpec adds a torrent from an already-parsed spec. magnetURI is the
// URI it came from, or empty for a .torrent file.
func (e *Engine) addSpec(spec *torrent.TorrentSpec, opts AddOpts, magnetURI string) (TorrentID, error) {
	// The client has a single default storage for everything, so a
	// torrent that wants its own download directory gets its own storage
	// instance - which then has to be closed when it is removed.
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
			magnet:     magnetURI,
			paused:     opts.Paused,
			savePath:   savePath,
			ownStorage: ownStorage,
		}
	}
	// Before anything can be transferred: a torrent added while the VPN
	// guard is holding, or added paused, must not get the second or so
	// until the next tick as a head start. A library torrent is allowed
	// both directions the moment it is added.
	e.torrents[id].applyDataFlow(e.blocked)
	e.mu.Unlock()

	// A magnet carries no file list - that metadata has to be fetched
	// from a peer first, and for a torrent with no peers it may never
	// arrive. So wait for it in the background rather than making the
	// caller wait an unbounded time.
	go func() {
		select {
		case <-t.GotInfo():
		case <-e.closeCh:
			return
		}
		e.log.Info("metadata received", "id", id, "name", t.Name(), "size", t.Length())
		// Every file except the ones already spoken for. For a .torrent
		// the metadata is here before this goroutine is scheduled, so a
		// priority set right after the add - by a script, or by the
		// session restore - lands in the window this would otherwise
		// overwrite.
		if !opts.Paused {
			downloadUnchosenFiles(t, e.chosenFilesOf(id))
		}
		// Now there is a metainfo to save, which is what makes the next
		// restart able to re-add this torrent without finding a peer.
		e.persist()
		e.snapshotAndBroadcastNow()
	}()

	e.log.Info("torrent added", "id", id, "name", t.Name(), "save_path", savePath, "paused", opts.Paused)
	e.persist()

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
	downloadUnchosenFiles(t, nil)
}

// downloadUnchosenFiles marks every file wanted except the ones whose
// index is in chosen - the files something has already decided about.
//
// Without that exemption, a priority set between an add and the goroutine
// that marks its files wanted is silently overwritten, and so is one
// restored from the session file.
func downloadUnchosenFiles(t *torrent.Torrent, chosen map[int]bool) {
	for i, f := range filesOrNil(t) {
		if chosen[i] {
			continue
		}
		f.SetPriority(torrent.PiecePriorityNormal)
	}
}

// chosenFilesOf copies a torrent's chosen set, for use outside the lock.
func (e *Engine) chosenFilesOf(id TorrentID) map[int]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	tr, ok := e.torrents[id]
	if !ok || len(tr.chosenFiles) == 0 {
		return nil
	}
	out := make(map[int]bool, len(tr.chosenFiles))
	for i := range tr.chosenFiles {
		out[i] = true
	}
	return out
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
	// Before the drop below, so a verification in flight stops rather
	// than running on against a torrent that is being taken apart.
	cancelCheckLocked(tr)
	delete(e.torrents, id)
	e.mu.Unlock()

	// Work out the paths before dropping the torrent: afterwards the file
	// list is gone.
	paths := dataPaths(tr.savePath, tr.t)

	tr.t.Drop()
	if tr.ownStorage != nil {
		tr.ownStorage.Close()
	}

	if deleteData {
		deleteFiles(paths)
		removeEmptyDirs(tr.savePath, paths)
	}
	e.log.Info("torrent removed", "id", id, "deleted_data", deleteData)
	e.removeMetainfo(id)
	e.persist()

	e.snapshotAndBroadcastNow()
	return nil
}

// dataPaths lists every place on disk a torrent's data can be: each
// file, and the ".part" the file storage writes an unfinished one to.
//
// Both, because which of the two exists depends on whether every piece of
// that file has landed and been verified - and the whole point of
// listing them is to delete them, where missing the .part leaves the
// bytes behind. A half-downloaded torrent is *entirely* .part files.
//
// Empty before metadata arrives: there is no file list to build one from,
// and nothing has been written either.
func dataPaths(savePath string, t *torrent.Torrent) []string {
	files := filesOrNil(t)
	paths := make([]string, 0, len(files)*2)
	for _, f := range files {
		p := filepath.Join(savePath, f.Path())
		paths = append(paths, p, p+partFileSuffix)
	}
	return paths
}

// dataDir is the directory holding a torrent's files - what to open in
// a file manager when someone asks to see it on disk.
//
// Two shapes: a multi-file torrent puts its files in a directory of its
// own name under the save path, a single-file one writes straight into
// the save path. The difference is visible in the file list, where a
// multi-file torrent's paths carry that directory as their first element,
// so the answer is read from there rather than from the torrent's name -
// which is not always what the directory ended up being called.
//
// The save path before metadata arrives, which is where the data will go
// and is a directory that already exists.
func dataDir(savePath string, t *torrent.Torrent) string {
	files := filesOrNil(t)
	if len(files) == 0 {
		return savePath
	}
	// The library documents Path() as the components joined by "/", on
	// every platform, so this is not filepath's separator to split on.
	// The first element is the directory the torrent owns; filepath.Dir
	// of a deeper path would land inside it.
	first, _, nested := strings.Cut(files[0].Path(), "/")
	if !nested {
		return savePath // written straight into the save path
	}
	return filepath.Join(savePath, first)
}

// partFileSuffix is what anacrolix/torrent's file storage appends to a
// file it has not finished. Not exported by the library (storage's
// fileExtra.partFilePath), so it is spelled out here; if it ever changes,
// deletions start missing incomplete data.
const partFileSuffix = ".part"

// deleteFiles removes each path, ignoring the ones that are not there -
// dataPaths lists both a file and its .part, and only one of the two
// exists at a time.
func deleteFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// removeEmptyDirs removes the directories a multi-file torrent left
// behind, stopping at savePath itself - that is usually a shared
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
	// Pausing stops a running hash check too. It is the only way to call
	// one off: a recheck on a large torrent runs for hours, and "stop
	// what you are doing" is what pausing already means - so this needs
	// no command of its own, and works from every client that can pause.
	if paused {
		cancelCheckLocked(tr)
	}
	// Resuming records the intent and asks for the switches to be turned;
	// whether they actually turn is applyDataFlow's decision, since the
	// VPN guard may be holding everything. The intent is what is
	// persisted either way, so a resume during a VPN outage takes effect
	// the moment the VPN returns rather than being forgotten.
	tr.applyDataFlow(e.blocked)
	e.mu.Unlock()

	if !paused {
		// A torrent added paused never had its files marked wanted, and
		// would otherwise sit at zero forever after being resumed. The
		// files chosen while it was paused are left as they were: they
		// read as PiecePriorityNone, exactly like a file nobody has
		// touched, so only the engine's own record can tell them apart.
		downloadUnchosenFiles(tr.t, e.chosenFilesOf(id))
	}

	e.persist()
	e.snapshotAndBroadcastNow()
	return nil
}

// applyFilePriorities puts each file back to the priority at the same
// index, for a torrent that has just been re-added against new storage.
//
// Short lists are tolerated rather than treated as an error: the caller
// is mid-move with the data already relocated, and a file count that has
// somehow changed is not worth failing that over.
func applyFilePriorities(t *torrent.Torrent, prios []Priority) {
	for i, f := range filesOrNil(t) {
		if i >= len(prios) {
			return
		}
		f.SetPriority(toLibPriority(prios[i]))
	}
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
	// Recorded so nothing marks this file wanted again behind the
	// caller's back - see chosenFiles.
	e.mu.Lock()
	if tr.chosenFiles == nil {
		tr.chosenFiles = map[int]bool{}
	}
	tr.chosenFiles[fileIndex] = true
	e.mu.Unlock()

	e.persist()
	e.snapshotAndBroadcastNow()
	return nil
}

// toLibPriority maps a Priority onto the library's scale, which runs
// None < Normal < High < Readahead < Next < Now.
//
// There is nothing between "not wanted" and "wanted normally", so
// PriorityLow - which in most clients means "wanted, but after
// everything else" - has no faithful equivalent and becomes Normal
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

// lookupFile resolves a torrent id + file index to the library's File,
// enforcing the metadata guard that every Files() call site needs (see
// filesOrNil).
func (e *Engine) lookupFile(id TorrentID, fileIndex int) (*tracked, *torrent.File, error) {
	tr, err := e.lookup(id)
	if err != nil {
		return nil, nil, err
	}
	files := filesOrNil(tr.t)
	if files == nil {
		return nil, nil, fmt.Errorf("torrent metadata not available yet")
	}
	if fileIndex < 0 || fileIndex >= len(files) {
		return nil, nil, fmt.Errorf("file index %d out of range (0..%d)", fileIndex, len(files)-1)
	}
	return tr, files[fileIndex], nil
}

// OpenFile returns a reader over one file's data, for streaming it while
// it is still downloading.
//
// The returned reader is the only sound way to read a torrent's file
// before it completes. Reading the path on disk is not equivalent: the
// storage backend writes to "<path>.part" until every piece of the file
// is present, and that file is sparse and filled out of order, so a
// reader of it sees zeros where pieces haven't landed. This reader blocks
// instead, and the act of reading is what tells the client which pieces
// to fetch first - the piece at the read head is raised to "now" and the
// readahead window behind it to "readahead" priority.
//
// The reader is not safe for concurrent use and holds a single read head:
// callers wanting to serve overlapping ranges must open one each. Callers
// must Close it, and should SetContext so a blocked read can be
// canceled.
func (e *Engine) OpenFile(id TorrentID, fileIndex int) (torrent.Reader, FileInfo, error) {
	_, f, err := e.lookupFile(id, fileIndex)
	if err != nil {
		return nil, FileInfo{}, err
	}
	info := FileInfo{
		Index:     fileIndex,
		Path:      f.Path(),
		Length:    f.Length(),
		Completed: f.BytesCompleted(),
		Priority:  fromLibPriority(f.Priority()),
	}
	return f.NewReader(), info, nil
}

// PrepareStream makes a file streamable: it resumes the torrent if it is
// paused and raises the file's priority if it is below normal.
//
// Both are required rather than courtesies. A read on a paused torrent
// does not block waiting for a resume - it fails immediately, because
// pausing sets DisallowDataDownload and the library treats that as "this
// data is never coming". A file left at priority none is likewise never
// requested from peers.
func (e *Engine) PrepareStream(id TorrentID, fileIndex int) error {
	tr, f, err := e.lookupFile(id, fileIndex)
	if err != nil {
		return err
	}

	e.mu.Lock()
	paused := tr.paused
	e.mu.Unlock()
	if paused {
		if err := e.SetPaused(id, false); err != nil {
			return fmt.Errorf("resume for streaming: %w", err)
		}
	}

	if fromLibPriority(f.Priority()) < PriorityHigh {
		if err := e.SetFilePriority(id, fileIndex, PriorityHigh); err != nil {
			return fmt.Errorf("raise priority for streaming: %w", err)
		}
	}
	return nil
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
// Zero means unlimited and a negative value means "leave this direction
// as it is", so one direction can be changed without having to know, and
// resend, the other.
//
// Unlike the global limits this is not enforced by the library, which has
// no per-torrent throttle at all. It is approximated a tick at a time -
// see enforceRateLimit - so it averages out near the cap but is bursty.
func (e *Engine) SetTorrentRateLimit(id TorrentID, uploadBps, downloadBps int64) error {
	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	if uploadBps >= 0 {
		tr.upLimit = uploadBps
	}
	if downloadBps >= 0 {
		tr.downLimit = downloadBps
	}
	e.mu.Unlock()

	e.persist()
	e.snapshotAndBroadcastNow()
	return nil
}

// verifyPieces hashes every piece of tr, publishing how far it has got as
// it goes.
//
// One piece at a time rather than through the library's whole-torrent
// VerifyData, which is the only way the progress can be seen: that call
// reports nothing at all until it returns, and on a large torrent that is
// a very long silence in front of a user wondering whether anything is
// happening.
//
// The caller sets tr.checking and tr.checkTotal before starting, and
// clears them afterwards - this only moves tr.checkDone.
// verifyPieces hashes each piece in turn, stopping early if ctx is
// cancelled.
//
// The context stops this loop queueing work; it does not reach into the
// library's own hashing. VerifyDataContext queues a piece and then waits,
// and only the wait honours the context - so a cancel gives up on the
// piece in flight and, more importantly, never asks for the remaining
// ones. That is where the cost is: verification is quadratic in the piece
// count against the completion database, so not starting the rest is the
// whole saving.
func (e *Engine) verifyPieces(ctx context.Context, tr *tracked, total int) error {
	for i := 0; i < total; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tr.t.Piece(i).VerifyDataContext(ctx); err != nil {
			return err
		}
		e.mu.Lock()
		tr.checkDone = i + 1
		e.mu.Unlock()
	}
	return nil
}

// beginCheck marks a torrent as checking and returns the context its
// verification runs under, plus the function to call when it ends.
//
// Any check already running on that torrent is cancelled first: two
// verification loops on one torrent would fight over checkDone and take
// twice as long to get nowhere.
func (e *Engine) beginCheck(tr *tracked, total int) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())

	e.mu.Lock()
	if tr.cancelCheck != nil {
		tr.cancelCheck()
	}
	tr.cancelCheck = cancel
	tr.checking = true
	tr.checkDone = 0
	tr.checkTotal = total
	e.mu.Unlock()

	return ctx, func() {
		cancel()
		e.mu.Lock()
		tr.cancelCheck = nil
		tr.checking = false
		tr.checkDone, tr.checkTotal = 0, 0
		e.mu.Unlock()
	}
}

// cancelCheckLocked stops a torrent's verification if one is running.
// The caller holds e.mu.
func cancelCheckLocked(tr *tracked) {
	if tr.cancelCheck != nil {
		tr.cancelCheck()
	}
}

// ForceRecheck re-verifies a torrent's on-disk data against its piece
// hashes.
//
// Refused before metadata arrives. The library's own VerifyData calls
// NumPieces without checking, and NumPieces reads through metadata that
// may not be there - so a recheck on a magnet that has not found a peer
// yet is a nil dereference that takes the whole daemon down, every other
// torrent with it.
func (e *Engine) ForceRecheck(id TorrentID) error {
	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	if tr.t.Info() == nil {
		return fmt.Errorf("torrent metadata not available yet")
	}
	total := tr.t.NumPieces()

	ctx, done := e.beginCheck(tr, total)
	e.log.Info("recheck started", "id", id, "pieces", total)
	e.snapshotAndBroadcastNow()

	// Registered with the wait group so shutdown, having cancelled the
	// check, waits for it to unwind before closing the client. Without
	// that the library's hashing goroutines outlive their own storage and
	// log a burst of "error marking piece complete ... closed".
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		started := time.Now()
		failed := e.verifyPieces(ctx, tr, total)

		e.mu.Lock()
		reached := tr.checkDone
		e.mu.Unlock()
		done()

		switch {
		// Stopping on request is not a failure, and recording it as one
		// would leave a red error on a torrent the user deliberately told
		// to stop.
		case errors.Is(failed, context.Canceled):
			e.log.Info("recheck cancelled", "id", id, "verified", reached, "of", total)
		case failed != nil:
			e.mu.Lock()
			tr.lastErr = fmt.Sprintf("recheck failed: %v", failed)
			e.mu.Unlock()
			e.log.Error("recheck failed", "id", id, "err", failed)
		default:
			e.log.Info("recheck finished", "id", id, "took", time.Since(started).Round(time.Millisecond))
		}
		e.snapshotAndBroadcastNow()
	}()
	return nil
}

// PurgeData deletes a torrent's data and keeps the torrent, paused, at
// zero - for freeing space without losing the entry, its save path, its
// limits or its place in the list. Resuming it downloads the data again.
//
// Deleting the files is not enough on its own: a running torrent holds
// its own picture of which pieces it has, and one whose data was deleted
// underneath it goes on reporting 100% and offering pieces it cannot
// read. The library has no way to make it look again short of hashing
// every piece, so the torrent is dropped and re-added instead, which is
// instant and hashes nothing.
//
// Nothing has to clear the piece-completion database by hand, which is
// worth knowing before adding code that does: for any piece the database
// calls complete, the file storage stats the files it spans, and a
// missing one both reports incomplete and corrects the record. Deleting
// the data is what makes the record wrong, and looking at it is what
// fixes it.
//
// Re-adding does invalidate any open reader, so a stream of this torrent
// stops - the same caveat MoveStorage carries, for the same reason.
func (e *Engine) PurgeData(id TorrentID) error {
	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	if tr.t.Info() == nil {
		return fmt.Errorf("torrent metadata not available yet")
	}

	e.mu.Lock()
	// Paused, not merely held: a torrent left running would start
	// downloading the data again the moment it came back, which is the
	// opposite of what was asked for. Held as well, so nothing moves in
	// the window before the pause takes effect.
	tr.paused = true
	tr.holdData = true
	tr.applyDataFlow(e.blocked)
	e.mu.Unlock()

	paths := dataPaths(tr.savePath, tr.t)
	ih := tr.t.InfoHash()
	mi := tr.t.Metainfo()
	oldStorage := tr.ownStorage

	tr.t.Drop()
	if oldStorage != nil {
		oldStorage.Close()
	}

	deleteFiles(paths)
	removeEmptyDirs(tr.savePath, paths)

	spec, err := torrent.TorrentSpecFromMetaInfoErr(&mi)
	if err != nil {
		return e.purgeFailed(tr, id, fmt.Errorf("rebuild spec: %w", err))
	}
	spec.InfoHash = ih
	// The same v1/v2 trap MoveStorage documents: an allocated-but-empty
	// piece-layer map makes the library take a v1 torrent for a v2 one and
	// refuse to add it.
	if len(spec.PieceLayers) == 0 {
		spec.PieceLayers = nil
	}
	var newStorage storage.ClientImplCloser
	if oldStorage != nil {
		newStorage = storage.NewFile(tr.savePath)
		spec.Storage = newStorage
	}

	t, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		if newStorage != nil {
			newStorage.Close()
		}
		return e.purgeFailed(tr, id, fmt.Errorf("re-add torrent after purge: %w", err))
	}

	e.mu.Lock()
	tr.t = t
	tr.ownStorage = newStorage
	tr.holdData = false
	tr.applyDataFlow(e.blocked) // still paused, so still off
	e.mu.Unlock()

	e.log.Info("torrent data deleted", "id", id, "name", t.Name(), "save_path", tr.savePath)
	e.persist()
	e.snapshotAndBroadcastNow()
	return nil
}

// purgeFailed records a purge that could not put the torrent back.
//
// The data is already gone and the old torrent already dropped, so there
// is no state to return to; what is left is to say so where the user will
// see it, rather than leaving a row that goes on showing the figures of a
// torrent that is no longer running at all.
func (e *Engine) purgeFailed(tr *tracked, id TorrentID, err error) error {
	e.mu.Lock()
	tr.lastErr = err.Error()
	tr.holdData = false
	e.mu.Unlock()

	e.log.Error("the data was deleted but the torrent could not be re-added", "id", id, "err", err)
	e.persist()
	e.snapshotAndBroadcastNow()
	return err
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

	e.mu.Lock()
	wasPaused := tr.paused
	// Held rather than switched off directly: the tick applies its own
	// verdict every second, and would allow transfers again in the middle
	// of the move - nothing else about the torrent says it is busy.
	tr.holdData = true
	tr.applyDataFlow(e.blocked)
	e.mu.Unlock()

	// Captured before the drop below: the re-added torrent is a fresh
	// instance with no priority history, and this is the last moment the
	// old one has any.
	oldPriorities := make([]Priority, 0, len(tr.t.Files()))
	for _, f := range tr.t.Files() {
		oldPriorities = append(oldPriorities, fromLibPriority(f.Priority()))
	}

	oldPath := tr.savePath
	for _, f := range tr.t.Files() {
		dst := filepath.Join(newDir, f.Path())
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(dst), err)
		}
		// Both names, because an unfinished file is on disk as
		// <path>.part and only one of the two exists at a time. Moving
		// the finished name alone found nothing for every incomplete
		// file, reported success, and left the data behind to be
		// downloaded again.
		for _, suffix := range []string{"", partFileSuffix} {
			src := filepath.Join(oldPath, f.Path()) + suffix
			if err := moveFile(src, dst+suffix); err != nil {
				return fmt.Errorf("move %s: %w", f.Path()+suffix, err)
			}
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
	// A v1 torrent has no piece layers, but Metainfo() still hands back a
	// map for them - allocated, then left empty because no file has a v2
	// piece root. A non-nil map is what the library takes as "this is a
	// v2 torrent", so on re-add it walks every file demanding a root that
	// cannot be there and fails with "no piece root set for file".
	//
	// That made move fail for any file spanning more than one piece,
	// which is every real torrent. It went unnoticed because the one in
	// the tests is a single piece, and those are skipped by that check.
	if len(spec.PieceLayers) == 0 {
		spec.PieceLayers = nil
	}

	tr.t.Drop()
	if oldStorage != nil {
		oldStorage.Close()
	}

	t, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		// The data has already been moved and the old torrent dropped, so
		// there is no unbroken state to return to. Record it where the
		// user will see it: otherwise the list goes on showing the stale
		// figures of a torrent that is no longer running at all.
		err = fmt.Errorf("re-add torrent at new location: %w", err)
		e.mu.Lock()
		tr.savePath = newDir
		tr.lastErr = err.Error()
		e.mu.Unlock()
		e.log.Error("move failed after the data was moved", "id", id, "dir", newDir, "err", err)
		e.snapshotAndBroadcastNow()
		return err
	}
	// The re-added Torrent is a fresh instance with every file back at
	// unset priority, so the ones captured above are put back. Without
	// this a move quietly undid a file selection: everything the user had
	// switched off came back wanted at normal.
	//
	// Applied whether or not the torrent was paused, because a priority
	// is not a pause - and SetPaused only reaches for downloadAllFiles
	// when every file is unset, so a restored selection survives a later
	// resume rather than being overwritten by it.
	if len(oldPriorities) > 0 {
		applyFilePriorities(t, oldPriorities)
	} else if !wasPaused {
		downloadAllFiles(t)
	}

	total := t.NumPieces()

	e.mu.Lock()
	tr.t = t
	tr.savePath = newDir
	tr.ownStorage = newStorage
	e.mu.Unlock()

	ctx, done := e.beginCheck(tr, total)

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		// The same piece-at-a-time verification a recheck uses. It was
		// the library's whole-torrent call here, which meant a move of a
		// large torrent sat on a bare "checking" for as long as it took,
		// with nothing to say whether it was a minute or an hour away.
		verifyErr := e.verifyPieces(ctx, tr, total)
		done()

		e.mu.Lock()
		// Cancelling leaves the data moved and partly verified, which is
		// a state the torrent recovers from by itself - not an error to
		// hang on it.
		if verifyErr != nil && !errors.Is(verifyErr, context.Canceled) {
			tr.lastErr = fmt.Sprintf("verify after move failed: %v", verifyErr)
		}
		tr.holdData = false
		tr.applyDataFlow(e.blocked)
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
	// engine lock is released first - holding both invites a deadlock
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

	// NumPieces and PieceStateRuns have the same requirement as Files:
	// they read through metadata that may not be there, and crash rather
	// than failing.
	var numPieces int
	var pieceLength int64
	var pieces []PieceRun
	if info := tr.t.Info(); info != nil {
		numPieces = tr.t.NumPieces()
		pieceLength = info.PieceLength
		for _, run := range tr.t.PieceStateRuns() {
			pieces = append(pieces, PieceRun{
				Length:   run.Length,
				Known:    run.Ok,
				Complete: run.Ok && run.Complete,
				Partial:  run.Partial,
				Checking: run.Hashing || run.QueuedForHash,
			})
		}
	}

	peers := e.peerInfo(tr, numPieces)

	// Only the static list from the torrent's own metadata. The library
	// exposes no live announce status - no last announce time, no error,
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

	return TorrentDetail{
		Snapshot:    snap,
		Files:       files,
		Peers:       peers,
		Trackers:    trackers,
		Pieces:      pieces,
		NumPieces:   numPieces,
		PieceLength: pieceLength,
	}, nil
}

// peerInfo builds the per-peer view for tr, deriving instantaneous
// download/upload speeds from the byte counters recorded on the previous
// call (see tracked.lastPeers for why the library's own rates can't be
// used). numPieces is 0 before metadata arrives.
func (e *Engine) peerInfo(tr *tracked, numPieces int) []PeerInfo {
	conns := tr.t.PeerConns()

	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tr.lastPeersAt).Seconds()
	if tr.lastPeersAt.IsZero() {
		elapsed = 0
	}
	// Rebuilt from scratch each call so peers that have disconnected drop
	// out rather than accumulating forever.
	current := make(map[string]peerBytes, len(conns))

	peers := make([]PeerInfo, 0, len(conns))
	for _, pc := range conns {
		stats := pc.Peer.Stats()
		addr := fmt.Sprint(pc.RemoteAddr)
		down := stats.BytesReadUsefulData.Int64()
		up := stats.BytesWrittenData.Int64()

		prev, seen := tr.lastPeers[addr]
		downBPS, upBPS := prev.downBPS, prev.upBPS
		if seen && elapsed > 0 {
			downBPS = math.Max(0, float64(down-prev.down)/elapsed)
			upBPS = math.Max(0, float64(up-prev.up)/elapsed)
		}
		current[addr] = peerBytes{down: down, up: up, downBPS: downBPS, upBPS: upBPS}

		var progress float64
		if numPieces > 0 {
			progress = float64(stats.RemotePieceCount) / float64(numPieces)
		}
		id := peerIDPrefix(pc.PeerID)
		peers = append(peers, PeerInfo{
			Addr:        addr,
			Client:      clientName(pc.PeerClientName.Load(), id),
			PeerID:      id,
			Source:      peerSourceName(pc.Discovery),
			DownloadBPS: downBPS,
			UploadBPS:   upBPS,
			PiecesHave:  stats.RemotePieceCount,
			PiecesTotal: numPieces,
			Progress:    progress,
			Encrypted:   pc.PeerPrefersEncryption,
		})
	}

	tr.lastPeers = current
	tr.lastPeersAt = now
	return peers
}

// peerSourceName renders how a peer was discovered. anacrolix/torrent's
// PeerSource values are the terse letter codes it puts on the wire ("Hg",
// "Tr", "X"), which mean nothing to a user reading a peers table.
func peerSourceName(s torrent.PeerSource) string {
	switch s {
	case torrent.PeerSourceTracker:
		return "tracker"
	case torrent.PeerSourceIncoming:
		return "incoming"
	case torrent.PeerSourceDhtGetPeers, torrent.PeerSourceDhtAnnouncePeer:
		return "dht"
	case torrent.PeerSourcePex:
		return "pex"
	case torrent.PeerSourceUtHolepunch:
		return "holepunch"
	case torrent.PeerSourceDirect:
		return "direct"
	default:
		return string(s)
	}
}

// clientName resolves a peer's display name. PeerClientName is an
// atomic.Value that holds nothing until the extended handshake supplies a
// name, so a bare fmt.Sprint of it renders the literal string "<nil>".
func clientName(v any, peerID string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return peerID
}

// peerIDPrefix renders the leading, human-meaningful part of a 20-byte
// peer ID (BEP 20's "-XXnnnn-" client/version prefix), replacing
// unprintable bytes so a hostile peer can't inject control sequences into
// the terminal.
func peerIDPrefix(id torrent.PeerID) string {
	const n = 8
	out := make([]rune, 0, n)
	for _, b := range id[:n] {
		if b < 0x20 || b > 0x7e {
			out = append(out, '.')
			continue
		}
		out = append(out, rune(b))
	}
	return string(out)
}
