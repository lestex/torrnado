// Package logging builds the daemon's logger.
//
// Output is text with a timestamp and a level, on stderr unless a file is
// named. Stderr is the default deliberately: under a service manager the
// journal already timestamps, stores and rotates what a process writes
// there, so a log file is something to configure only when you actually
// want one.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Logger is the daemon's logger plus the file handle behind it, so the
// destination can be reopened without rebuilding the logger.
type Logger struct {
	*slog.Logger

	// out is the indirection that makes reopening possible: the slog
	// handler holds a reference to this, not to the file, so swapping the
	// file underneath leaves every existing logger valid.
	out *swappableWriter
	// path is empty when logging to stderr, where reopening is meaningless.
	path string
}

// New builds a logger writing to path (or stderr when path is empty) at
// the given level.
func New(level, path string) (*Logger, error) {
	lvl, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}

	out := &swappableWriter{w: os.Stderr}
	if path != "" {
		f, err := openLogFile(path)
		if err != nil {
			return nil, err
		}
		out.w = f
		out.f = f
	}

	handler := slog.NewTextHandler(out, &slog.HandlerOptions{Level: lvl})
	return &Logger{Logger: slog.New(handler), out: out, path: path}, nil
}

// Handler exposes the underlying slog handler, for forwarding another
// library's records into the same output.
func (l *Logger) Handler() slog.Handler { return l.Logger.Handler() }

// Reopen closes the log file and opens it again at the same path.
//
// This is what makes logrotate work. A rotated file is renamed out from
// under the process, which keeps writing to the same inode - now
// unlinked, so the log silently goes nowhere and the disk never frees the
// space. Reopening on SIGHUP is the convention for saying "look again".
//
// A no-op when logging to stderr, where there is nothing to reopen.
func (l *Logger) Reopen() error {
	if l.path == "" {
		return nil
	}
	f, err := openLogFile(l.path)
	if err != nil {
		return err
	}
	old := l.out.swap(f)
	if old != nil {
		old.Close()
	}
	return nil
}

// Close releases the log file, if there is one.
func (l *Logger) Close() error {
	if f := l.out.swap(nil); f != nil {
		return f.Close()
	}
	return nil
}

func openLogFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}
	return f, nil
}

// ParseLevel maps a config value onto a slog level.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q (want debug, info, warn or error)", s)
}

// swappableWriter lets the destination change while the handler holding
// it stays the same. Writes are serialized because the file can be
// replaced from a signal handler while another goroutine is mid-write.
type swappableWriter struct {
	mu sync.Mutex
	w  io.Writer
	f  *os.File
}

func (s *swappableWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return len(p), nil // closed; drop rather than fail a log call
	}
	return s.w.Write(p)
}

// swap installs a new file and returns the old one, if any.
func (s *swappableWriter) swap(f *os.File) *os.File {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.f
	s.f = f
	if f != nil {
		s.w = f
	} else if old != nil {
		// Closing a file-backed logger leaves nothing to write to; fall
		// back to stderr rather than losing later messages entirely.
		s.w = os.Stderr
	}
	return old
}
