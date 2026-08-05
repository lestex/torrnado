package engine

import (
	"context"
	"log/slog"

	anacrolixlog "github.com/anacrolix/log"
)

// The torrent library logs through two separate channels, and capturing
// only one leaves half the output in a different format on raw stderr.
//
//   - ClientConfig.Slogger, which the library documents as the way to
//     receive what the client itself logs.
//   - a package-level global, used by code that never sees the client at
//     all. storage's piece-completion is an acknowledged case: its own
//     comment reads "This kinda sux using the global logger."
//
// Both are pointed at our handler here. Panics still reach stderr
// directly through the runtime, which is why stderr must stay attached
// whatever log.file says.

// libraryHandler forwards the library's records to ours, dropping
// anything below its own threshold. A separate level is the point: the
// library warns about every misbehaving tracker, which would otherwise
// force a choice between losing our own messages and drowning in theirs.
type libraryHandler struct {
	h   slog.Handler
	min slog.Level
}

func (l libraryHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= l.min
}

func (l libraryHandler) Handle(ctx context.Context, r slog.Record) error {
	// Tagged so library output can be told apart from ours in a journal
	// that carries both.
	r.AddAttrs(slog.String("src", "torrent"))
	return l.h.Handle(ctx, r)
}

func (l libraryHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return libraryHandler{h: l.h.WithAttrs(as), min: l.min}
}

func (l libraryHandler) WithGroup(name string) slog.Handler {
	return libraryHandler{h: l.h.WithGroup(name), min: l.min}
}

// routeLibraryLogs redirects the library's package-level logger, which
// otherwise writes its own format straight to stderr regardless of what
// the client is configured with.
func routeLibraryLogs(h slog.Handler, min slog.Level) {
	anacrolixlog.Default.SetHandlers(
		anacrolixlog.SlogHandlerAsHandler{
			SlogHandler: libraryHandler{h: h, min: min},
		},
	)
}
