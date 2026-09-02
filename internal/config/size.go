package config

import (
	"strconv"

	"github.com/lestex/torrnado/internal/format"
)

// Size is a byte quantity written the way a person would write it:
// "500M", "2G", or "none" for no limit.
//
// It shares Rate's parser deliberately. A size and a rate are the same
// number in different units, and letting "2G" mean one thing in
// rate_limit and another in min_free_space would be a trap nobody would
// think to check for. Only the rendering differs: this is bytes, not
// bytes per second.
type Size int64

func (s Size) String() string {
	if s <= 0 {
		return "none"
	}
	return format.Bytes(int64(s))
}

// UnmarshalText is called by the TOML decoder for any Size field.
func (s *Size) UnmarshalText(text []byte) error {
	v, err := ParseRate(string(text))
	if err != nil {
		return err
	}
	*s = Size(v)
	return nil
}

// MarshalText writes the round-trippable form - a bare byte count - not
// String's human one, which does not parse back.
func (s Size) MarshalText() ([]byte, error) {
	if s <= 0 {
		return []byte("none"), nil
	}
	return []byte(strconv.FormatInt(int64(s), 10)), nil
}
