package config

import (
	"fmt"
	"strings"
	"time"
)

// Duration is a time span written the way a person would write it:
// "48h", "30m", "7d", or "none".
//
// time.Duration's own parser is used for everything it understands, with
// "d" added on top: a seeding limit is naturally spoken in days, and
// making someone write "168h" for a week is the kind of small friction
// that stops a setting being used.
type Duration time.Duration

func (d Duration) String() string {
	if d <= 0 {
		return "none"
	}
	return time.Duration(d).String()
}

func (d *Duration) UnmarshalText(text []byte) error {
	v, err := ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// ParseDuration parses a span, accepting a "d" suffix for whole days on
// top of what time.ParseDuration takes.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "none") || strings.EqualFold(s, "unlimited") {
		return 0, nil
	}
	// Only a bare "<n>d"; mixing it into a compound like "1d6h" would
	// mean reimplementing the whole parser to gain very little.
	if rest, ok := strings.CutSuffix(strings.ToLower(s), "d"); ok {
		var days float64
		if _, err := fmt.Sscanf(rest, "%g", &days); err == nil {
			return time.Duration(days * 24 * float64(time.Hour)), nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (want e.g. 48h, 30m, 7d, or none)", s)
	}
	return d, nil
}
