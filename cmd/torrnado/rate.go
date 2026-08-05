package main

import (
	"fmt"
	"strconv"
	"strings"
)

// parseRate turns a human-written speed into bytes per second.
//
// Accepts a bare byte count, a suffixed one ("500k", "2M", "1.5G"), or
// "unlimited"/"0"/"none" for no cap. Suffixes are powers of 1024, matching
// how the sizes are printed back out.
//
// This lives in the CLI for now. It moves into the config package once
// there is one, so a rate written in a config file and one typed on the
// command line cannot drift apart.
func parseRate(s string) (int64, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "0", "none", "unlimited", "inf":
		return 0, nil
	}

	mult := int64(1)
	last := s[len(s)-1]
	// Tolerate both "2M" and "2MB"/"2MiB": the unit is never ambiguous,
	// so refusing the longer spellings would only be pedantry.
	trimmed := strings.TrimSuffix(strings.TrimSuffix(s, "iB"), "B")
	if trimmed != s {
		s = trimmed
		last = s[len(s)-1]
	}
	switch last {
	case 'k', 'K':
		mult = 1 << 10
	case 'm', 'M':
		mult = 1 << 20
	case 'g', 'G':
		mult = 1 << 30
	}
	if mult > 1 {
		s = s[:len(s)-1]
	}

	// Parsed as a float so "1.5M" works; the result is whole bytes.
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("bad rate %q (want e.g. 500k, 2M, or unlimited)", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("rate must not be negative")
	}
	return int64(n * float64(mult)), nil
}
