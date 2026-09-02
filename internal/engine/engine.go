package engine

import (
	"context"
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

	// RequireVPN holds every transfer, in both directions, while VPNCheck
	// reports the system is not on a VPN. Torrents are not paused: the
	// user's own pause flags are left exactly as they were, and transfers
	// resume by themselves when the VPN comes back.
	//
	// It stops piece data. Tracker announces and DHT traffic carry on -
	// they are client-wide and can only be turned off when the client is
	// built - so a blocked daemon is still visible to the swarm.
	RequireVPN bool
	// VPNCheck reports whether the system is on a VPN. Called once at
	// startup and again on every tick, so it must be cheap and must not
	// block. Only consulted when RequireVPN is set.
	//
	// Leaving it nil with RequireVPN set blocks everything: nothing can be
	// verified, and a guard that gives up and allows transfers is not a
	// guard.
	VPNCheck func() VPNStatus

	// UploadRateLimit/DownloadRateLimit are global, client-wide caps in
	// bytes/sec (0 = unlimited). The library supports a single pair of
	// limiters at the Client level and nothing per torrent - see
	// SetTorrentRateLimit for how per-torrent caps are approximated on
	// top of that.
	UploadRateLimit   int64
	DownloadRateLimit int64

	Seed bool

	// StateDir is where the session file and saved metainfo live, so a
	// restart can pick the torrent list back up. Empty disables
	// persistence entirely.
	StateDir string

	// Version is the daemon's build string, reported in GlobalStats so a
	// client can see what it is talking to. Empty is fine; it simply
	// leaves the field empty on the wire.
	Version string

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
	// opMu serializes move, purge and remove, which each drop the
	// library's Torrent and add a fresh one. Two interleaving on one
	// infohash wedge the client's lock. Nothing else serializes them: each
	// IPC connection dispatches inline, so two clients are enough.
	//
	// Held for an operation's synchronous part only, never across the
	// background verification a move starts - that runs for hours. Always
	// taken before e.mu, never while holding it.
	opMu sync.Mutex

	t        *torrent.Torrent
	addedAt  time.Time
	paused   bool
	savePath string
	// magnet is the URI this was added from, if it was. Kept for the
	// session file: a torrent whose metadata never arrived has no
	// metainfo to be re-added from, only this.
	magnet string
	// ownStorage is set when this torrent was given a dedicated save
	// path, rather than using the engine's shared default storage. It
	// must be closed when the torrent is removed.
	ownStorage storage.ClientImplCloser

	// chosenFiles holds the indices something has deliberately set a
	// priority for, so marking a torrent's files wanted can leave those
	// alone. The library cannot answer this: a file nobody has touched
	// and a file explicitly set to "none" are both PiecePriorityNone, so
	// asking it which is which marks a deselected file wanted again.
	chosenFiles map[int]bool

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

	// completeLogged stops the completion message repeating on every
	// tick once a torrent is done.
	completeLogged bool

	// holdData stops data moving for the duration of an operation that is
	// rearranging the torrent underneath it - a storage move. Without it
	// the next tick would happily allow transfers again halfway through,
	// since nothing else about the torrent says it is busy.
	holdData bool

	// checkWG is done once a cancelled verification has actually unwound.
	// See quiesceCheck for why waiting matters.
	checkWG sync.WaitGroup

	// cancelCheck stops the verification loop in checking, if one is
	// running. Held here rather than passed around because the things
	// that want to stop it - a pause, a removal, a shutdown - all reach
	// the torrent, not the goroutine.
	cancelCheck context.CancelFunc

	checking bool   // a hash check is running
	lastErr  string // last failure worth showing the user

	// How far a running hash check has got. Meaningful only while
	// checking is set.
	checkDone  int
	checkTotal int
}

// peerBytes is one peer's counters from the previous detail call, plus
// the rates derived then - reused when two calls land close enough
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

	closeOnce sync.Once
	closeErr  error

	// restoring suppresses session saves while a restore is in progress
	// (see persist).
	restoring bool

	upLimiter   *rate.Limiter
	downLimiter *rate.Limiter

	// mu guards torrents and subs. Every exported method takes it, so an
	// Engine is safe to share between the RPC server's goroutines.
	mu       sync.Mutex
	torrents map[TorrentID]*tracked
	subs     map[chan Event]struct{}

	// vpn is the last verdict from cfg.VPNCheck and blocked is what it
	// means for transfers. Separate from any torrent's paused flag: one is
	// a condition of the machine, the other is what the user asked for,
	// and conflating them would have a VPN drop rewrite the session file
	// with everything paused. Guarded by mu.
	vpn        VPNStatus
	blocked    bool
	vpnChecked bool

	lastTick  time.Time
	startedAt time.Time
	closeCh   chan struct{}
	wg        sync.WaitGroup
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
	// logs. It does not catch everything - see routeLibraryLogs.
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
		startedAt:   time.Now(),
		closeCh:     make(chan struct{}),
	}
	// Before the tick loop and before RestoreSession can add anything, so
	// a daemon started off-VPN never has a torrent that is briefly
	// allowed to transfer.
	e.refreshVPN()

	e.wg.Add(1)
	go e.tickLoop()
	return e, nil
}

// refreshVPN re-runs the VPN check and records what it means, logging the
// transitions and nothing else - the check runs every second, and a line
// per second saying the same thing is not a log, it is a wall.
func (e *Engine) refreshVPN() {
	if !e.cfg.RequireVPN {
		return // not asked for; nothing is ever blocked
	}

	// Called outside the lock: it dials a socket and enumerates
	// interfaces, and every RPC would queue behind it.
	st := VPNStatus{Reason: "no VPN check configured"}
	if e.cfg.VPNCheck != nil {
		st = e.cfg.VPNCheck()
	}

	blocked := !st.Active

	e.mu.Lock()
	changed := !e.vpnChecked || e.blocked != blocked
	e.vpn = st
	e.blocked = blocked
	e.vpnChecked = true
	e.mu.Unlock()

	// The switches themselves are turned in tick, which calls this first
	// and then applies the verdict to every torrent - so a change takes
	// effect on the same tick that noticed it.
	if !changed {
		return
	}
	if st.Active {
		e.log.Info("VPN detected, transfers allowed", "interface", st.Interface)
	} else {
		e.log.Warn("transfers held: the system is not on a VPN", "reason", st.Reason)
	}
}

// newClientOnPortRange binds the first port in [low,high] that is free.
//
// The library takes a single fixed port, so a range is simply a loop:
// try each one, keep the first client that comes up. Useful when several
// torrent clients share a machine, or a port is briefly still in use
// after a restart.
func newClientOnPortRange(tc *torrent.ClientConfig, low, high int) (*torrent.Client, error) {
	if low <= 0 {
		// Explicitly zero, not merely left alone: the library's default
		// config carries a fixed port (42069), so leaving it untouched
		// pins every client that asked for "any" to the same one. Two
		// daemons on a machine, or two tests running in parallel, then
		// fail with "address already in use" while the config said the OS
		// would choose.
		tc.ListenPort = 0
		return torrent.NewClient(tc)
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
//
// Safe to call more than once: shutdown paths overlap - a deferred close
// and an explicit one - and closing the same channel twice panics.
func (e *Engine) Close() error {
	e.closeOnce.Do(func() { e.closeErr = e.closeNow() })
	return e.closeErr
}

func (e *Engine) closeNow() error {
	// Saved before anything is torn down: a clean shutdown should leave
	// the session on disk matching what was running.
	e.persist()

	// Verification goroutines are not in e.wg, so nothing below waits for
	// them. Told to stop first, they unwind on their own rather than
	// racing the client out from under themselves.
	e.mu.Lock()
	for _, tr := range e.torrents {
		cancelCheckLocked(tr)
	}
	e.mu.Unlock()

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
// stale snapshot is worthless anyway - so a full channel has its
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
	// Before the lock, and before the switches below are turned: a VPN
	// that dropped a moment ago should stop transfers on this tick, not
	// the next one.
	e.refreshVPN()

	e.mu.Lock()
	now := time.Now()
	elapsed := now.Sub(e.lastTick).Seconds()
	if elapsed <= 0 {
		elapsed = tickInterval.Seconds()
	}
	e.lastTick = now

	// Decided here, applied after the lock is dropped - see flowDecision.
	type flow struct {
		t        *torrent.Torrent
		down, up bool
	}
	flows := make([]flow, 0, len(e.torrents))
	for _, tr := range e.torrents {
		tr.updateRates(elapsed)
		t, down, up := tr.flowDecision(e.blocked)
		flows = append(flows, flow{t, down, up})
	}
	ev := e.eventLocked()
	// Collected under the lock, logged outside it: the destination may be
	// a file, and a slow write should not stall every other operation.
	done := e.newlyCompleteLocked()
	e.mu.Unlock()

	for _, f := range flows {
		applyFlow(f.t, f.down, f.up)
	}
	for _, s := range done {
		e.log.Info("torrent complete", "id", s.ID, "name", s.Name, "size", s.TotalLength)
	}
	e.broadcast(ev)
}

// newlyCompleteLocked returns the torrents that finished since the last
// call, marking them so each is reported once. Callers must hold e.mu.
func (e *Engine) newlyCompleteLocked() []TorrentSnapshot {
	var done []TorrentSnapshot
	for id, tr := range e.torrents {
		if tr.completeLogged || tr.t.Info() == nil || !tr.t.Complete().Bool() {
			continue
		}
		tr.completeLogged = true
		done = append(done, e.snapshotLocked(id, tr))
	}
	return done
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
	ev.Global.VPNRequired = e.cfg.RequireVPN
	ev.Global.VPNActive = e.vpn.Active
	ev.Global.VPNInterface = e.vpn.Interface
	ev.Global.Version = e.cfg.Version
	ev.Global.StartedAt = e.startedAt
	if free, total, err := diskUsage(e.cfg.DataDir); err == nil {
		ev.Global.DiskFreeBytes = free
		ev.Global.DiskTotalBytes = total
	}
	return ev
}
