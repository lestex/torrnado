package config

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

		{" 2M ", 2 << 20}, // surrounding space is not an error
		{"4096B", 4096},   // a trailing B means plain bytes
	}
	for _, c := range cases {
		got, err := ParseRate(c.in)
		if err != nil {
			t.Errorf("ParseRate(%q) failed: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseRate(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseRateRejectsNonsense(t *testing.T) {
	for _, in := range []string{"fast", "-1", "-5M", "1..5M", "M"} {
		if got, err := ParseRate(in); err == nil {
			t.Errorf("ParseRate(%q) = %d, want an error", in, got)
		}
	}
}
