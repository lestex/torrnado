package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/engine"
)

// cellState is the aggregate state of the piece range one bitmap cell
// stands for. Ordering matters: aggregating a range keeps the highest
// value seen, so a single missing piece is enough to stop a cell from
// reading as complete.
type cellState int

const (
	cellComplete cellState = iota
	cellChecking
	cellPartial
	cellUnknown // downloaded state not yet read back from storage
	cellMissing
)

// renderPieceMap draws the piece completion bitmap for d, downsampled to
// fit width x height cells.
//
// A torrent can have tens of thousands of pieces and the pane is a few
// hundred cells, so each cell aggregates a contiguous range and reports
// the worst state in it. The runs are walked in place rather than
// expanded into a per-piece slice -- run-length is the whole reason the
// engine sends them this way.
func (m Model) renderPieceMap(d engine.TorrentDetail, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if d.NumPieces == 0 || len(d.Pieces) == 0 {
		return m.styles.Muted.Render(" waiting for metadata...")
	}

	cells := width * height
	if cells > d.NumPieces {
		cells = d.NumPieces
	}
	states := downsample(d.Pieces, d.NumPieces, cells)

	var b strings.Builder
	for i, st := range states {
		if i > 0 && i%width == 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.pieceStyle(st).Render("█"))
	}
	return b.String()
}

func (m Model) pieceStyle(st cellState) lipgloss.Style {
	switch st {
	case cellComplete:
		return m.styles.Success
	case cellChecking, cellPartial:
		return m.styles.Warning
	case cellUnknown:
		return m.styles.Muted
	default:
		return m.styles.ProgressTrack
	}
}

// downsample reduces numPieces pieces described by runs down to exactly
// cells aggregate states.
func downsample(runs []engine.PieceRun, numPieces, cells int) []cellState {
	if cells <= 0 {
		return nil
	}
	out := make([]cellState, cells)

	piece := 0
	for _, run := range runs {
		st := runState(run)
		for i := 0; i < run.Length && piece < numPieces; i, piece = i+1, piece+1 {
			// Map this piece onto its cell by proportion, so the last
			// cell can't overrun the slice however the division rounds.
			c := piece * cells / numPieces
			if c >= cells {
				c = cells - 1
			}
			if st > out[c] {
				out[c] = st
			}
		}
	}
	return out
}

func runState(r engine.PieceRun) cellState {
	switch {
	case r.Complete:
		return cellComplete
	case r.Checking:
		return cellChecking
	case r.Partial:
		return cellPartial
	case !r.Known:
		return cellUnknown
	default:
		return cellMissing
	}
}

// pieceSummary is the one-line header above the bitmap.
//
// It says "verified", not "complete", and that word is load-bearing: a
// torrent's byte progress counts data as soon as it arrives, but a piece
// only counts here once the library has hash-checked it and marked it in
// storage. Verification lags arrival, so a torrent sitting at 100% can
// show well under half its pieces verified for minutes afterwards. Saying
// "complete" would make that read as data loss.
func pieceSummary(d engine.TorrentDetail) string {
	if d.NumPieces == 0 {
		return ""
	}
	var verified, unknown int
	for _, run := range d.Pieces {
		switch {
		case run.Complete:
			verified += run.Length
		case !run.Known:
			unknown += run.Length
		}
	}
	s := fmt.Sprintf(" %d/%d pieces verified × %s", verified, d.NumPieces, formatBytes(d.PieceLength))
	if unknown > 0 {
		s += fmt.Sprintf("  (%d unknown)", unknown)
	}
	return s
}
