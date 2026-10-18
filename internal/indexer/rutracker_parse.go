package indexer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// The result table is <table id="tor-tbl"> and every row is a
// <tr class="... hl-tr"> carrying data-topic_id.
//
// Three of its cells carry a data-ts_text attribute, which the page puts
// there for its own column sorting and which holds the exact machine
// value behind the human one: the size in bytes beside "3.16 GB", the
// seeder count, and the post date as a unix timestamp beside
// "30-Сен-26". Reading those instead of the text is what keeps this
// parser out of the business of understanding abbreviated Russian month
// names, and makes the size exact rather than rounded to three digits.
const (
	rowClass     = "hl-tr"
	topicAttr    = "data-topic_id"
	sortAttr     = "data-ts_text"
	sizeClass    = "tor-size"
	leechClass   = "leechmed"
	titleClass   = "t-title"
	forumClass   = "f-name"
	seedTextSpan = "seedmed"
)

// parseRutracker reads the result table out of a search page.
func parseRutracker(doc *html.Node) ([]Result, error) {
	table := elementByID(doc, "tor-tbl")
	if table == nil {
		// Not an empty result set: that is this table with no rows. A
		// missing table means the page was not the one we think.
		return nil, fmt.Errorf("no result table on the page")
	}
	var out []Result
	for _, row := range elementsByClass(table, "tr", rowClass) {
		if r, ok := rowResult(row); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// rowResult turns one row into a Result, or reports that it is not one.
func rowResult(row *html.Node) (Result, bool) {
	id := attr(row, topicAttr)
	if id == "" {
		return Result{}, false
	}
	cells := children(row, "td")
	if len(cells) < 8 {
		return Result{}, false
	}

	r := Result{
		Tracker: RutrackerName,
		// No magnet and no infohash anywhere on this page, so the row
		// cannot carry a link yet - see token.go. The id is what the
		// topic page needs.
		Link:     Token(RutrackerName, id),
		Seeders:  Unknown,
		Leechers: Unknown,
	}

	for i, td := range cells {
		switch {
		case hasClass(td, titleClass) || findByClass(td, titleClass) != nil:
			if r.Title == "" {
				r.Title = strings.TrimSpace(text(td))
			}
		case hasClass(td, sizeClass):
			r.Size = numAttr(td, sortAttr)
		case hasClass(td, leechClass):
			// The leechers cell is the one number the page does not give
			// a sort value for, so this is the only count read from the
			// text rather than from an attribute.
			r.Leechers = countFromText(text(td))
			// The seeders cell sits immediately before the leechers one
			// and is the only cell with no class of its own, so it is
			// identified by that position rather than by a selector.
			if i > 0 {
				prev := cells[i-1]
				r.Seeders = clampCount(int(numAttr(prev, sortAttr)), text(prev))
			}
		}
	}
	if r.Title == "" {
		return Result{}, false
	}
	return r, true
}

// clampCount turns a cell's sort value into a count.
//
// A torrent with no seeders has no number in its cell at all: the page
// shows how long it has been that way ("1 дн.") and sets the sort value
// to -1. That is not our Unknown, which means the source did not say.
// rutracker plainly does know - knowing is what lets it say how long -
// so it has to read as zero, or a dead torrent renders as "-" and looks
// merely unreported, which is the one distinction a user scans this list
// to make.
func clampCount(n int, cellText string) int {
	if n < 0 {
		if strings.TrimSpace(cellText) == "" {
			return Unknown
		}
		return 0
	}
	return n
}

// countFromText reads a count the page prints but does not annotate.
// Anything that is not a plain number is Unknown rather than zero: the
// difference between "nobody is downloading this" and "the page did not
// say" is the same one clampCount exists to keep.
func countFromText(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return Unknown
	}
	return n
}

// findMagnet returns the one magnet link on a topic page.
func findMagnet(doc *html.Node) string {
	var found string
	walk(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "a" {
			if href := attr(n, "href"); strings.HasPrefix(href, "magnet:") {
				found = href
				return false
			}
		}
		return true
	})
	return found
}

func sortResults(results []Result) {
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Seeders > results[j].Seeders
	})
}

// --- the small amount of DOM walking this needs ---------------------
//
// x/net/html gives a tree and no queries, and a selector library would
// be a dependency for four lookups.

func walk(n *html.Node, visit func(*html.Node) bool) {
	if !visit(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func numAttr(n *html.Node, name string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(attr(n, name)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func elementByID(root *html.Node, id string) *html.Node {
	var found *html.Node
	walk(root, func(n *html.Node) bool {
		if n.Type == html.ElementNode && attr(n, "id") == id {
			found = n
			return false
		}
		return true
	})
	return found
}

func elementsByClass(root *html.Node, tag, class string) []*html.Node {
	var out []*html.Node
	walk(root, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == tag && hasClass(n, class) {
			out = append(out, n)
		}
		return true
	})
	return out
}

func findByClass(root *html.Node, class string) *html.Node {
	var found *html.Node
	walk(root, func(n *html.Node) bool {
		if n.Type == html.ElementNode && hasClass(n, class) {
			found = n
			return false
		}
		return true
	})
	return found
}

// children returns the direct child elements with the given tag, which
// is what a row's cells are: a nested table would otherwise contribute
// cells of its own.
func children(n *html.Node, tag string) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			out = append(out, c)
		}
	}
	return out
}

func text(n *html.Node) string {
	var b strings.Builder
	walk(n, func(x *html.Node) bool {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		return true
	})
	return strings.Join(strings.Fields(b.String()), " ")
}
