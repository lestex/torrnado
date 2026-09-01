# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`torrnado` (module `github.com/lestex/torrnado`) is a terminal BitTorrent
client: a vim-like TUI (bubbletea/lipgloss) on top of a torrent engine
(anacrolix/torrent) that runs as a background daemon. The TUI and CLI are
both thin clients that talk to that daemon over a local Unix socket.

## Commands

Go is managed via goenv and is **not on `PATH` in a plain non-interactive
shell** (goenv init lives in `~/.zshrc`, which non-interactive shells don't
source). Every command below needs this prefix first, or it fails with
`command not found: go`:

```sh
export PATH="$HOME/.goenv/shims:$HOME/.goenv/bin:$PATH"
```

```sh
# Build
go build -o torrnado ./cmd/torrnado

# Build + type-check everything without producing a binary
go build ./...

# Vet and format-check (both must be clean before considering work done)
go vet ./...
gofmt -l .          # non-empty output = files need `gofmt -w`

# Tidy dependencies after adding/removing an import
go mod tidy

# Run
./torrnado                 # attach TUI, spawning a daemon if none is running
./torrnado daemon           # run the engine in the foreground
./torrnado add <magnet|file|url|dir|glob|magnet-list-file>...
./torrnado list
./torrnado --help           # full subcommand reference
```

There are no test files in this repo yet. If adding tests, standard `go
test ./...` conventions apply.

Requires Go 1.25+ (see `go.mod`).

## Architecture

The whole design exists to make one thing possible: **the daemon keeps
running after the TUI exits.** Everything is layered so that no UI code
ever touches the torrent library directly, and no daemon code ever
touches the UI:

```
internal/format   byte/rate/ratio/ETA formatting. Zero deps, used by both
                  the TUI and CLI tabular output so they can't drift.
internal/engine   Wraps anacrolix/torrent.Client behind a small Go API +
                  an event channel of periodic state snapshots. No IPC or
                  UI code here - this package alone is what you'd embed
                  if you wanted a different frontend entirely.
internal/logging  The daemon's slog text logger. Writes through a
                  swappable writer so SIGHUP can reopen the log file
                  without rebuilding the logger every package holds.
internal/ipc      Hand-rolled gob-over-Unix-socket protocol: call/reply
                  for commands, server-pushed events for state, both
                  multiplexed on one long-lived connection (see
                  internal/ipc/protocol.go's doc comment for why this
                  isn't net/rpc or REST). This is the only thing that
                  makes daemon/TUI separation possible.
internal/config   TOML config, XDG path resolution (respects
                  $XDG_CONFIG_HOME/$XDG_DATA_HOME on every platform, not
                  just Linux - see paths.go), validation that names the
                  bad key rather than failing silently.
internal/theme    Built-in palettes + TOML theme overrides. Colors are
                  lipgloss.Color hex strings; truecolor->256->16
                  degradation is handled by lipgloss/termenv automatically,
                  not by this package.
internal/batch    Expands `add` arguments (dirs, globs, magnet-list files,
                  http(s) .torrent URLs) into a flat, self-describing
                  source list for ipc.Client.AddBatch.
internal/stream   Loopback HTTP server handing torrent files to a media
                  player while they download, backed by torrent.Reader
                  (blocking reads; the read position is what drives piece
                  priority). Separate from ipc because the gob protocol
                  cannot carry a byte stream. Depends only on engine.
internal/launch   Starts an external program on a path or URL and leaves
                  it running - a player against a stream, a file manager
                  against a folder. Detached, so it outlives the TUI; not
                  tea.ExecProcess, which suspends the TUI. A configured
                  command may place the argument itself with %f, which is
                  substituted after the split so a path with a space in it
                  stays one argument.
internal/vpn      Reports whether traffic leaves through a tunnel, for the
                  `vpn.required` guard. Stdlib only: a UDP dial (which
                  sends nothing) gives the interface the route table would
                  use, then a build-tagged classifier says what kind of
                  device that is - sysfs on Linux, point-to-point with no
                  MAC on darwin. Classifies by device, never by name, and
                  fails closed. The engine does not import it; the daemon
                  passes it in as a closure.
internal/tui      bubbletea Model. Three panes (status sidebar, torrent
                  list, docked Pieces/Peers/Files detail pane) + command
                  palette; keys route by which pane has focus, not by a
                  "current view". layout.go owns all geometry - render
                  functions take widths from the computed `panes`, never
                  from constants, and every pane's content goes through
                  clampBlock (lipgloss Height() is a minimum, not a
                  maximum, so overflowing content pushes the frame off
                  screen). Talks to the engine only through an ipc.Client,
                  so it never knows whether the daemon is a spawned
                  subprocess or one the user started themselves.
cmd/torrnado      cobra CLI: daemon lifecycle (spawn/attach/foreground),
                  and one subcommand per engine operation as a scriptable
                  passthrough to the same IPC the TUI uses.
```

Dependency order is strict and one-directional: `format`, `engine` and
`vpn` have no internal deps; `ipc` depends only on `engine`; `config`
depends on `format`; `tui` depends on `engine`, `ipc`, `config`, `theme`,
`batch`; `cmd` depends on everything. `engine` taking its VPN check as a
`func() VPNStatus` rather than importing `vpn` is what keeps that first
clause true. Nothing in `internal/engine` or
`internal/ipc` imports `internal/tui` or `cmd` - if you find yourself
wanting to, the abstraction has leaked.

### Why a daemon + IPC instead of one process

`cmd/torrnado`'s bare command dials `$XDG_DATA_HOME/torrnado/daemon.sock`;
if nothing answers, it spawns `torrnado daemon` detached
(`SysProcAttr.Setsid`, POSIX-only) and retries the dial. Every CLI
subcommand (`add`, `pause`, `list`, ...) does the same dial-or-spawn and
then makes one RPC call. Quitting the TUI never stops the daemon.

### anacrolix/torrent gotchas baked into internal/engine

These aren't obvious from the anacrolix/torrent docs and caused real bugs
during development - if you're touching `internal/engine`, read the doc
comments on `downloadAllFiles`, `filesOrNil`, and `enforceRateLimitLocked`
before assuming an API does what it sounds like it does. Short version:

- `Torrent.Files()` / `NumPieces()` panic (nil deref, crashes the whole
  daemon) if called before that torrent's metadata has arrived. Every
  call site must check `t.Info() != nil` first (`filesOrNil` in ops.go).
- `File.Priority()` reflects only what was last passed to
  `File.SetPriority()` - it does **not** reflect piece priorities set by
  `Torrent.DownloadAll()`/`DownloadPieces()`. The engine deliberately
  avoids `DownloadAll()` and calls `File.SetPriority()` per file instead
  (`downloadAllFiles`), or the files list would report "none" forever
  while actually downloading fine.
- Rate limiting (`golang.org/x/time/rate`) is client-wide only; there's no
  per-`Torrent` hook. Global limits are exact. Per-torrent limits are a
  best-effort approximation via `Allow/DisallowDataDownload/Upload`
  toggling each ~1s tick, not a real token bucket.
- `PieceState.Complete` (hash-verified and marked in storage) lags a
  torrent's byte progress (`bytesLeft()` counts received-but-unverified
  "dirty" chunks) by minutes on a large torrent. Don't label the piece
  count "complete" - it will sit far below a 100% progress bar and read
  as data loss. It's "verified"; it does converge. `PieceState.Complete`
  is also gated on `storage.Completion.Ok`, so `engine.PieceRun` carries
  `Known` separately: an unconsulted piece is unknown, not missing.
- Peer choke state is unexported (`Peer.choking`/`peerChoking`, no
  accessor) and `PeerStats.DownloadRate` is a lifetime average, not an
  instantaneous rate. `Engine.peerInfo` computes live per-peer speeds
  from byte-counter deltas held on `tracked.lastPeers`.
- `VerifyDataContext` (force-recheck) is O(N²) on a single-file torrent:
  each piece's `MarkComplete` scans every piece of the file through the
  SQLite completion DB. A 24k-piece torrent takes hours at ~170% CPU, and
  it can also hit an internal assertion panic that kills the daemon from
  a library goroutine (unrecoverable from here). Don't reach for recheck
  as a diagnostic; see the README for detail.
- File storage defaults `UsePartFiles` to true, so an incomplete file is at
  `<path>.part` on disk, sparse and written out of order; it is renamed only
  once every one of its pieces lands. Never hand that path to anything that
  reads it - use `Engine.OpenFile`. `RemoveTorrent` and `MoveStorage` both
  handle the pair (`dataPaths`, and the move loop's two suffixes): missing
  the `.part` leaves a half-downloaded torrent's data entirely behind.
- Reads on a paused torrent do not block waiting for a resume, they fail
  immediately - `DisallowDataDownload` means "never coming". Hence
  `Engine.PrepareStream`.
- Never infer pausedness from `TorrentSnapshot.State` - `State` reports
  Checking/Error in preference to Paused. Use `Snapshot.Paused`.
- No native pause/resume, no live "move storage", no port-range binding,
  no Local Service Discovery (LSD) support at all in the library -
  each is worked around in `internal/engine` and documented on the
  function that works around it.

### Session persistence

`internal/engine/session.go` writes `<state_dir>/session.json` (one record
per torrent: paused, save path, rate limits, per-file priorities,
added-at, and the magnet URI for torrents with no metadata yet) plus
`<state_dir>/torrents/<infohash>.torrent`. It lives in `engine` because it
reads `tracked` and the library's file priorities, neither of which
another package can see. Saved after every mutating op (temp file +
rename); restored by `Engine.RestoreSession`, which the daemon calls
before it starts listening. A record that cannot be restored is logged and
skipped - refusing to start over one bad record is worse than starting
with fewer torrents. Only the socket is guarded against two daemons
sharing it, so a second daemon needs its own `state_dir` as well as its
own socket.

The torrent library logs through both `ClientConfig.Slogger` and its own
package global (`anacrolixlog.Default`); `internal/engine/logbridge.go`
points both at our handler, because capturing one leaves the other
writing a different format straight to stderr.

### IPC wire format

`internal/ipc/protocol.go` defines one gob-encoded envelope type
(`message`) carrying either a `Request`/`Response` call-reply pair or a
pushed `Event`, multiplexed on a single `gob.Encoder`/`Decoder` pair per
connection. `Request`/`Response` are deliberately flat "kitchen sink"
structs (all fields for all methods) rather than one type per method
behind an `interface{}`, because gob can't encode interface values
without registering every concrete type up front.

### Config/theme resolution

`internal/config.Load` returns `Default()` (not an error) when the config
file doesn't exist; an unknown key or bad value fails loudly, naming the
key. `internal/theme.Load` checks
`$XDG_CONFIG_HOME/torrnado/themes/<name>.toml` before falling back to a
built-in of that name.
