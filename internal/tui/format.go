package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/format"
)

func formatBytes(n int64) string { return format.Bytes(n) }

func formatRate(bytesPerSec float64) string { return format.Rate(bytesPerSec) }

func formatRatio(r float64) string { return format.Ratio(r) }

func formatETA(d time.Duration) string { return format.ETA(d) }

// truncate shortens s to fit width terminal cells, appending an ellipsis
// when it has to cut.
//
// Width here is display width, not rune count: torrent names routinely
// contain CJK and emoji, which occupy two cells each, so counting runes
// would let a name overflow its column and shear every column to its
// right. lipgloss.Width accounts for that (and for any embedded ANSI).
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	// Trim runes off the end until the remainder plus the ellipsis fits.
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		if lipgloss.Width(string(runes))+1 <= width {
			return string(runes) + "…"
		}
	}
	return "…"
}

// padRight pads or truncates s to exactly width display cells.
func padRight(s string, width int) string {
	s = truncate(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// padLeft right-aligns s within exactly width display cells.
func padLeft(s string, width int) string {
	s = truncate(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}
