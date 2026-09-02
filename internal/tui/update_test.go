package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/engine"
)

// press feeds one key to the model, the way bubbletea would.
func press(m Model, key string) Model {
	var msg tea.KeyMsg
	switch key {
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func testModel(names ...string) Model {
	m := Model{
		keymap:   DefaultKeyMap(),
		selected: map[engine.TorrentID]bool{},
	}
	for _, n := range names {
		m.torrents = append(m.torrents, engine.TorrentSnapshot{
			ID: engine.TorrentID(n), Name: n, State: engine.StateDownloading,
		})
	}
	return m
}

func TestCursorMovesWithinBounds(t *testing.T) {
	m := testModel("a", "b", "c")

	m = press(m, "j")
	if m.cursor != 1 {
		t.Errorf("after j, cursor = %d, want 1", m.cursor)
	}

	// Already at the top: k must not run off the start.
	m.cursor = 0
	m = press(m, "k")
	if m.cursor != 0 {
		t.Errorf("k at the top moved the cursor to %d", m.cursor)
	}

	// Nor off the end.
	m.cursor = 2
	m = press(m, "j")
	if m.cursor != 2 {
		t.Errorf("j at the bottom moved the cursor to %d", m.cursor)
	}
}

func TestTopAndBottom(t *testing.T) {
	m := testModel("a", "b", "c")
	m.cursor = 1

	if m = press(m, "G"); m.cursor != 2 {
		t.Errorf("G put the cursor at %d, want 2", m.cursor)
	}
	if m = press(m, "g"); m.cursor != 0 {
		t.Errorf("g put the cursor at %d, want 0", m.cursor)
	}
}

// Marking a row advances, so a run can be selected by holding one key.
func TestSelectMarksAndAdvances(t *testing.T) {
	m := testModel("a", "b", "c")

	m = press(m, " ")
	if !m.selected["a"] {
		t.Error("space did not mark the cursor row")
	}
	if m.cursor != 1 {
		t.Errorf("space left the cursor at %d, want it advanced", m.cursor)
	}

	// And is a toggle.
	m.cursor = 0
	m = press(m, " ")
	if m.selected["a"] {
		t.Error("space did not unmark an already-marked row")
	}
}

// Torrents disappear while being looked at, so the cursor has to be
// corrected when the list shrinks - otherwise it points past the end and
// every action reads the wrong row, or none.
func TestCursorIsClampedWhenTorrentsVanish(t *testing.T) {
	m := testModel("a", "b", "c")
	m.cursor = 2

	next, _ := m.Update(engineEventMsg{Torrents: []engine.TorrentSnapshot{{ID: "a", Name: "a"}}})
	m = next.(Model)

	if m.cursor != 0 {
		t.Errorf("cursor = %d after the list shrank to one row", m.cursor)
	}
	if _, ok := m.cursorTorrent(); !ok {
		t.Error("cursor does not point at a torrent")
	}
}

// The cursor indexes the visible list, so a filter that hides the row it
// was on must not leave it dangling.
func TestCursorIsClampedByTheFilter(t *testing.T) {
	m := testModel("a", "b", "c")
	m.cursor = 2
	m.filter = filterSeeding // nothing here is seeding

	m.clampCursor(len(m.visibleTorrents()))

	if _, ok := m.cursorTorrent(); ok {
		t.Error("cursor points at a torrent when none is visible")
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d with an empty list, want 0", m.cursor)
	}
}

// An action applies to the marked torrents if any are marked, and to the
// row under the cursor otherwise. That rule is what lets the same keys
// work on one torrent or fifty with no separate mode.
func TestTargetsPrefersSelectionOverCursor(t *testing.T) {
	m := testModel("a", "b", "c")
	visible := m.visibleTorrents()

	m.cursor = 2
	got := m.targets(visible)
	if len(got) != 1 || got[0].Name != "c" {
		t.Errorf("with nothing marked, targets = %v, want just the cursor row", got)
	}

	m.selected["a"] = true
	m.selected["b"] = true
	got = m.targets(visible)
	if len(got) != 2 {
		t.Fatalf("with two marked, targets = %v", got)
	}
	// List order, not map order: a status message counting them, or a
	// partial failure, has to be reproducible.
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("targets came back in %v, want list order", got)
	}
}

func TestTargetsIsEmptyWhenNothingIsVisible(t *testing.T) {
	m := testModel()
	if got := m.targets(m.visibleTorrents()); len(got) != 0 {
		t.Errorf("targets = %v on an empty list", got)
	}
}

// Escape peels one layer at a time rather than clearing everything, so it
// is never a surprise.
func TestBackClearsSelectionThenFilter(t *testing.T) {
	m := testModel("a", "b")
	m.filter = filterDownloading
	m.selected["a"] = true

	m = press(m, "esc")
	if len(m.selected) != 0 {
		t.Error("first escape should clear the selection")
	}
	if m.filter != filterDownloading {
		t.Error("first escape should leave the filter alone")
	}

	m = press(m, "esc")
	if m.filter != filterAll {
		t.Error("second escape should clear the filter")
	}
}

func TestSearchNarrowsTheList(t *testing.T) {
	m := testModel("ubuntu.iso", "debian.iso", "ubuntu-server.iso")

	m = press(m, "/")
	if m.mode != modeSearch {
		t.Fatal("/ did not enter search mode")
	}
	for _, r := range "ubuntu" {
		m = press(m, string(r))
	}

	if got := len(m.visibleTorrents()); got != 2 {
		t.Errorf("searching \"ubuntu\" left %d torrents, want 2", got)
	}
}

// Matching ignores case: nobody types a torrent's capitalisation exactly.
func TestSearchIgnoresCase(t *testing.T) {
	m := testModel("Ubuntu.iso")
	m.searchQuery = "ubuntu"

	if got := len(m.visibleTorrents()); got != 1 {
		t.Errorf("case-insensitive search found %d torrents, want 1", got)
	}
}

// The search and the sidebar filter both apply - they intersect rather
// than one replacing the other.
func TestSearchAndFilterIntersect(t *testing.T) {
	m := testModel("ubuntu.iso", "ubuntu-server.iso")
	m.torrents[1].State = engine.StateSeeding
	m.torrents[1].TotalLength, m.torrents[1].Completed = 1, 1

	m.searchQuery = "ubuntu"
	m.filter = filterSeeding

	got := m.visibleTorrents()
	if len(got) != 1 || got[0].Name != "ubuntu-server.iso" {
		t.Errorf("search+filter gave %v, want only the seeding match", got)
	}
}

// Cancelling puts the list back, rather than leaving behind a filter that
// was never confirmed.
func TestEscapeCancelsSearch(t *testing.T) {
	m := testModel("a", "b")
	m = press(m, "/")
	m = press(m, "a")

	m = press(m, "esc")
	if m.mode != modeNormal {
		t.Error("escape did not leave search mode")
	}
	if m.searchQuery != "" {
		t.Errorf("escape left the query %q behind", m.searchQuery)
	}
}

// While typing, keys are text: "q" is a letter, not quit.
func TestKeysAreTextWhileSearching(t *testing.T) {
	m := testModel("a")
	m = press(m, "/")

	m = press(m, "q")
	if m.quitting {
		t.Fatal("typing q in a search quit the program")
	}
	if m.searchQuery != "q" {
		t.Errorf("query = %q, want it to contain the typed letter", m.searchQuery)
	}
}

// A paste arrives as one message carrying several runes. Anything that
// handles only single-rune messages silently drops the rest.
func TestSearchAcceptsMultiRuneInput(t *testing.T) {
	m := testModel("ubuntu.iso")
	m.mode = modeSearch

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ubuntu")})
	m = next.(Model)

	if m.searchQuery != "ubuntu" {
		t.Errorf("query = %q, want the whole pasted string", m.searchQuery)
	}
}

func TestHelpIsDismissedByAnyKey(t *testing.T) {
	m := testModel("a")

	m = press(m, "h")
	if !m.showHelp {
		t.Fatal("h did not open help")
	}

	m = press(m, "j")
	if m.showHelp {
		t.Error("a keypress did not close help")
	}
	if m.cursor != 0 {
		t.Error("the key that closed help should not also have moved the cursor")
	}
}

// The daemon reports torrents from a map and Go randomises map
// iteration, so without a sort the list reshuffles every tick with the
// cursor pointing at whatever lands under it.
func TestListOrderIsStableAcrossCalls(t *testing.T) {
	m := testModel("zulu", "alpha", "mike", "bravo", "yankee")

	first := names(m.visibleTorrents())
	for i := 0; i < 20; i++ {
		if got := names(m.visibleTorrents()); got != first {
			t.Fatalf("order changed between calls: %q then %q", first, got)
		}
	}
	if first != "alpha bravo mike yankee zulu" {
		t.Errorf("default order = %q, want it sorted by name", first)
	}
}

func TestSortModes(t *testing.T) {
	m := testModel("a", "b", "c")
	m.torrents[0].TotalLength = 300
	m.torrents[1].TotalLength = 100
	m.torrents[2].TotalLength = 200

	m.sortBy = sortSize
	if got := names(m.visibleTorrents()); got != "b c a" {
		t.Errorf("sorted by size = %q, want \"b c a\"", got)
	}

	m.sortDesc = true
	if got := names(m.visibleTorrents()); got != "a c b" {
		t.Errorf("descending by size = %q, want \"a c b\"", got)
	}
}

func TestParseSortMode(t *testing.T) {
	for _, name := range []string{"name", "size", "progress", "ratio", "eta", "added", "down", "up"} {
		if _, ok := ParseSortMode(name); !ok {
			t.Errorf("ParseSortMode(%q) was rejected", name)
		}
	}
	if _, ok := ParseSortMode("color"); ok {
		t.Error("ParseSortMode accepted a column that does not exist")
	}
}

func names(snaps []engine.TorrentSnapshot) string {
	out := make([]string, len(snaps))
	for i, s := range snaps {
		out[i] = s.Name
	}
	return strings.Join(out, " ")
}

// vim's dd: two presses remove, one does nothing.
func TestDDChordNeedsBothPresses(t *testing.T) {
	m := testModel("a", "b")

	m = press(m, "d")
	if !m.pendingDD {
		t.Fatal("the first d did not start a chord")
	}
	if m.quitting {
		t.Error("a single d should do nothing on its own")
	}

	// Anything else abandons it, so d-then-j is not half a deletion.
	m = press(m, "j")
	if m.pendingDD {
		t.Error("another key should abandon a half-typed chord")
	}
}

// A "d" from minutes ago must not turn the next one into a deletion.
func TestDDChordExpires(t *testing.T) {
	m := testModel("a", "b")
	m.pendingDD = true
	m.pendingAt = time.Now().Add(-time.Hour)

	next, cmd := m.handleListKey("d")
	m = next.(Model)

	if cmd != nil {
		t.Error("a stale chord should not have completed a removal")
	}
	if !m.pendingDD {
		t.Error("the stale d should have started a fresh chord")
	}
}

// A status message describes something that happened, so it has to go
// away. "rechecking 1 torrent(s)" outliving the recheck by however long
// the TUI stays open reads as a job that never finished.
func TestAStatusMessageExpires(t *testing.T) {
	m := testModel()

	next, cmd := m.Update(okStatus("rechecking 1 torrent(s)"))
	m = next.(Model)
	if m.status != "rechecking 1 torrent(s)" {
		t.Fatalf("status = %q, want the message", m.status)
	}
	if cmd == nil {
		t.Fatal("setting a status returned no command to clear it")
	}

	next, _ = m.Update(statusExpiredMsg{seq: m.statusSeq})
	m = next.(Model)

	if m.status != "" {
		t.Errorf("status = %q after expiring, want it cleared", m.status)
	}
}

// The timer for a message that has already been replaced must not wipe
// the one now on screen - two commands in quick succession is the
// ordinary case, not a rare one.
func TestAStaleExpiryLeavesTheCurrentMessage(t *testing.T) {
	m := testModel()

	next, _ := m.Update(okStatus("added 1 torrent(s)"))
	m = next.(Model)
	stale := statusExpiredMsg{seq: m.statusSeq}

	next, _ = m.Update(okStatus("rechecking 1 torrent(s)"))
	m = next.(Model)

	next, _ = m.Update(stale)
	m = next.(Model)

	if m.status != "rechecking 1 torrent(s)" {
		t.Errorf("status = %q, want the newer message to survive", m.status)
	}
}

// The command really does carry the sequence number it was made with,
// which is what stops a stale timer clearing a newer message. Run with a
// delay short enough to wait for.
func TestExpireStatusCmdCarriesItsSequence(t *testing.T) {
	msg := expireStatusCmd(7, time.Millisecond)()
	expired, ok := msg.(statusExpiredMsg)
	if !ok {
		t.Fatalf("expireStatusCmd produced %T, want statusExpiredMsg", msg)
	}
	if expired.seq != 7 {
		t.Errorf("seq = %d, want 7", expired.seq)
	}
}

// An error is the answer to "why did nothing happen", so it is worth
// reading twice - but it still goes away.
func TestAnErrorStaysLongerThanAConfirmation(t *testing.T) {
	m := testModel()

	if _, cmd := m.Update(okStatus("done")); cmd == nil {
		t.Error("a confirmation has no expiry")
	}
	if _, cmd := m.Update(errStatus(errors.New("nope"))); cmd == nil {
		t.Error("an error has no expiry")
	}
	if statusErrorTTL <= statusTTL {
		t.Errorf("error TTL %v should outlast the plain one %v", statusErrorTTL, statusTTL)
	}
}

// A lost daemon is a standing condition rather than an event, so it stays
// until it is replaced.
func TestALostConnectionDoesNotExpire(t *testing.T) {
	m := testModel()

	next, cmd := m.Update(engineClosedMsg{})
	m = next.(Model)

	if m.status == "" {
		t.Fatal("a lost connection said nothing")
	}
	if cmd != nil {
		t.Error("the lost-connection message was given an expiry")
	}
}

// A message too long to sit beside the transfer totals takes the line
// instead of being dropped. Every error that names a path is too long
// for an 80-column footer, and one that never appears is the same as no
// error at all.
func TestALongStatusIsShownRatherThanDropped(t *testing.T) {
	m := testModel("a")
	m.width, m.height = 80, 24
	p := layout(m.width, m.height)

	m.status = `unknown theme "nope" (built-in themes: [catppuccin dracula gruvbox nord plain])`
	m.statusIsErr = true

	footer := m.renderFooter(p)

	if !strings.Contains(footer, "unknown theme") {
		t.Errorf("a long status was dropped from the footer: %q", footer)
	}
	if w := lipgloss.Width(footer); w > p.footerW {
		t.Errorf("the footer is %d columns, want at most %d: %q", w, p.footerW, footer)
	}
}

// The folder key acts on the cursor row alone: eight marked torrents
// would mean eight windows, which is not what anyone means by "open it".
func TestOpenActsOnTheCursorRow(t *testing.T) {
	dir := t.TempDir()

	m := testModel("a", "b")
	m.opener = "true" // a real binary that exits immediately
	m.torrents[0].DataDir, m.torrents[0].SavePath = dir, dir
	m.selected = map[engine.TorrentID]bool{"b": true}

	msg := openStatus(t, m)
	if msg.isErr {
		t.Fatalf("o reported an error: %q", msg.text)
	}
	if !strings.Contains(msg.text, dir) {
		t.Errorf("opened %q, want the cursor row's folder %q", msg.text, dir)
	}
	if len(m.selected) != 1 {
		t.Error("opening a folder disturbed the selection")
	}
}

// A torrent that has downloaded nothing, or whose data was just purged,
// has no folder of its own - the save path does exist, and showing where
// the data is going to land beats an error about a directory the user
// never named.
func TestOpenFallsBackToTheSavePath(t *testing.T) {
	dir := t.TempDir()

	m := testModel("a")
	m.opener = "true"
	m.torrents[0].SavePath = dir
	m.torrents[0].DataDir = filepath.Join(dir, "not-downloaded-yet")

	msg := openStatus(t, m)
	if msg.isErr {
		t.Fatalf("o reported an error: %q", msg.text)
	}
	if !strings.Contains(msg.text, dir) || strings.Contains(msg.text, "not-downloaded-yet") {
		t.Errorf("opened %q, want the save path %q", msg.text, dir)
	}
}

// openStatus presses the folder key and returns what it reported.
func openStatus(t *testing.T, m Model) statusMsg {
	t.Helper()
	_, cmd := m.openCursorFolder()
	if cmd == nil {
		t.Fatal("the folder key produced no command")
	}
	msg, ok := cmd().(statusMsg)
	if !ok {
		t.Fatalf("the folder key produced %T, want a status", cmd())
	}
	return msg
}

// The speeds sit in fixed cells, so nothing to their right moves as the
// numbers change length. Without that the separator and the torrent count
// slid sideways every second, which is movement the eye follows and then
// has to dismiss.
func TestFooterSpeedsHaveAFixedWidth(t *testing.T) {
	p := layout(120, 30)

	rates := []struct {
		name string
		down float64
		up   float64
	}{
		{"idle", 0, 0},
		{"kilobytes", 367.6 * 1024, 0},
		// The widest either can be: format.Bytes switches unit at 1024, so
		// four digits before the point is as long as it gets.
		{"the widest rate there is", 1023.9 * 1024, 1023.9 * 1024 * 1024},
	}

	want := -1
	for _, r := range rates {
		m := testModel("a")
		m.global = engine.GlobalStats{DownloadBPS: r.down, UploadBPS: r.up, NumTorrents: 3}

		footer := m.renderFooter(p)
		got, ok := columnOf(footer, "│")
		if !ok {
			t.Fatalf("%s: no separator in the footer: %q", r.name, footer)
		}
		if want == -1 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("%s: the separator moved to column %d, want %d: %q",
				r.name, got, want, footer)
		}
	}
}

// A short one still shares the line with the totals, which are the
// footer's reason for existing the rest of the time.
func TestAShortStatusSharesTheFooter(t *testing.T) {
	m := testModel("a")
	m.width, m.height = 120, 30
	p := layout(m.width, m.height)
	m.status = "paused 1 torrent(s)"

	footer := m.renderFooter(p)

	if !strings.Contains(footer, "paused 1 torrent(s)") {
		t.Errorf("the status is missing: %q", footer)
	}
	if !strings.Contains(footer, "torrents") {
		t.Errorf("the totals are missing: %q", footer)
	}
	if w := lipgloss.Width(footer); w > p.footerW {
		t.Errorf("the footer is %d columns, want at most %d", w, p.footerW)
	}
}

// "?" is what someone who has never used this program presses, and "h"
// is what they press once they know it. Both have to reach the same
// screen, or the discoverable one is a key that appears to do nothing.
func TestBothHelpKeysOpenTheHelpScreen(t *testing.T) {
	for _, key := range []string{"h", "?"} {
		if m := press(testModel("a"), key); !m.showHelp {
			t.Errorf("%q did not open the help screen", key)
		}
	}
}

// The screen closes on any key, "?" included - it must not toggle itself
// straight back open.
func TestHelpClosesOnTheKeyThatOpenedIt(t *testing.T) {
	m := press(testModel("a"), "?")
	if m = press(m, "?"); m.showHelp {
		t.Error("pressing ? again left the help screen open")
	}
}

// The detail tabs used to be matched as literal "1"/"2"/"3" before focus
// dispatch ran, so a config that bound anything to a digit lost it with
// nothing to say why. They are bindings now, which is what makes the
// digit reachable for something else once the tab is moved off it.
func TestADigitFreedFromItsTabReachesTheActionBoundToIt(t *testing.T) {
	m := testModel("a", "b", "c")
	m.keymap.TabPieces = "f1" // move the tab off the digit
	m.keymap.Bottom = "1"     // and give the digit to something else

	next := press(m, "1")

	if next.cursor != 2 {
		t.Errorf("cursor = %d, want 2: the digit never reached the action bound to it", next.cursor)
	}
	if next.detailTab == tabPieces && m.detailTab != tabPieces {
		t.Error("the digit switched the detail tab it is no longer bound to")
	}
}

// And the tab still follows its binding wherever it is moved to.
func TestTheDetailTabFollowsItsBinding(t *testing.T) {
	m := testModel("a")
	m.keymap.TabPeers = "z"

	if got := press(m, "z").detailTab; got != tabPeers {
		t.Errorf("detailTab = %v, want tabPeers: the rebound key did nothing", got)
	}
}
