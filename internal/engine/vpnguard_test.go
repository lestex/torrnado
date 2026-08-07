package engine

import (
	"encoding/json"
	"os"
	"testing"
)

// fakeVPN is a VPN check a test can flip between ticks.
type fakeVPN struct{ active bool }

func (f *fakeVPN) check() VPNStatus {
	if f.active {
		return VPNStatus{Active: true, Interface: "utun9"}
	}
	return VPNStatus{Reason: "traffic leaves by en0, which is a physical device"}
}

// guardedEngine is an engine with the guard on and the VPN in the given
// state, plus the switch to change it.
func guardedEngine(t *testing.T, active bool) (*Engine, *fakeVPN) {
	t.Helper()
	fake := &fakeVPN{active: active}
	cfg := testConfig(t)
	cfg.RequireVPN = true
	cfg.VPNCheck = fake.check

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e, fake
}

// dataFlowOf reports what the engine would allow for one torrent right
// now. The library keeps no readable copy of either switch, so this asks
// the decision rather than the torrent.
func dataFlowOf(t *testing.T, e *Engine, id TorrentID) (down, up bool) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	tr, ok := e.torrents[id]
	if !ok {
		t.Fatalf("no torrent %q", id)
	}
	return tr.dataFlow(e.blocked)
}

// The whole point: nothing transfers while the system is off-VPN.
func TestGuardHoldsTransfersWhileOffVPN(t *testing.T) {
	e, fake := guardedEngine(t, false)

	// A real torrent rather than a magnet: a torrent still fetching a
	// magnet's metadata reports "checking" whatever else is true of it,
	// and the status this is about would be hidden behind that.
	id, err := e.AddTorrentFile(writeTestTorrent(t, t.TempDir(), 64*1024), AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	// Immediately, not a tick later: a torrent added off-VPN must not get
	// the second until the next tick as a head start.
	if down, up := dataFlowOf(t, e, id); down || up {
		t.Errorf("a torrent added off-VPN may transfer (down=%v up=%v)", down, up)
	}
	if got := e.ListTorrents()[0].StatusText(); got != "blocked" {
		t.Errorf("status = %q, want %q", got, "blocked")
	}

	// And it comes back by itself, without anyone touching the torrent.
	fake.active = true
	e.tick()

	if down, up := dataFlowOf(t, e, id); !down || !up {
		t.Errorf("transfers still held after the VPN came up (down=%v up=%v)", down, up)
	}
	if got := e.ListTorrents()[0].StatusText(); got == "blocked" {
		t.Error("still reported as blocked after the VPN came up")
	}
}

func TestGuardBlocksWhenTheVPNDropsUnderway(t *testing.T) {
	e, fake := guardedEngine(t, true)

	id, _ := e.AddMagnet(testMagnet, AddOpts{})
	if down, up := dataFlowOf(t, e, id); !down || !up {
		t.Fatalf("nothing was allowed even on a VPN (down=%v up=%v)", down, up)
	}

	fake.active = false
	e.tick()

	if down, up := dataFlowOf(t, e, id); down || up {
		t.Errorf("transfers continued after the VPN dropped (down=%v up=%v)", down, up)
	}
}

// The guard is a condition of the machine; pausing is what the user
// asked for. Conflating them would have a VPN drop rewrite the session
// with everything paused, and a reconnect would leave it that way.
func TestGuardDoesNotTouchThePauseFlagOrTheSession(t *testing.T) {
	e, fake := guardedEngine(t, true)
	e.cfg.StateDir = t.TempDir()

	id, err := e.AddTorrentFile(writeTestTorrent(t, t.TempDir(), 64*1024), AddOpts{})
	if err != nil {
		t.Fatalf("AddTorrentFile: %v", err)
	}

	fake.active = false
	e.tick()

	if snap := e.ListTorrents()[0]; snap.Paused {
		t.Error("the VPN guard paused a torrent instead of holding it")
	}

	e.persist()
	data, err := os.ReadFile(e.sessionPath())
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	var sf sessionFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("parsing the session: %v", err)
	}
	if len(sf.Torrents) != 1 {
		t.Fatalf("session holds %d torrents, want 1", len(sf.Torrents))
	}
	if sf.Torrents[0].Paused {
		t.Error("the session recorded a blocked torrent as paused; it would stay paused after a restart")
	}

	// A torrent the user really did pause stays paused when the VPN
	// returns -- the two states do not leak into each other.
	if err := e.SetPaused(id, true); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}
	fake.active = true
	e.tick()

	if snap := e.ListTorrents()[0]; !snap.Paused || snap.StatusText() != "paused" {
		t.Errorf("after the VPN returned: paused=%v status=%q, want paused",
			snap.Paused, snap.StatusText())
	}
	if down, up := dataFlowOf(t, e, id); down || up {
		t.Errorf("a paused torrent was allowed to transfer once the VPN was up (down=%v up=%v)", down, up)
	}
}

// Resuming during an outage records the intent without lifting the guard,
// so it takes effect the moment the VPN returns rather than being
// forgotten or, worse, letting that one torrent through.
func TestResumingWhileBlockedTakesEffectLater(t *testing.T) {
	e, fake := guardedEngine(t, false)

	id, _ := e.AddMagnet(testMagnet, AddOpts{Paused: true})
	if err := e.SetPaused(id, false); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	if snap := e.ListTorrents()[0]; snap.Paused {
		t.Error("the resume was not recorded")
	}
	if down, up := dataFlowOf(t, e, id); down || up {
		t.Errorf("resuming let a torrent through the guard (down=%v up=%v)", down, up)
	}

	fake.active = true
	e.tick()

	if down, up := dataFlowOf(t, e, id); !down || !up {
		t.Errorf("the resume was forgotten (down=%v up=%v)", down, up)
	}
}

// Off by default means off: no check is run, nothing is ever held, and
// the state a client sees says the guard is not in play rather than
// "no VPN".
func TestNothingIsBlockedWhenTheGuardIsOff(t *testing.T) {
	e := newTestEngine(t) // RequireVPN unset

	calls := 0
	e.cfg.VPNCheck = func() VPNStatus { calls++; return VPNStatus{} }

	id, _ := e.AddMagnet(testMagnet, AddOpts{})
	e.tick()

	if calls != 0 {
		t.Errorf("the VPN check ran %d times with the guard off", calls)
	}
	if down, up := dataFlowOf(t, e, id); !down || !up {
		t.Errorf("a transfer was held with the guard off (down=%v up=%v)", down, up)
	}

	e.mu.Lock()
	ev := e.eventLocked()
	e.mu.Unlock()
	if ev.Global.VPNRequired {
		t.Error("VPNRequired is set with the guard off")
	}
}

// A guard with no way to check is a guard that cannot clear anything, so
// it holds. Failing open here would mean a misconfigured daemon quietly
// transferring off-VPN, which is the exact thing being guarded against.
func TestAMissingCheckBlocksEverything(t *testing.T) {
	cfg := testConfig(t)
	cfg.RequireVPN = true // and no VPNCheck

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	id, _ := e.AddMagnet(testMagnet, AddOpts{})
	if down, up := dataFlowOf(t, e, id); down || up {
		t.Errorf("transfers allowed with no way to check for a VPN (down=%v up=%v)", down, up)
	}
}

// The rate limiter and the guard are independent reasons to stop, and
// neither may switch the other back on -- the bug this funnel exists to
// prevent, since the tick used to allow data whenever a torrent was under
// its cap.
func TestRateLimitAndGuardDoNotOverrideEachOther(t *testing.T) {
	tr := &tracked{}

	cases := []struct {
		name             string
		paused, blocked  bool
		hold             bool
		downLimit        int64
		lastDownBPS      float64
		wantDown, wantUp bool
	}{
		{name: "nothing in the way", wantDown: true, wantUp: true},
		{name: "under its cap", downLimit: 1000, lastDownBPS: 500, wantDown: true, wantUp: true},
		{name: "over its cap", downLimit: 1000, lastDownBPS: 5000, wantDown: false, wantUp: true},
		{name: "under its cap but off-VPN", downLimit: 1000, lastDownBPS: 500, blocked: true},
		{name: "under its cap but paused", downLimit: 1000, lastDownBPS: 500, paused: true},
		{name: "mid-move", hold: true},
	}
	for _, c := range cases {
		tr.paused, tr.holdData = c.paused, c.hold
		tr.downLimit, tr.lastDownBPS = c.downLimit, c.lastDownBPS

		down, up := tr.dataFlow(c.blocked)
		if down != c.wantDown || up != c.wantUp {
			t.Errorf("%s: down=%v up=%v, want down=%v up=%v",
				c.name, down, up, c.wantDown, c.wantUp)
		}
	}
}
