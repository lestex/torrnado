package main

import (
	"bytes"
	"strings"
	"testing"
)

// A release build stamps all three values in; the report has to carry
// them, since a bug report quoting this is how a version ever gets
// checked.
func TestWriteVersionReportsWhatWasStamped(t *testing.T) {
	var buf bytes.Buffer
	writeVersion(&buf, buildInfo{
		Version: "v0.1.0",
		Commit:  "0f0fcc253799bcca544cf3fece8e27df1d34fae8",
		Date:    "2026-08-07T00:00:00Z",
		Go:      "go1.25.12",
	})

	for _, want := range []string{"v0.1.0", "0f0fcc253799bcca", "2026-08-07", "go1.25.12"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("the report is missing %q:\n%s", want, buf.String())
		}
	}
}

// A plain `go build` stamps nothing. The command still has to say
// something useful rather than printing empty fields.
func TestWriteVersionWithNothingStamped(t *testing.T) {
	var buf bytes.Buffer
	writeVersion(&buf, buildInfo{Version: "dev", Go: "go1.25.12"})

	out := buf.String()
	if !strings.Contains(out, "torrnado dev") {
		t.Errorf("an unstamped build reports %q", out)
	}
	// Blank lines for values nobody set read as a broken build.
	if strings.Contains(out, "commit  \n") || strings.Contains(out, "built   \n") {
		t.Errorf("empty fields were printed:\n%s", out)
	}
}

// currentBuild reads the toolchain's own record when the ldflags are
// absent, which is what makes a `go install ...@latest` binary
// identifiable at all.
func TestCurrentBuildAlwaysHasAVersionAndAGoVersion(t *testing.T) {
	b := currentBuild()

	if b.Version == "" {
		t.Error("currentBuild reported no version")
	}
	if b.Go == "" {
		t.Error("currentBuild reported no Go version")
	}
	// The one-line form goes in a log field and in --version, so it must
	// never come out empty either.
	if strings.TrimSpace(b.String()) == "" {
		t.Error("the one-line form is empty")
	}
}

// The module graph's own version is only worth showing when it names a
// real tag. A pseudo-version restates the commit and the timestamp that
// are already on the next two lines.
func TestModuleVersionPrefersATagOverAPseudoVersion(t *testing.T) {
	cases := map[string]string{
		"v0.1.0":                             "v0.1.0",
		"v1.2.3+dirty":                       "v1.2.3",
		"(devel)":                            "dev",
		"":                                   "dev",
		"v0.0.0-20260807014500-0f0fcc253799": "dev",
	}
	for in, want := range cases {
		if got := moduleVersion(in); got != want {
			t.Errorf("moduleVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShortCommitKeepsTheDirtyMarker(t *testing.T) {
	cases := map[string]string{
		"0f0fcc253799bcca544cf3fece8e27df1d34fae8":       "0f0fcc253799",
		"0f0fcc253799bcca544cf3fece8e27df1d34fae8-dirty": "0f0fcc253799-dirty",
		"short": "short",
		"":      "",
	}
	for in, want := range cases {
		if got := shortCommit(in); got != want {
			t.Errorf("shortCommit(%q) = %q, want %q", in, got, want)
		}
	}
}
