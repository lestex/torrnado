package engine

import (
	"testing"
	"time"
)

// newTestEngine builds an engine writing into a directory the testing
// package creates and removes for us.
func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(testConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

// testConfig keeps the tests off the network: no peer discovery, and a
// port chosen by the OS so parallel runs cannot collide.
func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{DataDir: t.TempDir(), DisableDHT: true, DisablePEX: true}
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
	e, err := New(testConfig(t))
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
