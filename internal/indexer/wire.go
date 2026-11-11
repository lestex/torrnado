package indexer

import "encoding/json"

// Helpers for reading the backends' wire structs, whose fields are all
// pointers so an explicit JSON null stays distinguishable from a value.

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// firstNonEmpty returns the first value that is both present and not the
// empty string.
//
// Empty counts as absent, not just nil: some trackers send "" for a
// magnet they do not have, and a Result whose Link is "" would otherwise
// look like a link we could use.
func firstNonEmpty(ps ...*string) string {
	for _, p := range ps {
		if p != nil && *p != "" {
			return *p
		}
	}
	return ""
}

// numInt64 reads a size. Missing is 0.
func numInt64(p *json.Number) int64 {
	if p == nil {
		return 0
	}
	if i, err := p.Int64(); err == nil {
		return i
	}
	// Some builds report a size with an exponent or a decimal point.
	// Truncating is right for bytes and better than discarding the row.
	if f, err := p.Float64(); err == nil {
		return int64(f)
	}
	return 0
}

// numCount reads a seeder or leecher count. Missing is Unknown, which is
// the distinction the whole pointer dance exists to preserve.
func numCount(p *json.Number) int {
	if p == nil {
		return Unknown
	}
	if i, err := p.Int64(); err == nil {
		return int(i)
	}
	if f, err := p.Float64(); err == nil {
		return int(f)
	}
	return Unknown
}
