package engine

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/mse"
)

// Config configures the engine's underlying torrent.Client.
type Config struct {
	// DataDir is where downloaded files are written.
	DataDir string

	// ListenPortLow/ListenPortHigh bound the port the client tries to
	// bind for BitTorrent/uTP/DHT traffic. anacrolix/torrent's
	// ClientConfig only accepts a single fixed port (0 = random); a range
	// is implemented here by retrying NewClient across the range until
	// one port binds successfully. If both are 0, a random port is used.
	ListenPortLow  int
	ListenPortHigh int

	DisableDHT bool
	DisablePEX bool
	// DisableEncryption forces plaintext-only connections (no MSE/RC4
	// header obfuscation offered or required). This is a client-wide
	// setting; anacrolix/torrent has no per-torrent encryption policy.
	DisableEncryption bool

	Seed bool
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
	cfg    Config
	client *torrent.Client

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

	tc := torrent.NewDefaultClientConfig()
	tc.DataDir = cfg.DataDir
	tc.NoDHT = cfg.DisableDHT
	tc.DisablePEX = cfg.DisablePEX
	tc.Seed = cfg.Seed
	if cfg.DisableEncryption {
		tc.HeaderObfuscationPolicy = torrent.HeaderObfuscationPolicy{
			Preferred: false, RequirePreferred: false,
		}
		tc.CryptoProvides = mse.CryptoMethodPlaintext
	}

	client, err := newClientOnPortRange(tc, cfg.ListenPortLow, cfg.ListenPortHigh)
	if err != nil {
		return nil, fmt.Errorf("engine: start torrent client: %w", err)
	}

	e := &Engine{
		cfg:      cfg,
		client:   client,
		torrents: map[TorrentID]*tracked{},
		subs:     map[chan Event]struct{}{},
		closeCh:  make(chan struct{}),
	}
	e.wg.Add(1)
	go e.tickLoop()
	return e, nil
}

// newClientOnPortRange binds the first port in [low,high] that is free.
//
// The library takes a single fixed port, so a range is simply a loop:
// try each one, keep the first client that comes up. Useful when several
// torrent clients share a machine, or a port is briefly still in use
// after a restart.
func newClientOnPortRange(tc *torrent.ClientConfig, low, high int) (*torrent.Client, error) {
	if low <= 0 {
		return torrent.NewClient(tc) // port 0: let the OS choose
	}
	if high < low {
		high = low
	}
	var lastErr error
	for port := low; port <= high; port++ {
		tc.ListenPort = port
		client, err := torrent.NewClient(tc)
		if err == nil {
			return client, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no free port in [%d,%d]: %w", low, high, lastErr)
}

// Close stops the tick loop and closes every subscriber's channel.
func (e *Engine) Close() error {
	close(e.closeCh)
	e.wg.Wait()

	e.mu.Lock()
	for ch := range e.subs {
		close(ch)
	}
	e.subs = nil
	// Unlocked explicitly rather than deferred: closing the client can
	// block while it tears down connections, and holding the lock through
	// that would stall anything still calling in.
	e.mu.Unlock()

	if errs := e.client.Close(); len(errs) > 0 {
		return fmt.Errorf("engine: close: %v", errs)
	}
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
