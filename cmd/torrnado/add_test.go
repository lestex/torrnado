package main

import "testing"

// Classifying a source is pure string logic, so it can be tested without
// a daemon, a socket or a network anywhere in sight.
func TestIsMagnet(t *testing.T) {
	cases := []struct {
		source string
		want   bool
	}{
		{"magnet:?xt=urn:btih:abc", true},
		{"MAGNET:?xt=urn:btih:abc", true},        // schemes are case-insensitive
		{"/home/me/ubuntu.torrent", false},       // absolute path
		{"ubuntu.torrent", false},                // relative path
		{"./magnet:weird.torrent", false},        // contains "magnet:" but isn't one
		{"https://example.com/x.torrent", false}, // a URL, not a magnet
		{"", false},
	}
	for _, c := range cases {
		if got := isMagnet(c.source); got != c.want {
			t.Errorf("isMagnet(%q) = %v, want %v", c.source, got, c.want)
		}
	}
}
