package engine

import (
	"testing"
	"time"
)

// newTestEngine builds an engine writing into a directory the testing
// package creates and removes for us.
func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func TestNewRequiresDataDir(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New with no DataDir should fail")
	}
}

func TestSubscribeReceivesEvents(t *testing.T) {
	e := newTestEngine(t)
	events, unsubscribe := e.Subscribe()
	defer unsubscribe()

	// Drive a tick directly rather than waiting a real second for the
	// background loop -- tests should not sleep if they can avoid it.
	e.tick()

	select {
	case ev := <-events:
		if ev.At.IsZero() {
			t.Error("event has no timestamp")
		}
	case <-time.After(time.Second):
		t.Fatal("no event after tick")
	}
}

// broadcast must never block, even when a subscriber has stopped reading,
// or one stalled UI would freeze the whole engine.
func TestBroadcastDropsStaleEvents(t *testing.T) {
	e := newTestEngine(t)
	events, unsubscribe := e.Subscribe()
	defer unsubscribe()

	first := Event{At: time.Now()}
	second := Event{At: first.At.Add(time.Second)}

	// The channel holds one event. The second send has nowhere to go, so
	// it must replace the first rather than wait for a reader.
	e.broadcast(first)
	e.broadcast(second)

	got := <-events
	if !got.At.Equal(second.At) {
		t.Errorf("subscriber saw the stale event; want the newest")
	}
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	e := newTestEngine(t)
	events, unsubscribe := e.Subscribe()

	unsubscribe()
	unsubscribe() // a second call must not panic on an already-closed channel

	if _, open := <-events; open {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestCloseClosesSubscribers(t *testing.T) {
	e, err := New(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	events, _ := e.Subscribe()

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, open := <-events; open {
		t.Error("Close should close subscriber channels")
	}
}

func TestAdvanceStopsWhenPausedOrDone(t *testing.T) {
	tr := &tracked{total: 10 << 20}

	tr.advance(1)
	if tr.done == 0 {
		t.Fatal("a running torrent should make progress")
	}

	tr.paused = true
	at := tr.done
	tr.advance(1)
	if tr.done != at {
		t.Error("a paused torrent should not make progress")
	}
	if tr.rate != 0 {
		t.Error("a paused torrent should report no rate")
	}

	// Progress is capped at the total, however long we run for.
	tr.paused = false
	tr.advance(10000)
	if tr.done != tr.total {
		t.Errorf("done = %d, want it capped at total %d", tr.done, tr.total)
	}
}

func TestSnapshotState(t *testing.T) {
	tr := &tracked{id: "abc", total: 100}

	if got := tr.snapshot().State; got != StateDownloading {
		t.Errorf("incomplete torrent: State = %v, want downloading", got)
	}

	tr.done = tr.total
	if got := tr.snapshot().State; got != StateSeeding {
		t.Errorf("complete torrent: State = %v, want seeding", got)
	}

	// Paused outranks seeding for display, but Paused is the flag anything
	// making decisions should read.
	tr.paused = true
	snap := tr.snapshot()
	if snap.State != StatePaused || !snap.Paused {
		t.Errorf("paused torrent: State = %v, Paused = %v", snap.State, snap.Paused)
	}
}
