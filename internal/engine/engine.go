package engine

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/mse"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
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

	// UploadRateLimit/DownloadRateLimit are global, client-wide caps in
	// bytes/sec (0 = unlimited). The library supports a single pair of
	// limiters at the Client level and nothing per torrent -- see
	// SetTorrentRateLimit for how per-torrent caps are approximated on
	// top of that.
	UploadRateLimit   int64
	DownloadRateLimit int64

	Seed bool

	// Logger receives the engine's own messages and the torrent
	// library's. Nil discards both.
	Logger *slog.Logger
	// LibraryLevel filters the library separately, since it warns about
	// every tracker that misbehaves.
	LibraryLevel slog.Level
}

// tickInterval is how often the engine recomputes progress and speeds and
// broadcasts a fresh Event.
const tickInterval = time.Second

// tracked is one torrent's bookkeeping, private to the engine. Callers
// only ever see the immutable TorrentSnapshot built from it.
type tracked struct {
	t        *torrent.Torrent
	addedAt  time.Time
	paused   bool
	savePath string
	// ownStorage is set when this torrent was given a dedicated save
	// path, rather than using the engine's shared default storage. It
	// must be closed when the torrent is removed.
	ownStorage storage.ClientImplCloser

	// The client reports cumulative byte counters, not speeds, so a rate
	// is the change since the previous tick divided by the time between
	// them. These hold the previous reading and the rate derived from it.
	lastDownloaded int64
	lastUploaded   int64
	lastDownBPS    float64
	lastUpBPS      float64

	downLimit int64 // bytes/sec, 0 = unlimited (best-effort; see SetTorrentRateLimit)
	upLimit   int64

	// lastPeers holds the previous per-peer byte counters, keyed by
	// remote address, so a detail call can report instantaneous peer
	// speeds. The library has no such rate: PeerStats.DownloadRate is a
	// lifetime average, so speeds are deltas between detail calls, the
	// same trick updateRates uses per torrent.
	lastPeers   map[string]peerBytes
	lastPeersAt time.Time

	checking bool   // a hash check is running
	lastErr  string // last failure worth showing the user

	// How far a running hash check has got. Meaningful only while
	// checking is set.
	checkDone  int
	checkTotal int
}

// peerBytes is one peer's counters from the previous detail call, plus
// the rates derived then -- reused when two calls land close enough
// together that a delta would be meaningless.
type peerBytes struct {
	down, up       int64
	downBPS, upBPS float64
}

// Engine tracks torrents and publishes their state.
//
// Callers hold an *Engine and never anything below it, which is what lets
// the storage and networking underneath be replaced without touching a
// single caller.
type Engine struct {
	cfg    Config
	client *torrent.Client
	log    *slog.Logger

	upLimiter   *rate.Limiter
	downLimiter *rate.Limiter

	// mu guards torrents and subs. Every exported method takes it, so an
	// Engine is safe to share between the RPC server's goroutines.
	mu       sync.Mutex
	torrents map[TorrentID]*tracked
	subs     map[chan Event]struct{}

	lastTick time.Time
	closeCh  chan struct{}
	wg       sync.WaitGroup
}

// New starts an engine and its background tick loop.
func New(cfg Config) (*Engine, error) {
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("engine: DataDir must be set")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("engine: create data dir: %w", err)
	}

	// The limiters are always installed, even when unlimited: the client
	// documents that a nil limiter cannot be turned into a limited one
	// later, only an existing limiter's rate can be changed.
	upLimiter := rate.NewLimiter(rate.Inf, 1<<20)
	downLimiter := rate.NewLimiter(rate.Inf, 1<<20)
	if cfg.UploadRateLimit > 0 {
		upLimiter.SetLimit(rate.Limit(cfg.UploadRateLimit))
	}
	if cfg.DownloadRateLimit > 0 {
		downLimiter.SetLimit(rate.Limit(cfg.DownloadRateLimit))
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	routeLibraryLogs(logger.Handler(), cfg.LibraryLevel)

	tc := torrent.NewDefaultClientConfig()
	tc.DataDir = cfg.DataDir
	tc.NoDHT = cfg.DisableDHT
	tc.DisablePEX = cfg.DisablePEX
	tc.Seed = cfg.Seed
	tc.UploadRateLimiter = upLimiter
	tc.DownloadRateLimiter = downLimiter
	// The library's own recommendation for capturing what the client
	// logs. It does not catch everything -- see routeLibraryLogs.
	tc.Slogger = slog.New(libraryHandler{h: logger.Handler(), min: cfg.LibraryLevel})
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
		cfg:         cfg,
		log:         logger,
		client:      client,
		upLimiter:   upLimiter,
		downLimiter: downLimiter,
		torrents:    map[TorrentID]*tracked{},
		subs:        map[chan Event]struct{}{},
		lastTick:    time.Now(),
		closeCh:     make(chan struct{}),
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

// dhtNodeCount totals the nodes known across every DHT server, which is
// a rough measure of how well peer discovery is doing.
func dhtNodeCount(c *torrent.Client) int {
	var n int
	for _, s := range c.DhtServers() {
		n += s.Stats().(dht.ServerStats).GoodNodes
	}
	return n
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

// tick recomputes speeds and publishes the current state.
func (e *Engine) tick() {
	e.mu.Lock()
	now := time.Now()
	elapsed := now.Sub(e.lastTick).Seconds()
	if elapsed <= 0 {
		elapsed = tickInterval.Seconds()
	}
	e.lastTick = now

	for _, tr := range e.torrents {
		tr.updateRates(elapsed)
		tr.enforceRateLimit()
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
	for id, tr := range e.torrents {
		snap := e.snapshotLocked(id, tr)
		ev.Torrents = append(ev.Torrents, snap)
		ev.Global.DownloadBPS += snap.DownloadBPS
		ev.Global.TotalDownload += snap.Completed
	}
	ev.Global.NumTorrents = len(ev.Torrents)
	ev.Global.ListenPort = e.client.LocalPort()
	ev.Global.DhtNodes = dhtNodeCount(e.client)
	if free, total, err := diskUsage(e.cfg.DataDir); err == nil {
		ev.Global.DiskFreeBytes = free
		ev.Global.DiskTotalBytes = total
	}
	return ev
}
