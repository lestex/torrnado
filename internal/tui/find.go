package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/format"
	"github.com/lestex/torrnado/internal/indexer"
)

// The find overlay asks a configured indexer - Jackett or Prowlarr - for
// torrents matching a query, and adds the one chosen through the same
// path as :add. It is a floating window over the panes rather than a
// takeover of the list, which keeps the sidebar and the detail pane
// describing the torrents that are really there: a detail pane still
// captioning a torrent hidden behind a list of search results would be
// actively misleading.

// finder is what the overlay asks. Narrower than indexer.Searcher on
// purpose: the daemon does the searching now, so all a client needs is
// the one call, and *ipc.Client satisfies this without the interface
// having to describe anything a client cannot do.
type finder interface {
	// Search returns results, then one message per source that failed -
	// a search across several sources can half work.
	Search(query string) ([]indexer.Result, []string, error)
}

// findState is what the overlay is doing. Three states rather than a
// bool, because "open and waiting" has to be drawn differently from
// "open with results" and a search can take tens of seconds.
type findState int

const (
	findClosed findState = iota
	findLoading
	findLoaded
)

// findPrompt is the sigil for modeFind. Deliberately a word, so it
// cannot be mistaken for "/" (which filters the list that is loaded) or
// ":" (which runs a command).
const findPrompt = "find:"

// openFind starts the prompt, or explains that there is nothing to ask.
func (m Model) openFind() (tea.Model, tea.Cmd) {
	if !m.canFind() {
		cmd := m.setStatus(errStatus(errNoIndexer()))
		return m, cmd
	}
	m.mode = modeFind
	return m, nil
}

// canFind reports whether there is anything to search.
//
// Two conditions, because they fail differently: no client at all (a
// model built without one, in tests), and a daemon that named no
// sources - which is the ordinary case, since the feature is opt-in.
// The client itself is always present once attached, so its presence
// says nothing about whether an indexer is configured.
func (m Model) canFind() bool {
	return m.finder != nil && len(m.findSources) > 0
}

// errNoIndexer says what to add to config.toml. Most users will not have
// a search source, so this is the common answer rather than an edge
// case, and "not configured" on its own would leave them nowhere to go.
//
// The wording is indexer's, not ours: this message used to be its own
// copy and named the keys of the [indexer] table long after [search.*]
// replaced it.
func errNoIndexer() error {
	return errors.New(indexer.NotConfiguredMsg)
}

// startFind fires a query and opens the overlay in its loading state.
//
// The overlay opens before the reply arrives on purpose. The footer's
// status line expires after a few seconds while a search can take tens
// of them, so a busy message there would vanish and leave the interface
// looking idle; a state the overlay renders has no timer on it. This is
// the same reason losing the daemon sets the status directly rather than
// through setStatus - a request in flight is a standing condition, not an
// event that has passed.
func (m Model) startFind(query string) (tea.Model, tea.Cmd) {
	query = strings.TrimSpace(query)
	if query == "" {
		// Nothing to search for. Leave the prompt as it is rather than
		// opening an empty window.
		return m, nil
	}
	if !m.canFind() {
		cmd := m.setStatus(errStatus(errNoIndexer()))
		return m, cmd
	}

	m.mode = modeNormal
	m.findQuery = query
	m.findState = findLoading
	m.findResults = nil
	m.findErr = nil
	m.findCursor = 0
	m.findSeq++

	return m, findCmd(m.finder, query, m.findSeq)
}

// findCmd runs the search off the update loop.
//
// The sequence number travels with the request and comes back with the
// reply, which is how a stale answer is recognised - the same guard the
// detail pane uses when the cursor has moved on. It has to be a number
// rather than the query text: closing the overlay also bumps it, so a
// reply the user has walked away from cannot reopen the window, and two
// searches for the same words stay distinguishable.
func findCmd(f finder, query string, seq int) tea.Cmd {
	return func() tea.Msg {
		results, errs, err := f.Search(query)
		return findResultsMsg{seq: seq, query: query, results: results, warnings: errs, err: err}
	}
}

// applyFindResults takes a reply, ignoring one that has been superseded.
func (m Model) applyFindResults(msg findResultsMsg) (Model, tea.Cmd) {
	if msg.seq != m.findSeq {
		return m, nil
	}
	m.findState = findLoaded
	m.findResults = msg.results
	m.findWarnings = msg.warnings
	m.findErr = msg.err
	m.findCursor = 0
	return m, nil
}

// closeFind hides the overlay.
//
// Bumping the sequence is what makes walking away stick: a search still
// in flight will answer eventually, and without this the window would
// reopen on its own.
func (m Model) closeFind() Model {
	m.findState = findClosed
	m.findSeq++
	return m
}

// handleFindKey routes a keystroke while the overlay is open, and takes
// every key for the same reason the theme picker does.
func (m Model) handleFindKey(key string) (tea.Model, tea.Cmd) {
	km := m.keymap

	switch key {
	case "esc", km.Back:
		return m.closeFind(), nil

	// Searching again, with the last query already in the buffer, which
	// is the one-keystroke fix for a typo.
	case km.Find:
		m = m.closeFind()
		m.mode = modeFind
		return m, nil

	case "enter":
		return m.addFindResult()

	case km.Up, "up":
		m.findCursor = max(0, m.findCursor-1)
		return m, nil

	case km.Down, "down":
		if n := len(m.findResults); n > 0 {
			m.findCursor = min(n-1, m.findCursor+1)
		}
		return m, nil

	case km.Top:
		m.findCursor = 0
		return m, nil

	case km.Bottom:
		if n := len(m.findResults); n > 0 {
			m.findCursor = n - 1
		}
		return m, nil
	}

	// Everything else is swallowed. A key reaching the panes behind the
	// window would act on a list the user cannot see.
	return m, nil
}

// addFindResult hands the highlighted result to the add path.
//
// Note what is not here: no save-path form, no file picking, no confirm
// step. The link goes to addBatchCmd, which is the same code :add runs,
// so a magnet is passed through and an http(s) .torrent is fetched to a
// temp file first - and the result lands in the list through the same
// machinery that clears a filter hiding a newly added torrent.
//
// Space is deliberately not bound to this. It means "mark" everywhere
// else in the interface, and a torrent added by a stray keystroke while
// scrolling is a surprise worth avoiding; enter is the only way.
func (m Model) addFindResult() (tea.Model, tea.Cmd) {
	if m.findCursor >= len(m.findResults) {
		return m, nil
	}
	r := m.findResults[m.findCursor]

	if r.Link == "" {
		// Listed so the count agrees with what the indexer reported, but
		// there is nothing to add. The overlay stays open so another row
		// can be chosen.
		cmd := m.setStatus(errStatus(fmt.Errorf("that result has no magnet or download link")))
		return m, cmd
	}

	m = m.closeFind()
	cmd := m.setStatus(okStatus("adding " + truncate(r.Title, 60)))
	return m, tea.Batch(cmd, addBatchCmd(m.client, []string{r.Link}))
}

// Column widths for the results table. Fixed but for the title, the way
// the torrent list does it, so the rows line up without measuring every
// result first.
const (
	colFindNum     = 3
	colFindSize    = 9
	colFindPeers   = 6
	colFindTracker = 16
	minFindTitle   = 24
)

// findColumns divides the width between the title and the rest, and says
// whether there is room for the rest at all.
//
// Below the threshold the trailing columns are dropped rather than
// squeezed, which is what nameWidth does for the torrent list: a title
// truncated to nothing tells you less than a title with no tracker
// beside it.
func findColumns(contentW int) (titleW int, wide bool) {
	fixed := colFindNum + 1 + colFindSize + 1 + colFindPeers + 1 + colFindPeers + 1 + colFindTracker + 1
	if w := contentW - fixed; w >= minFindTitle {
		return w, true
	}
	return max(1, contentW-colFindNum-1), false
}

// renderFindResults draws the floating box, returning it and the
// top-left corner to splice it at - the same contract as
// renderThemePicker, and the same compositing primitives.
func (m Model) renderFindResults() (box string, x, y int) {
	// Near the full width of the terminal, because a five-column table
	// needs it. The theme picker is narrow only because its content is.
	inner := max(minFindTitle+colFindNum+1, m.width-2*borderWidth-2*panePadX)

	var b strings.Builder
	b.WriteString(m.styles.Title.Render("find"))
	if m.findQuery != "" {
		b.WriteString(m.styles.Muted.Render("  " + truncate(m.findQuery, inner-6)))
	}
	b.WriteString("\n\n")

	rows := max(1, m.height-1-borderHeight-5)
	b.WriteString(m.findBody(inner, rows))

	b.WriteString("\n")
	b.WriteString(m.styles.Muted.Render(truncate(
		"enter add · j/k move · "+displayKey(m.keymap.Find)+" new search · esc close", inner)))

	box = m.styles.pane(true).Width(inner + 2*panePadX).Render(b.String())
	x, y = center(m.width, m.height-1, lipgloss.Width(box), lipgloss.Height(box))
	return box, x, y
}

// findBody is the middle of the box: whichever of the four things there
// is to say.
func (m Model) findBody(inner, rows int) string {
	switch {
	case m.findState == findLoading:
		where := ""
		if len(m.findSources) > 0 {
			where = " " + strings.Join(m.findSources, ", ")
		}
		return m.styles.Muted.Render(truncate(
			fmt.Sprintf("searching%s for %q...", where, m.findQuery), inner)) + "\n"

	case m.findErr != nil:
		// The error already carries the remedy - which config key to
		// look at - so it is shown as it is rather than summarised.
		return m.styles.Error.Render(wrapTo(m.findErr.Error(), inner)) + "\n"

	case len(m.findResults) == 0:
		return m.styles.Muted.Render(truncate(
			fmt.Sprintf("nothing matched %q", m.findQuery), inner)) + "\n"
	}

	titleW, wide := findColumns(inner)
	var b strings.Builder
	b.WriteString(m.styles.ColHeader.Render(findHeader(titleW, wide, inner)))
	b.WriteString("\n")

	start, end := scrollWindow(m.findCursor, len(m.findResults), rows)
	for i := start; i < end; i++ {
		line := m.findRow(i, titleW, wide, inner)
		if i == m.findCursor {
			b.WriteString(m.styles.CursorRow.Render(line))
		} else {
			b.WriteString(m.styles.Row.Render(line))
		}
		b.WriteString("\n")
	}

	// A source that failed is said beneath what did come back, rather
	// than instead of it.
	for _, w := range m.findWarnings {
		b.WriteString(m.styles.Error.Render(truncate(" ! "+w, inner)))
		b.WriteString("\n")
	}

	if len(m.findResults) > rows {
		b.WriteString(m.styles.Muted.Render(truncate(
			fmt.Sprintf(" %d of %d", m.findCursor+1, len(m.findResults)), inner)))
		b.WriteString("\n")
	}
	return b.String()
}

func findHeader(titleW int, wide bool, inner int) string {
	var b strings.Builder
	b.WriteString(padRight("", colFindNum))
	b.WriteString(" ")
	b.WriteString(padRight("TITLE", titleW))
	if wide {
		b.WriteString(" " + padRight("SIZE", colFindSize))
		b.WriteString(" " + padRight("SEEDS", colFindPeers))
		b.WriteString(" " + padRight("PEERS", colFindPeers))
		b.WriteString(" " + padRight("TRACKER", colFindTracker))
	}
	return padRight(truncate(b.String(), inner), inner)
}

// findRow builds one line, styled as a whole by the caller.
//
// One style for the line rather than per cell, as renderRow does: nested
// escape sequences reset the highlight partway along and leave the
// cursor row looking broken.
func (m Model) findRow(i, titleW int, wide bool, inner int) string {
	r := m.findResults[i]

	marker := "  "
	if i == m.findCursor {
		marker = "> "
	}
	title := r.Title
	if title == "" {
		title = "(untitled)"
	}
	if r.Link == "" {
		// Nothing to add, said on the row rather than only on enter.
		title += " (no link)"
	}

	var b strings.Builder
	b.WriteString(padRight(marker+fmt.Sprint(i+1), colFindNum))
	b.WriteString(" ")
	b.WriteString(padRight(truncate(title, titleW), titleW))
	if wide {
		size := "-"
		if r.Size > 0 {
			size = format.Bytes(r.Size)
		}
		b.WriteString(" " + padRight(size, colFindSize))
		b.WriteString(" " + padRight(format.Count(r.Seeders), colFindPeers))
		b.WriteString(" " + padRight(format.Count(r.Leechers), colFindPeers))
		b.WriteString(" " + padRight(truncate(r.Tracker, colFindTracker), colFindTracker))
	}
	return padRight(truncate(b.String(), inner), inner)
}

// wrapTo breaks text at width, on spaces where it can. An indexer error
// is a sentence with advice in it, and truncating would cut off the half
// that says what to do.
func wrapTo(s string, width int) string {
	if width < 1 {
		return s
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case lipgloss.Width(line)+1+lipgloss.Width(word) <= width:
				line += " " + word
			default:
				lines = append(lines, padRight(line, width))
				line = word
			}
		}
		lines = append(lines, padRight(truncate(line, width), width))
	}
	return strings.Join(lines, "\n")
}
