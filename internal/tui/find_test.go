package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/indexer"
)

// fakeFinder stands in for the daemon. The model must never reach the
// network in a test, and Search is never actually called: the tests
// inspect the command rather than running it, as palette_test does.
type fakeFinder struct{}

func (fakeFinder) Search(string) ([]indexer.Result, []string, error) {
	return nil, nil, errors.New("fakeFinder.Search should not run in a test")
}

func findModel(results ...indexer.Result) Model {
	m := testModel("existing-torrent")
	m.width, m.height = 120, 40
	m.finder = fakeFinder{}
	// What makes search available is the daemon naming a source, not
	// the client existing - see canFind.
	m.findSources = []string{"jackett"}
	if results != nil {
		m.findState = findLoaded
		m.findResults = results
		m.findQuery = "ubuntu"
	}
	return m
}

func sampleResults() []indexer.Result {
	return []indexer.Result{
		{Title: "ubuntu-24.04.iso", Size: 6203355136, Seeders: 1243, Leechers: 87,
			Tracker: "ubuntu-official", Link: "magnet:?xt=urn:btih:aaa"},
		{Title: "quiet tracker", Size: 1024, Seeders: indexer.Unknown, Leechers: indexer.Unknown,
			Tracker: "someplace", Link: "http://x/dl/bbb"},
		{Title: "nothing to fetch", Seeders: 0, Leechers: 0, Tracker: "deadsite", Link: ""},
	}
}

// Most installations have no indexer, so this is the common path rather
// than an edge case, and refusing without saying what to add would leave
// the user nowhere to go.
func TestFindWithNoIndexerSaysWhatToConfigure(t *testing.T) {
	m := testModel("a")
	m.finder = fakeFinder{}
	m.findSources = nil // the daemon has no indexer configured

	next := press(m, m.keymap.Find)
	if next.findState != findClosed {
		t.Error("the overlay should not open with no indexer configured")
	}
	if !next.statusIsErr {
		t.Error("want an error status")
	}
	for _, want := range []string{"indexer.url", "indexer.api_key"} {
		if !strings.Contains(next.status, want) {
			t.Errorf("status should mention %q, got %q", want, next.status)
		}
	}
}

func TestFindPromptCollectsAQuery(t *testing.T) {
	m := findModel()

	m = press(m, m.keymap.Find)
	if m.mode != modeFind {
		t.Fatalf("mode = %v, want modeFind", m.mode)
	}

	for _, k := range []string{"u", "b", "u"} {
		m = press(m, k)
	}
	m = press(m, " ")
	m = press(m, "x")
	if m.findQuery != "ubu x" {
		t.Errorf("findQuery = %q, want %q", m.findQuery, "ubu x")
	}

	// Backspace, then escape, which abandons the whole thing.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)
	if m.findQuery != "ubu " {
		t.Errorf("after backspace findQuery = %q", m.findQuery)
	}

	m = press(m, "esc")
	if m.mode != modeNormal || m.findQuery != "" {
		t.Errorf("esc should leave the prompt and clear it, got mode %v query %q", m.mode, m.findQuery)
	}
}

// An empty query would be a request the indexer cannot answer, so enter
// does nothing rather than opening an empty window.
func TestFindPromptIgnoresAnEmptyQuery(t *testing.T) {
	m := findModel()
	m = press(m, m.keymap.Find)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)
	if got.findState != findClosed {
		t.Error("an empty query should not open the overlay")
	}
	if cmd != nil {
		t.Error("an empty query should not run a search")
	}
}

// The overlay opens before the reply arrives, because a search takes
// seconds and the footer status expires in five.
func TestSubmittingAQueryOpensTheOverlayLoading(t *testing.T) {
	m := findModel()
	m = press(m, m.keymap.Find)
	for _, k := range []string{"u", "b"} {
		m = press(m, k)
	}

	before := m.findSeq
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	if got.findState != findLoading {
		t.Errorf("findState = %v, want findLoading", got.findState)
	}
	if got.mode != modeNormal {
		t.Errorf("mode = %v, want to be out of the prompt", got.mode)
	}
	if got.findSeq == before {
		t.Error("findSeq should advance with each request")
	}
	if cmd == nil {
		t.Error("want a command to run the search")
	}
	if !strings.Contains(got.View(), "searching") {
		t.Error("the overlay should say it is working")
	}
}

// A reply for a request that has been superseded has to be dropped, or a
// slow first search would overwrite a faster second one.
func TestAStaleReplyIsIgnored(t *testing.T) {
	m := findModel()
	m.findState = findLoading
	m.findSeq = 7

	next, _ := m.Update(findResultsMsg{seq: 6, query: "old", results: sampleResults()})
	got := next.(Model)

	if got.findState != findLoading {
		t.Error("a superseded reply should not change the overlay")
	}
	if len(got.findResults) != 0 {
		t.Error("a superseded reply should not load results")
	}
}

// Walking away has to stick. Esc bumps the sequence for exactly this:
// the search is still running and will answer, and the window must not
// reopen on its own once the user has moved on.
func TestAReplyAfterEscapeDoesNotReopenTheOverlay(t *testing.T) {
	m := findModel()
	m.findState = findLoading
	inFlight := m.findSeq

	m = press(m, "esc")
	if m.findState != findClosed {
		t.Fatal("esc should close the overlay")
	}

	next, _ := m.Update(findResultsMsg{seq: inFlight, results: sampleResults()})
	if got := next.(Model); got.findState != findClosed {
		t.Error("a reply arriving after esc reopened the overlay")
	}
}

func TestResultsLoadAndTheCursorMoves(t *testing.T) {
	m := findModel()
	m.findState = findLoading

	next, _ := m.Update(findResultsMsg{seq: m.findSeq, query: "ubuntu", results: sampleResults()})
	m = next.(Model)
	if m.findState != findLoaded || len(m.findResults) != 3 {
		t.Fatalf("results did not load: state %v, %d results", m.findState, len(m.findResults))
	}

	m = press(m, m.keymap.Down)
	if m.findCursor != 1 {
		t.Errorf("findCursor = %d, want 1", m.findCursor)
	}
	m = press(m, m.keymap.Bottom)
	if m.findCursor != 2 {
		t.Errorf("findCursor = %d, want the last row", m.findCursor)
	}
	// Clamped at both ends rather than wrapping or running off.
	m = press(m, m.keymap.Down)
	if m.findCursor != 2 {
		t.Errorf("findCursor = %d, want it to stop at the end", m.findCursor)
	}
	m = press(m, m.keymap.Top)
	if m.findCursor != 0 {
		t.Errorf("findCursor = %d, want 0", m.findCursor)
	}
	m = press(m, m.keymap.Up)
	if m.findCursor != 0 {
		t.Errorf("findCursor = %d, want it to stop at the start", m.findCursor)
	}
}

// The regression test for the whole overlay idea: a key that leaked
// through would act on the list behind the window. With no client in the
// model, anything reaching an action would panic, which makes this a
// strong assertion as well as a cheap one.
func TestTheOverlaySwallowsKeys(t *testing.T) {
	m := findModel(sampleResults()...)
	m.cursor = 0

	for _, key := range []string{"j", "k", "x", "D", "p", "r", "/", ":", "g", "G", "v", "o", "m", "1"} {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		got := next.(Model)
		if got.cursor != 0 {
			t.Errorf("%q moved the torrent cursor to %d", key, got.cursor)
		}
		if got.mode != modeNormal {
			t.Errorf("%q opened a prompt behind the overlay", key)
		}
		if got.findState == findClosed {
			t.Errorf("%q closed the overlay", key)
		}
	}
}

// Space means "mark" everywhere else, and a torrent added by a stray
// keystroke while scrolling is a surprise. Enter is the only way.
func TestSpaceDoesNotAddAResult(t *testing.T) {
	m := findModel(sampleResults()...)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if got := next.(Model); got.findState == findClosed {
		t.Error("space closed the overlay as though it had added something")
	}
	if cmd != nil {
		t.Error("space produced a command")
	}
}

func TestEnterOnAResultWithNoLinkKeepsTheOverlayOpen(t *testing.T) {
	m := findModel(sampleResults()...)
	m.findCursor = 2 // the row with no link

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	if got.findState == findClosed {
		t.Error("the overlay should stay open so another row can be chosen")
	}
	if !got.statusIsErr {
		t.Error("want an error status explaining there is nothing to add")
	}
	if cmd == nil {
		t.Error("want the status command")
	}
}

func TestEnterAddsTheHighlightedResult(t *testing.T) {
	m := findModel(sampleResults()...)
	m.findCursor = 0

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	if got.findState != findClosed {
		t.Error("the overlay should close once a result is taken")
	}
	if cmd == nil {
		t.Fatal("want a command that adds the torrent")
	}
	if got.statusIsErr {
		t.Errorf("status should not be an error, got %q", got.status)
	}
	if !strings.Contains(got.status, "adding") {
		t.Errorf("status = %q, want it to say what is happening", got.status)
	}
}

// The one-keystroke fix for a typo: search again with the last query
// already in the buffer.
func TestFindKeyReopensThePromptWithTheLastQuery(t *testing.T) {
	m := findModel(sampleResults()...)
	m.findQuery = "ubntu"

	m = press(m, m.keymap.Find)
	if m.mode != modeFind {
		t.Errorf("mode = %v, want modeFind", m.mode)
	}
	if m.findQuery != "ubntu" {
		t.Errorf("findQuery = %q, want the previous query kept for editing", m.findQuery)
	}
	if m.findState != findClosed {
		t.Error("the results should be dismissed while retyping")
	}
}

// An unreported count and a real zero must not look alike: zero seeders
// means a torrent that will never finish.
func TestRenderedResultsDistinguishUnknownFromZero(t *testing.T) {
	m := findModel(sampleResults()...)
	view := stripANSI(m.View())

	if !strings.Contains(view, "ubuntu-24.04.iso") {
		t.Fatalf("results not rendered:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "quiet tracker") && !strings.Contains(line, "-") {
			t.Errorf("an unreported count should render as -, got %q", line)
		}
		if strings.Contains(line, "nothing to fetch") {
			if !strings.Contains(line, "0") {
				t.Errorf("a real zero should render as 0, got %q", line)
			}
			if !strings.Contains(line, "no link") {
				t.Errorf("a result with nothing to add should say so, got %q", line)
			}
		}
	}
}

// An indexer error already carries the remedy, so it is shown rather
// than summarised - and it has to fit in the box.
func TestAnErrorIsShownInTheOverlay(t *testing.T) {
	m := findModel()
	m.findState = findLoaded
	m.findQuery = "ubuntu"
	m.findErr = errors.New("jackett: cannot reach http://localhost:9117 (connection refused) - " +
		"is it running, and is indexer.url right?")

	view := stripANSI(m.View())
	if !strings.Contains(view, "indexer.url") {
		t.Errorf("the overlay should show the error's advice:\n%s", view)
	}
}

// lipgloss Height() is a minimum, not a maximum, so content wider or
// taller than the frame pushes it off the screen. Checked at both a
// cramped and a roomy size.
func TestTheOverlayFitsTheTerminal(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}, {200, 60}} {
		many := make([]indexer.Result, 60)
		for i := range many {
			many[i] = indexer.Result{
				Title:   strings.Repeat("a-very-long-torrent-name", 6),
				Tracker: "a-tracker-with-a-long-name",
				Seeders: i, Leechers: i, Size: 1 << 30,
				Link: "magnet:?xt=urn:btih:aaa",
			}
		}

		m := findModel(many...)
		m.width, m.height = size.w, size.h

		box, _, _ := m.renderFindResults()
		for i, line := range strings.Split(box, "\n") {
			if w := lipgloss.Width(line); w > size.w {
				t.Errorf("%dx%d: box line %d is %d cells wide", size.w, size.h, i, w)
			}
		}
		if h := lipgloss.Height(box); h > size.h-1 {
			t.Errorf("%dx%d: box is %d rows tall, want at most %d", size.w, size.h, h, size.h-1)
		}

		// And the composed frame, which is what actually reaches the
		// terminal.
		for i, line := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(stripANSI(line)); w > size.w {
				t.Errorf("%dx%d: view line %d is %d cells wide", size.w, size.h, i, w)
			}
		}
	}
}

// Below the threshold the trailing columns go rather than squeezing the
// title to nothing.
func TestFindColumns(t *testing.T) {
	if _, wide := findColumns(120); !wide {
		t.Error("120 columns should fit the whole table")
	}
	titleW, wide := findColumns(30)
	if wide {
		t.Error("30 columns should drop the trailing columns")
	}
	if titleW < 1 {
		t.Errorf("titleW = %d, want at least 1", titleW)
	}
	// Never negative, whatever it is handed.
	if w, _ := findColumns(2); w < 1 {
		t.Errorf("findColumns(2) = %d, want at least 1", w)
	}
}

// The prompt sigil is a word here, where every other prompt is one cell,
// so the room arithmetic has to measure it rather than assume.
func TestPromptFitsAMultiCellSigil(t *testing.T) {
	m := findModel()
	for _, width := range []int{80, 40, 12, 6} {
		got := stripANSI(m.renderPrompt(findPrompt, strings.Repeat("x", 200), width))
		if w := lipgloss.Width(got); w > width {
			t.Errorf("width %d: prompt rendered %d cells wide: %q", width, w, got)
		}
	}
}

// :find with no argument opens the prompt; with one it searches.
func TestPaletteFind(t *testing.T) {
	m := findModel()
	next, _ := m.execCommand("find")
	if got := next.(Model); got.mode != modeFind {
		t.Errorf("mode = %v, want the prompt open", got.mode)
	}

	m = findModel()
	next, cmd := m.execCommand("find ubuntu 24.04")
	got := next.(Model)
	if got.findState != findLoading {
		t.Errorf("findState = %v, want findLoading", got.findState)
	}
	// Joined with spaces: a query is a phrase.
	if got.findQuery != "ubuntu 24.04" {
		t.Errorf("findQuery = %q, want the words joined", got.findQuery)
	}
	if cmd == nil {
		t.Error("want a command to run the search")
	}
}
