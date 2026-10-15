// Package format holds byte/rate/ratio/ETA formatting shared by the TUI
// and the CLI's tabular output (e.g. `torrnado list`), so the two don't
// drift.
package format

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

func Bytes(n int64) string {
	if n < 0 {
		return "-" + Bytes(-n)
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func Rate(bytesPerSec float64) string {
	return Bytes(int64(bytesPerSec)) + "/s"
}

// Count renders a peer count, where a negative number means the tracker
// did not report one.
//
// Written as "-" rather than "0", because zero seeders is a real and
// important answer - it means the torrent will never finish - and a
// tracker that simply keeps its counts to itself must not be made to say
// it. Shared with the TUI so a result reads the same in both.
func Count(n int) string {
	if n < 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

func Ratio(r float64) string {
	if math.IsInf(r, 1) {
		return "∞"
	}
	return fmt.Sprintf("%.2f", r)
}

func ETA(d time.Duration) string {
	if d <= 0 {
		return "–"
	}
	if d > 99*time.Hour {
		return ">99h"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
