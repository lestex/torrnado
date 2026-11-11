package main

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
)

// fakeTorrentID pads a prefix out to a full 40-character id.
func fakeTorrentID(prefix string) engine.TorrentID {
	return engine.TorrentID(prefix + strings.Repeat("0", 40-len(prefix)))
}

func fullSnapshot() engine.TorrentSnapshot {
	return engine.TorrentSnapshot{
		ID: fakeTorrentID("6a66e9f7"), Name: "debian-12.iso",
		TotalLength: 4_000_000_000, Completed: 2_000_000_000, Progress: 0.5,
		DownloadBPS: 1_500_000.4, UploadBPS: 20_000.6,
		Downloaded: 2_100_000_000, Uploaded: 630_000_000, Ratio: 0.3,
		NumPeers: 12, NumSeeds: 4, ETA: 90 * time.Second,
		State:    engine.StateDownloading,
		SavePath: "/dl", DataDir: "/dl/debian",
		AddedAt:       time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
		DownloadLimit: 5_000_000, Label: "linux",
	}
}

// decodeList runs the real writer and reads back what a script would see.
func decodeList(t *testing.T, snaps ...engine.TorrentSnapshot) []map[string]any {
	t.Helper()
	var b bytes.Buffer
	if err := writeTorrentsJSON(&b, snaps); err != nil {
		t.Fatalf("writeTorrentsJSON: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(b.Bytes(), &out); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, b.String())
	}
	return out
}

func keysOf(m map[string]any) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}

// The key set is the contract. Pinning it means a field cannot be added,
// dropped or renamed by accident - only by someone who also changes this
// list, and therefore the documentation that sits beside it.
func TestListJSONKeysAreTheContract(t *testing.T) {
	got := keysOf(decodeList(t, fullSnapshot())[0])

	want := []string{
		"added_at", "check_progress", "completed", "data_dir", "download_limit",
		"download_rate", "downloaded", "error", "eta_seconds", "id", "label",
		"name", "paused", "peers", "progress", "ratio", "save_path", "seeds",
		"size", "state", "upload_limit", "upload_rate", "uploaded",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the listing's keys changed:\n got  %v\n want %v", got, want)
	}
}

func TestListJSONValuesAndUnits(t *testing.T) {
	m := decodeList(t, fullSnapshot())[0]

	for key, want := range map[string]any{
		"id":             string(fakeTorrentID("6a66e9f7")),
		"name":           "debian-12.iso",
		"state":          "downloading",
		"paused":         false,
		"progress":       0.5,             // a fraction, not 50
		"size":           4_000_000_000.0, // bytes
		"completed":      2_000_000_000.0,
		"download_rate":  1_500_000.0, // bytes per second, rounded
		"upload_rate":    20_001.0,
		"ratio":          0.3,
		"peers":          12.0,
		"seeds":          4.0,
		"eta_seconds":    90.0,
		"label":          "linux",
		"added_at":       "2026-11-01T12:00:00Z",
		"download_limit": 5_000_000.0,
		"upload_limit":   0.0,
		"error":          "",
	} {
		if m[key] != want {
			t.Errorf("%s = %v (%T), want %v", key, m[key], m[key], want)
		}
	}
}

// encoding/json refuses to encode an infinity - the whole listing fails,
// not just that field - and the engine reports +Inf for the ordinary case
// of seeding data that was already on disk. This is the failure that a
// plain marshal of the engine's own struct would have shipped.
func TestAnInfiniteRatioIsNullAndDoesNotSinkTheListing(t *testing.T) {
	if _, err := json.Marshal(math.Inf(1)); err == nil {
		t.Skip("encoding/json now encodes infinity; this guard has nothing left to guard")
	}

	seeding := fullSnapshot()
	seeding.Downloaded, seeding.Uploaded, seeding.Ratio = 0, 5_000, math.Inf(1)
	nan := fullSnapshot()
	nan.Ratio = math.NaN()

	for name, s := range map[string]engine.TorrentSnapshot{"+Inf": seeding, "NaN": nan} {
		rows := decodeList(t, s, fullSnapshot())
		if rows[0]["ratio"] != nil {
			t.Errorf("%s ratio = %v, want null", name, rows[0]["ratio"])
		}
		if rows[1]["ratio"] != 0.3 {
			t.Errorf("the other torrent in the listing was disturbed: %v", rows[1]["ratio"])
		}
	}
}

// Null is for "not known", used wherever zero would be a different answer.
func TestWhatIsNotKnownIsNullNotZero(t *testing.T) {
	s := fullSnapshot()
	s.ETA = 0               // negligible speed, or nothing left to wait for
	s.AddedAt = time.Time{} // a daemon too old to record it
	s.Checking = false

	m := decodeList(t, s)[0]

	for _, key := range []string{"eta_seconds", "added_at", "check_progress"} {
		if m[key] != nil {
			t.Errorf("%s = %v, want null", key, m[key])
		}
	}
}

func TestCheckProgressIsReportedOnlyWhileChecking(t *testing.T) {
	s := fullSnapshot()
	s.CheckProgress = 0.25

	if got := decodeList(t, s)[0]["check_progress"]; got != nil {
		t.Errorf("stale progress with no check running = %v, want null", got)
	}
	s.Checking = true
	if got := decodeList(t, s)[0]["check_progress"]; got != 0.25 {
		t.Errorf("while checking = %v, want 0.25", got)
	}
}

// State reports checking in preference to paused, so a paused torrent being
// rechecked reads as "checking". Paused has to be its own field or a script
// would conclude it was running.
func TestPausedIsItsOwnFieldBecauseStateCannotSayIt(t *testing.T) {
	s := fullSnapshot()
	s.State, s.Paused = engine.StateChecking, true

	m := decodeList(t, s)[0]

	if m["state"] != "checking" || m["paused"] != true {
		t.Errorf("state=%v paused=%v, want checking and true", m["state"], m["paused"])
	}
}

func TestEveryStateIsAStableToken(t *testing.T) {
	token := regexp.MustCompile(`^[a-z_]+$`)
	seen := map[string]bool{}
	for s := engine.State(0); s <= engine.StateLowDisk+1; s++ {
		got := stateToken(s)
		if !token.MatchString(got) {
			t.Errorf("state %d is %q, want lower-case words joined by underscores", s, got)
		}
		seen[got] = true
	}
	if !seen["low_disk"] || seen["low disk"] {
		t.Errorf("the low-disk state should be low_disk: %v", seen)
	}
}

func TestAnEmptyListingIsAnArrayNotNull(t *testing.T) {
	var b bytes.Buffer
	if err := writeTorrentsJSON(&b, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(b.String()); got != "[]" {
		t.Errorf("got %q, want []", got)
	}
}

func TestFloatNoiseDoesNotReachAScript(t *testing.T) {
	s := fullSnapshot()
	s.Ratio, s.Progress = 0.30000000000000004, 1.0/3.0

	m := decodeList(t, s)[0]

	if m["ratio"] != 0.3 || m["progress"] != 0.3333 {
		t.Errorf("ratio=%v progress=%v, want 0.3 and 0.3333", m["ratio"], m["progress"])
	}
}

// ---- status

func decodeStatus(t *testing.T, st statusJSON) map[string]any {
	t.Helper()
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

var statusNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func TestStatusJSONKeysAreTheContract(t *testing.T) {
	got := keysOf(decodeStatus(t, buildStatusJSON("/s", ipc.DaemonStatus{}, engine.GlobalStats{}, false, statusNow)))

	want := []string{"answering", "disk", "listen_port", "lock", "pid", "running",
		"socket", "started_at", "transfers", "uptime_seconds", "version", "vpn"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("status keys changed:\n got  %v\n want %v", got, want)
	}
}

func TestStatusWhenNothingIsRunning(t *testing.T) {
	st := buildStatusJSON("/run/torrnado.sock", ipc.DaemonStatus{LockPath: "/run/torrnado.sock.lock"},
		engine.GlobalStats{}, false, statusNow)
	m := decodeStatus(t, st)

	if m["running"] != false || m["answering"] != false {
		t.Errorf("running=%v answering=%v, want both false", m["running"], m["answering"])
	}
	if m["socket"] != "/run/torrnado.sock" || m["lock"] != "/run/torrnado.sock.lock" {
		t.Errorf("it must say where it looked: socket=%v lock=%v", m["socket"], m["lock"])
	}
	for _, key := range []string{"pid", "version", "started_at", "uptime_seconds", "listen_port", "transfers", "vpn", "disk"} {
		if m[key] != nil {
			t.Errorf("%s = %v, want null with nothing running", key, m[key])
		}
	}
}

// The state that gets mistaken for a dead daemon, and acting on that mistake
// means two engines on one data directory - so it is its own answer.
func TestARunningDaemonThatDoesNotAnswerIsNotADeadOne(t *testing.T) {
	m := decodeStatus(t, buildStatusJSON("/s", ipc.DaemonStatus{Running: true, PID: 4242, LockPath: "/s.lock"},
		engine.GlobalStats{}, false, statusNow))

	if m["running"] != true || m["answering"] != false {
		t.Errorf("running=%v answering=%v, want true and false", m["running"], m["answering"])
	}
	if m["pid"] != 4242.0 {
		t.Errorf("pid = %v, want 4242", m["pid"])
	}
	if m["version"] != nil || m["transfers"] != nil {
		t.Errorf("a daemon that did not answer must not be credited with answers: %v", m)
	}
}

func TestAnAnsweringDaemonReportsItself(t *testing.T) {
	g := engine.GlobalStats{
		Version: "0.10.1 (da4134de07be)", StartedAt: statusNow.Add(-90 * time.Second),
		ListenPort: 51413, NumTorrents: 2, DownloadBPS: 1000.4, UploadBPS: 500.6,
		DownloadLimit: 0, UploadLimit: 250_000,
		VPNRequired: true, VPNActive: true, VPNInterface: "utun4",
		DiskFreeBytes: 100, DiskTotalBytes: 1000, DiskLow: true, MinFreeSpace: 200,
	}
	m := decodeStatus(t, buildStatusJSON("/s", ipc.DaemonStatus{Running: true, PID: 7}, g, true, statusNow))

	if m["answering"] != true || m["version"] != "0.10.1 (da4134de07be)" || m["listen_port"] != 51413.0 {
		t.Errorf("identity: %v", m)
	}
	if m["uptime_seconds"] != 90.0 || m["started_at"] != "2026-11-10T11:58:30Z" {
		t.Errorf("uptime_seconds=%v started_at=%v", m["uptime_seconds"], m["started_at"])
	}
	tr := m["transfers"].(map[string]any)
	if tr["torrents"] != 2.0 || tr["download_rate"] != 1000.0 || tr["upload_rate"] != 501.0 || tr["upload_limit"] != 250000.0 {
		t.Errorf("transfers: %v", tr)
	}
	if vpn := m["vpn"].(map[string]any); vpn["required"] != true || vpn["active"] != true || vpn["interface"] != "utun4" {
		t.Errorf("vpn: %v", vpn)
	}
	if d := m["disk"].(map[string]any); d["free_bytes"] != 100.0 || d["total_bytes"] != 1000.0 || d["low"] != true || d["min_free_bytes"] != 200.0 {
		t.Errorf("disk: %v", d)
	}
}

// A daemon outlives its clients, so an old one answering a new CLI is
// ordinary. What it cannot say is null, not a made-up zero.
func TestADaemonTooOldToReportItselfGivesNullsNotInventions(t *testing.T) {
	m := decodeStatus(t, buildStatusJSON("/s", ipc.DaemonStatus{Running: true, PID: 7},
		engine.GlobalStats{NumTorrents: 3}, true, statusNow))

	for _, key := range []string{"version", "started_at", "uptime_seconds", "listen_port", "disk"} {
		if m[key] != nil {
			t.Errorf("%s = %v, want null from a daemon that cannot report it", key, m[key])
		}
	}
	if tr := m["transfers"].(map[string]any); tr["torrents"] != 3.0 {
		t.Errorf("what it did report should still show: %v", tr)
	}
}

func TestAnUnpidedDaemonHasANullPID(t *testing.T) {
	// A daemon from before the pid was recorded.
	m := decodeStatus(t, buildStatusJSON("/s", ipc.DaemonStatus{Running: true}, engine.GlobalStats{}, true, statusNow))
	if m["pid"] != nil {
		t.Errorf("pid = %v, want null rather than 0", m["pid"])
	}
}

func TestThePIDAndAnswerCannotContradictRunning(t *testing.T) {
	// Not running, yet "answering": the probe should never have been made,
	// but if it somehow was, nothing running cannot be answering.
	m := decodeStatus(t, buildStatusJSON("/s", ipc.DaemonStatus{Running: false}, engine.GlobalStats{Version: "x"}, true, statusNow))
	if m["answering"] != false || m["version"] != nil {
		t.Errorf("a daemon that is not running answered: %v", m)
	}
}

func TestContradictoryFlagsAreRefused(t *testing.T) {
	if err := checkListFlags(true, true); err == nil || !strings.Contains(err.Error(), "--watch") {
		t.Errorf("--json with --watch: %v", err)
	}
	if err := checkListFlags(true, false); err != nil {
		t.Errorf("--json alone refused: %v", err)
	}
	if err := checkListFlags(false, true); err != nil {
		t.Errorf("--watch alone refused: %v", err)
	}
	if err := checkStatusFlags(true, true); err == nil || !strings.Contains(err.Error(), "--quiet") {
		t.Errorf("--quiet with --json: %v", err)
	}
	if err := checkStatusFlags(false, true); err != nil || checkStatusFlags(true, false) != nil {
		t.Errorf("a single flag refused")
	}
}
