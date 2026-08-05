package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
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
func (e *Engine) SetPaused(id TorrentID, paused bool) error {
	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	tr.paused = paused
	e.mu.Unlock()

	e.snapshotAndBroadcastNow()
	return nil
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
