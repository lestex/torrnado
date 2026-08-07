package engine

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotFound is returned for an id no torrent answers to.
var ErrNotFound = fmt.Errorf("torrent not found")

// simulatedLength is the size this in-memory engine gives every torrent
// it is asked to add.
const simulatedLength = 700 << 20 // 700 MiB

// AddMagnet adds a torrent from a magnet URI.
func (e *Engine) AddMagnet(uri string, opts AddOpts) (TorrentID, error) {
	if !strings.HasPrefix(uri, "magnet:") {
		return "", fmt.Errorf("not a magnet uri: %s", uri)
	}
	return e.add(magnetName(uri), fakeID(uri), opts)
}

// AddTorrentFile adds a torrent from a local .torrent file.
func (e *Engine) AddTorrentFile(path string, opts AddOpts) (TorrentID, error) {
	// Check the file is there before accepting it. Anything that is not a
	// magnet reaches this function, so without this a typo is silently
	// added as a torrent that can never download.
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("read torrent file: %w", err)
	}
	name := strings.TrimSuffix(filepath.Base(path), ".torrent")
	return e.add(name, fakeID(path), opts)
}

// add registers a torrent. Adding one that is already tracked is not an
// error -- it simply returns the existing id, matching what a real
// client does with a duplicate infohash.
func (e *Engine) add(name string, id TorrentID, opts AddOpts) (TorrentID, error) {
	savePath := e.cfg.DataDir
	if opts.SavePath != "" {
		savePath = opts.SavePath
	}

	e.mu.Lock()
	if _, exists := e.torrents[id]; !exists {
		e.torrents[id] = &tracked{
			id:       id,
			name:     name,
			total:    simulatedLength,
			paused:   opts.Paused,
			addedAt:  time.Now(),
			savePath: savePath,
		}
	}
	e.mu.Unlock()

	e.snapshotAndBroadcastNow()
	return id, nil
}

// RemoveTorrent stops tracking a torrent. deleteData is accepted but has
// nothing to delete yet.
func (e *Engine) RemoveTorrent(id TorrentID, deleteData bool) error {
	e.mu.Lock()
	if _, ok := e.torrents[id]; !ok {
		e.mu.Unlock()
		return ErrNotFound
	}
	delete(e.torrents, id)
	e.mu.Unlock()

	e.snapshotAndBroadcastNow()
	return nil
}

// SetPaused pauses or resumes a torrent.
func (e *Engine) SetPaused(id TorrentID, paused bool) error {
	e.mu.Lock()
	tr, ok := e.torrents[id]
	if ok {
		tr.paused = paused
	}
	e.mu.Unlock()
	if !ok {
		return ErrNotFound
	}

	e.snapshotAndBroadcastNow()
	return nil
}

// ListTorrents returns a snapshot of every tracked torrent.
func (e *Engine) ListTorrents() []TorrentSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]TorrentSnapshot, 0, len(e.torrents))
	for _, tr := range e.torrents {
		out = append(out, tr.snapshot())
	}
	return out
}

// TorrentDetail returns the full detail view for one torrent.
func (e *Engine) TorrentDetail(id TorrentID) (TorrentDetail, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	tr, ok := e.torrents[id]
	if !ok {
		return TorrentDetail{}, ErrNotFound
	}
	return TorrentDetail{Snapshot: tr.snapshot()}, nil
}

// fakeID derives a stable id from a source string, standing in for the
// infohash a real client would read out of the torrent's metadata.
func fakeID(source string) TorrentID {
	sum := sha1.Sum([]byte(source))
	return TorrentID(hex.EncodeToString(sum[:]))
}

// magnetName pulls the display name (the "dn" parameter) out of a magnet
// URI, falling back to the URI itself.
func magnetName(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	if dn := u.Query().Get("dn"); dn != "" {
		return dn
	}
	return uri
}
