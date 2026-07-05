package engine

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Config configures the engine.
type Config struct {
	// DataDir is where downloaded files are written.
	DataDir string
}

// tickInterval is how often the engine recomputes progress and speeds and
// broadcasts a fresh Event.
const tickInterval = time.Second

// simulatedRate is the download speed this in-memory engine pretends to
// achieve, so progress visibly moves while the real networking is still
// to be written.
const simulatedRate = 2 << 20 // 2 MiB/s

// tracked is one torrent's mutable bookkeeping, private to the engine.
// Callers only ever see the immutable TorrentSnapshot built from it.
type tracked struct {
	id       TorrentID
	name     string
	total    int64
	done     int64
	rate     float64 // bytes/sec, this tick
	paused   bool
	addedAt  time.Time
	savePath string
}

// advance moves a torrent forward by dt seconds. Paused torrents and
// finished ones stand still.
func (tr *tracked) advance(dt float64) {
	if tr.paused || tr.done >= tr.total {
		tr.rate = 0
		return
	}
	tr.rate = simulatedRate
	tr.done += int64(tr.rate * dt)
	if tr.done > tr.total {
		tr.done = tr.total
	}
}

// snapshot builds the public view of a torrent.
func (tr *tracked) snapshot() TorrentSnapshot {
	var progress float64
	if tr.total > 0 {
		progress = float64(tr.done) / float64(tr.total)
	}

	state := StateDownloading
	switch {
	case tr.paused:
		state = StatePaused
	case tr.total > 0 && tr.done >= tr.total:
		state = StateSeeding
	}

	var eta time.Duration
	if missing := tr.total - tr.done; missing > 0 && tr.rate > 0 {
		eta = time.Duration(float64(missing)/tr.rate) * time.Second
	}

	return TorrentSnapshot{
		ID:          tr.id,
		Name:        tr.name,
		InfoHash:    string(tr.id),
		TotalLength: tr.total,
		Completed:   tr.done,
		Progress:    progress,
		DownloadBPS: tr.rate,
		Downloaded:  tr.done,
		ETA:         eta,
		State:       state,
		Paused:      tr.paused,
		SavePath:    tr.savePath,
		AddedAt:     tr.addedAt,
	}
}

// Engine tracks torrents and publishes their state.
//
// Callers hold an *Engine and never anything below it, which is what lets
// the storage and networking underneath be replaced without touching a
// single caller.
type Engine struct {
	cfg Config

	// mu guards torrents and subs. Every exported method takes it, so an
	// Engine is safe to share between the RPC server's goroutines.
	mu       sync.Mutex
	torrents map[TorrentID]*tracked
	subs     map[chan Event]struct{}

	closeCh chan struct{}
	wg      sync.WaitGroup
}

// New starts an engine and its background tick loop.
func New(cfg Config) (*Engine, error) {
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("engine: DataDir must be set")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("engine: create data dir: %w", err)
	}

	e := &Engine{
		cfg:      cfg,
		torrents: map[TorrentID]*tracked{},
		subs:     map[chan Event]struct{}{},
		closeCh:  make(chan struct{}),
	}
	e.wg.Add(1)
	go e.tickLoop()
	return e, nil
}

// Close stops the tick loop and closes every subscriber's channel.
func (e *Engine) Close() error {
	close(e.closeCh)
	e.wg.Wait()

	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs {
		close(ch)
	}
	e.subs = nil
	return nil
}

// Subscribe registers a new event listener. Call unsubscribe when done;
// it is safe to call more than once.
func (e *Engine) Subscribe() (events <-chan Event, unsubscribe func()) {
	// Buffered with a single slot, because broadcast never blocks: a
	// subscriber that falls behind gets the newest state, not a backlog.
	ch := make(chan Event, 1)

	e.mu.Lock()
	e.subs[ch] = struct{}{}
	e.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			e.mu.Lock()
			defer e.mu.Unlock()
			if _, ok := e.subs[ch]; ok {
				delete(e.subs, ch)
				close(ch)
			}
		})
	}
}

// broadcast sends ev to every subscriber without ever blocking.
//
// A UI that stalls for a second must not stall the engine with it, and a
// stale snapshot is worthless anyway -- so a full channel has its
// pending event replaced rather than waited on.
func (e *Engine) broadcast(ev Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs {
		select {
		case ch <- ev:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}
}

func (e *Engine) tickLoop() {
	defer e.wg.Done()
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.closeCh:
			return
		case <-ticker.C:
			e.tick()
		}
	}
}

// tick advances every unpaused torrent and publishes the result.
func (e *Engine) tick() {
	e.mu.Lock()
	for _, tr := range e.torrents {
		tr.advance(tickInterval.Seconds())
	}
	ev := e.eventLocked()
	e.mu.Unlock()

	e.broadcast(ev)
}

// snapshotAndBroadcastNow publishes current state immediately, so a
// caller that just changed something sees it without waiting for the next
// tick.
func (e *Engine) snapshotAndBroadcastNow() {
	e.mu.Lock()
	ev := e.eventLocked()
	e.mu.Unlock()

	e.broadcast(ev)
}

// eventLocked builds an Event from current state. Callers must hold e.mu.
func (e *Engine) eventLocked() Event {
	ev := Event{
		Torrents: make([]TorrentSnapshot, 0, len(e.torrents)),
		At:       time.Now(),
	}
	for _, tr := range e.torrents {
		snap := tr.snapshot()
		ev.Torrents = append(ev.Torrents, snap)
		ev.Global.DownloadBPS += snap.DownloadBPS
		ev.Global.TotalDownload += snap.Completed
	}
	ev.Global.NumTorrents = len(ev.Torrents)
	return ev
}
