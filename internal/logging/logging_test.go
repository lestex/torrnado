package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo}, // unset means the default, not an error
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"  INFO  ", slog.LevelInfo}, // case and spacing are forgiven
	}
	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}

	if _, err := ParseLevel("verbose"); err == nil {
		t.Error("an unknown level should be an error")
	}
}

func TestLogsToFileWithTimestampAndLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.log")

	lg, err := New("info", path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer lg.Close()

	lg.Info("daemon started", "pid", 1234)

	body := readFile(t, path)
	for _, want := range []string{"level=INFO", "msg=", "daemon started", "pid=1234", "time="} {
		if !strings.Contains(body, want) {
			t.Errorf("log line %q is missing %q", body, want)
		}
	}
}

// Messages below the level must not reach the file at all -- a level
// that only affects formatting is not a level.
func TestLevelFiltersMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.log")

	lg, err := New("warn", path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer lg.Close()

	lg.Debug("debug line")
	lg.Info("info line")
	lg.Warn("warn line")

	body := readFile(t, path)
	if strings.Contains(body, "debug line") || strings.Contains(body, "info line") {
		t.Errorf("messages below the level were written: %q", body)
	}
	if !strings.Contains(body, "warn line") {
		t.Errorf("the warning was not written: %q", body)
	}
}

// Reopen is what makes logrotate work: after the file is renamed away,
// writes must land in a fresh file at the original path rather than in
// the unlinked inode nobody can read.
func TestReopenWritesToANewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.log")

	lg, err := New("info", path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer lg.Close()

	lg.Info("before rotation")

	// What logrotate does.
	if err := os.Rename(path, filepath.Join(dir, "d.log.1")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := lg.Reopen(); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	lg.Info("after rotation")

	fresh := readFile(t, path)
	if !strings.Contains(fresh, "after rotation") {
		t.Errorf("the new file is missing the later message: %q", fresh)
	}
	if strings.Contains(fresh, "before rotation") {
		t.Errorf("the new file should start empty: %q", fresh)
	}

	rotated := readFile(t, filepath.Join(dir, "d.log.1"))
	if !strings.Contains(rotated, "before rotation") {
		t.Errorf("the rotated file lost the earlier message: %q", rotated)
	}
}

// Reopening a stderr logger is meaningless, and must not be an error --
// the daemon sends SIGHUP through the same path either way.
func TestReopenOnStderrIsANoOp(t *testing.T) {
	lg, err := New("info", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := lg.Reopen(); err != nil {
		t.Errorf("Reopen on a stderr logger: %v", err)
	}
}

func TestNewRejectsAnUnwritableFile(t *testing.T) {
	if _, err := New("info", filepath.Join(t.TempDir(), "no-such-dir", "d.log")); err == nil {
		t.Error("a log file that cannot be opened should fail at startup")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(b)
}
