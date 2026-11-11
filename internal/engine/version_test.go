package engine

import (
	"strings"
	"testing"
)

func TestTheSameBuildIsNotSkewed(t *testing.T) {
	note, skewed := VersionSkew("0.10.1 (da4134de07be)", "0.10.1 (da4134de07be)")
	if skewed || note != "" {
		t.Errorf("identical builds reported skew: %q", note)
	}
}

// Same release, different commit is a different build - which is exactly
// a development daemon left running under a rebuilt client.
func TestADifferentBuildIsSkewedAndTheNoteNamesBoth(t *testing.T) {
	for _, tc := range []struct{ client, daemon string }{
		{"0.10.1 (da4134de07be)", "0.10.0 (a649a39b4f79)"},
		{"0.10.1 (da4134de07be)", "0.10.1 (ffffffffffff)"},
		{"v0.10.1-dirty (da4134de07be-dirty)", "0.10.1 (da4134de07be)"},
	} {
		note, skewed := VersionSkew(tc.client, tc.daemon)
		if !skewed {
			t.Errorf("%q against %q was not flagged", tc.client, tc.daemon)
		}
		for _, want := range []string{tc.client, tc.daemon} {
			if !strings.Contains(note, want) {
				t.Errorf("the note %q does not say %q", note, want)
			}
		}
	}
}

// A daemon from before it reported its version leaves the field empty.
// That is not "unknown, so fine": a daemon that old is not this build.
func TestADaemonTooOldToReportItsVersionIsSkewed(t *testing.T) {
	note, skewed := VersionSkew("0.10.1 (da4134de07be)", "")
	if !skewed || !strings.Contains(note, "too old") || !strings.Contains(note, "0.10.1") {
		t.Errorf("got %q, %v", note, skewed)
	}
}

// A client that does not know its own build - a test, an embedder - has
// nothing to compare and must never complain.
func TestAClientThatCannotSayWhatItIsNeverComplains(t *testing.T) {
	for _, daemon := range []string{"", "0.10.0 (a649a39b4f79)"} {
		if note, skewed := VersionSkew("", daemon); skewed || note != "" {
			t.Errorf("an unversioned client complained about %q: %q", daemon, note)
		}
	}
}
