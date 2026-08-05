package main

import "testing"

func TestParseRate(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"", 0}, // every spelling of "no cap"
		{"0", 0},
		{"none", 0},
		{"unlimited", 0},
		{"UNLIMITED", 0},

		{"1024", 1024}, // a bare byte count
		{"500k", 500 << 10},
		{"500K", 500 << 10},
		{"2M", 2 << 20},
		{"1G", 1 << 30},
		{"1.5M", 1536 << 10}, // fractions are allowed

		{"2MB", 2 << 20}, // the longer spellings mean the same thing
		{"2MiB", 2 << 20},
		{" 2M ", 2 << 20}, // surrounding space is not an error
	}
	for _, c := range cases {
		got, err := parseRate(c.in)
		if err != nil {
			t.Errorf("parseRate(%q) failed: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseRate(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseRateRejectsNonsense(t *testing.T) {
	for _, in := range []string{"fast", "-1", "-5M", "1..5M", "M"} {
		if got, err := parseRate(in); err == nil {
			t.Errorf("parseRate(%q) = %d, want an error", in, got)
		}
	}
}
