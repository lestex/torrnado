package format

import (
	"math"
	"testing"
	"time"
)

// Go's testing convention: a file named x_test.go next to x.go, functions
// named TestSomething(t *testing.T), run with `go test ./...`.
//
// These are "table-driven" tests - the cases live in a slice and one
// loop checks them all. It is the standard Go style because adding a case
// means adding a line, not another copy of the assertion.

func TestBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{1, "1B"},
		{1023, "1023B"}, // last value below the first unit boundary
		{1024, "1.0KiB"},
		{1536, "1.5KiB"},
		{1024 * 1024, "1.0MiB"},
		{1024 * 1024 * 1024, "1.0GiB"},
		{1 << 60, "1.0EiB"}, // the largest unit in the table
		{-1536, "-1.5KiB"},  // negatives borrow the positive formatting
	}
	for _, c := range cases {
		if got := Bytes(c.in); got != c.want {
			t.Errorf("Bytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRate(t *testing.T) {
	if got, want := Rate(0), "0B/s"; got != want {
		t.Errorf("Rate(0) = %q, want %q", got, want)
	}
	if got, want := Rate(1536), "1.5KiB/s"; got != want {
		t.Errorf("Rate(1536) = %q, want %q", got, want)
	}
}

func TestRatio(t *testing.T) {
	// A torrent that has uploaded but never downloaded has an infinite
	// ratio, and %.2f would print "+Inf".
	if got, want := Ratio(math.Inf(1)), "∞"; got != want {
		t.Errorf("Ratio(+Inf) = %q, want %q", got, want)
	}
	if got, want := Ratio(1.005), "1.00"; got != want {
		t.Errorf("Ratio(1.005) = %q, want %q", got, want)
	}
	if got, want := Ratio(0), "0.00"; got != want {
		t.Errorf("Ratio(0) = %q, want %q", got, want)
	}
}

func TestETA(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "–"},                  // unknown, not "0s"
		{-time.Second, "–"},       // never negative
		{42 * time.Second, "42s"}, // seconds only
		{5*time.Minute + 3*time.Second, "5m03s"},
		{time.Hour + 5*time.Minute, "1h05m"},
		{100 * time.Hour, ">99h"}, // anything longer is not worth a number
	}
	for _, c := range cases {
		if got := ETA(c.in); got != c.want {
			t.Errorf("ETA(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
