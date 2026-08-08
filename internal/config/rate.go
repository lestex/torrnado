package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lestex/torrnado/internal/format"
)

// Rate is a bytes/sec quantity written the way a person would write it:
// "500k", "2M", "1.5G", or "unlimited".
//
// It implements encoding.TextUnmarshaler, which is what lets the TOML
// decoder turn a config value into one of these without any special
// handling at the call site. The same parser backs the command line, so a
// rate written in a file and one typed at a prompt cannot mean different
// things.
type Rate int64

func (r Rate) String() string {
	if r <= 0 {
		return "unlimited"
	}
	return format.Rate(float64(r))
}

// UnmarshalText is called by the TOML decoder for any Rate field.
func (r *Rate) UnmarshalText(text []byte) error {
	v, err := ParseRate(string(text))
	if err != nil {
		return err
	}
	*r = Rate(v)
	return nil
}

func (r Rate) MarshalText() ([]byte, error) {
	if r <= 0 {
		return []byte("unlimited"), nil
	}
	return []byte(strconv.FormatInt(int64(r), 10)), nil
}

// ParseRate parses a human-friendly rate into bytes/sec.
//
// Accepts a bare byte count, a suffixed size ("500k", "2M", "1.5G" -
// binary multiples, case-insensitive), or "unlimited"/"none"/"" for no
// limit. Zero is the internal representation of "no limit" throughout.
func ParseRate(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "unlimited") || strings.EqualFold(s, "none") {
		return 0, nil
	}

	mult := int64(1)
	numPart := s
	switch s[len(s)-1] {
	case 'k', 'K':
		mult = 1 << 10
		numPart = s[:len(s)-1]
	case 'm', 'M':
		mult = 1 << 20
		numPart = s[:len(s)-1]
	case 'g', 'G':
		mult = 1 << 30
		numPart = s[:len(s)-1]
	case 'b', 'B':
		// A bare "B" is just bytes, and the multiplier stays 1.
		numPart = s[:len(s)-1]
	}
	numPart = strings.TrimSpace(numPart)

	// Parsed as a float so "1.5M" works; the result is whole bytes.
	f, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid rate %q: expected a number optionally suffixed with k/M/G, or \"unlimited\"", s)
	}
	if f < 0 {
		return 0, fmt.Errorf("invalid rate %q: must not be negative", s)
	}
	return int64(f * float64(mult)), nil
}
