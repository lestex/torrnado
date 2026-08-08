package launch

import (
	"reflect"
	"testing"
)

// A configured command may carry flags, so it is split on spaces. It is
// deliberately not run through a shell: passing it to `sh -c` would make
// the path a shell-injection surface.
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

// Where the path lands is the whole of what a configured command can say,
// so each shape it can take is worth pinning down - especially the one
// with a space in it, which is what a real download directory looks like
// and what splitting after substitution would have quietly broken in two.
func TestWithPathPutsThePathWhereTheCommandAsked(t *testing.T) {
	cases := []struct {
		name string
		args []string
		path string
		want []string
	}{
		{
			"no placeholder means appended, as every player did before there was one",
			[]string{"--no-terminal"}, "http://127.0.0.1:9/x",
			[]string{"--no-terminal", "http://127.0.0.1:9/x"},
		},
		{
			"no arguments at all",
			[]string{}, "/tmp/x",
			[]string{"/tmp/x"},
		},
		{
			"substituted in the middle, not appended",
			[]string{"--working-directory", "%f", "-e", "zsh"}, "/tmp/x",
			[]string{"--working-directory", "/tmp/x", "-e", "zsh"},
		},
		{
			"inside a larger field",
			[]string{"--working-directory=%f"}, "/tmp/x",
			[]string{"--working-directory=/tmp/x"},
		},
		{
			"a path with spaces stays one argument",
			[]string{"--working-directory", "%f"}, "/Users/me/Downloads/Some Show",
			[]string{"--working-directory", "/Users/me/Downloads/Some Show"},
		},
		{
			"used more than once",
			[]string{"--title", "%f", "--path", "%f"}, "/tmp/x",
			[]string{"--title", "/tmp/x", "--path", "/tmp/x"},
		},
	}
	for _, c := range cases {
		if got := withPath(c.args, c.path); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n  withPath(%v, %q) = %v\n  want %v", c.name, c.args, c.path, got, c.want)
		}
	}
}

func TestDetachedWithNothingConfigured(t *testing.T) {
	if err := Detached("", "http://127.0.0.1:1/x"); err == nil {
		t.Error("launching with no command configured should fail")
	}
}

func TestDetachedReportsAMissingBinary(t *testing.T) {
	if err := Detached("definitely-not-a-real-program", "http://127.0.0.1:1/x"); err == nil {
		t.Error("launching a binary that does not exist should fail")
	}
}
