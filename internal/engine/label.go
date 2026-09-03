package engine

import (
	"fmt"
	"strings"
	"unicode"
)

// maxLabelLen bounds a label so one cannot be pasted in at a length that
// makes every list and sidebar unreadable. Generous: the sidebar is 20
// columns and truncates anyway, so this is a guard against absurdity
// rather than a width the interface depends on.
const maxLabelLen = 64

// SetLabel files a torrent under a label, or clears it when label is
// empty.
//
// There is deliberately no "create a label" call and no registry of
// them. A label exists exactly while some torrent carries it: applying
// one to the first torrent brings it into being, and clearing it from
// the last takes it away again. That means nothing to garbage-collect,
// no way to accumulate labels nobody uses, and no cap to argue about -
// the sidebar can only ever list labels that something is filed under.
func (e *Engine) SetLabel(id TorrentID, label string) error {
	label, err := normalizeLabel(label)
	if err != nil {
		return err
	}

	tr, err := e.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	was := tr.label
	tr.label = label
	name := tr.t.Name()
	e.mu.Unlock()

	// Logged like every other mutating operation, and with the value it
	// replaced: "the label is gone" is a question the log could not
	// answer before, which left removal timestamps as the only way to
	// work out what had happened to one.
	if was != label {
		e.log.Info("torrent labelled", "id", id, "name", name, "label", label, "was", was)
	}

	e.persist()
	e.snapshotAndBroadcastNow()
	return nil
}

// normalizeLabel trims a label and rejects one that cannot be displayed.
//
// Control characters are the reason this exists rather than trimming
// alone: a tab or a newline in a label corrupts every line of the list
// it appears on, and the failure looks like a rendering bug rather than
// like the input it came from.
func normalizeLabel(label string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", nil // clearing
	}
	if len(label) > maxLabelLen {
		return "", fmt.Errorf("label is %d bytes, over the %d-byte limit", len(label), maxLabelLen)
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("label must not contain control characters")
		}
	}
	return label, nil
}
