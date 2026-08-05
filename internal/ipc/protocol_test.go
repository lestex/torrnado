package ipc

import (
	"bytes"
	"encoding/gob"
	"testing"
	"time"

	"github.com/lestex/torrnado/internal/engine"
)

// roundTrip encodes a message and decodes it back, the way the socket
// does. Everything on this wire is hand-rolled, so it is worth proving
// each kind survives the trip intact.
func roundTrip(t *testing.T, in message) message {
	t.Helper()

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(in); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var out message
	if err := gob.NewDecoder(&buf).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestRoundTripCall(t *testing.T) {
	in := message{Kind: kindCall, Req: &Request{
		Seq:    7,
		Method: MethodAddMagnet,
		Source: "magnet:?xt=urn:btih:abc",
		Opts:   engine.AddOpts{SavePath: "/tmp/x", Paused: true},
	}}

	out := roundTrip(t, in)

	if out.Kind != kindCall || out.Req == nil {
		t.Fatalf("kind = %v, req = %v", out.Kind, out.Req)
	}
	if out.Req.Seq != 7 || out.Req.Method != MethodAddMagnet {
		t.Errorf("got seq %d method %q", out.Req.Seq, out.Req.Method)
	}
	if !out.Req.Opts.Paused || out.Req.Opts.SavePath != "/tmp/x" {
		t.Errorf("AddOpts did not survive: %+v", out.Req.Opts)
	}
}

func TestRoundTripReply(t *testing.T) {
	in := message{Kind: kindReply, Resp: &Response{
		Seq: 7,
		OK:  true,
		Snapshot: []engine.TorrentSnapshot{{
			ID:    "abc",
			Name:  "Example",
			State: engine.StateDownloading,
			ETA:   90 * time.Second,
		}},
	}}

	out := roundTrip(t, in)

	if out.Resp == nil || len(out.Resp.Snapshot) != 1 {
		t.Fatalf("snapshot did not survive: %+v", out.Resp)
	}
	got := out.Resp.Snapshot[0]
	if got.Name != "Example" || got.State != engine.StateDownloading || got.ETA != 90*time.Second {
		t.Errorf("snapshot changed in transit: %+v", got)
	}
}

func TestRoundTripEvent(t *testing.T) {
	in := message{Kind: kindEvent, Event: &engine.Event{
		Torrents: []engine.TorrentSnapshot{{ID: "abc", Progress: 0.5}},
		Global:   engine.GlobalStats{NumTorrents: 1, DownloadBPS: 1024},
	}}

	out := roundTrip(t, in)

	if out.Kind != kindEvent || out.Event == nil {
		t.Fatalf("kind = %v, event = %v", out.Kind, out.Event)
	}
	if out.Event.Global.NumTorrents != 1 || out.Event.Torrents[0].Progress != 0.5 {
		t.Errorf("event changed in transit: %+v", out.Event)
	}
}

// gob omits zero-valued fields, so an unset pointer decodes as nil rather
// than as an empty struct. Callers rely on that to tell the kinds apart.
func TestUnsetFieldsDecodeAsNil(t *testing.T) {
	out := roundTrip(t, message{Kind: kindCall, Req: &Request{Method: MethodPing}})

	if out.Resp != nil {
		t.Error("Resp should be nil on a call")
	}
	if out.Event != nil {
		t.Error("Event should be nil on a call")
	}
}
