package player

import (
	"reflect"
	"testing"
)

// A configured player may carry flags, so it is split on spaces. It is
// deliberately not run through a shell: passing it to `sh -c` would make
// the stream URL a shell-injection surface.
func TestParseSplitsCommandAndFlags(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantArgs []string
	}{
		{"mpv", "mpv", []string{}},
		{"mpv --no-terminal", "mpv", []string{"--no-terminal"}},
		{"  vlc  --intf dummy  ", "vlc", []string{"--intf", "dummy"}},
		{"/usr/bin/mpv", "/usr/bin/mpv", []string{}},
		{"", "", nil},
		{"   ", "", nil},
	}
	for _, c := range cases {
		name, args := parse(c.in)
		if name != c.wantName {
			t.Errorf("parse(%q) name = %q, want %q", c.in, name, c.wantName)
		}
		if len(args) != len(c.wantArgs) || (len(args) > 0 && !reflect.DeepEqual(args, c.wantArgs)) {
			t.Errorf("parse(%q) args = %v, want %v", c.in, args, c.wantArgs)
		}
	}
}

func TestLaunchWithNoPlayerConfigured(t *testing.T) {
	if err := Launch("", "http://127.0.0.1:1/x"); err == nil {
		t.Error("launching with no player configured should fail")
	}
}

func TestLaunchReportsAMissingBinary(t *testing.T) {
	if err := Launch("definitely-not-a-real-player", "http://127.0.0.1:1/x"); err == nil {
		t.Error("launching a binary that does not exist should fail")
	}
}
