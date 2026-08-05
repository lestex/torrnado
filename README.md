# torrnado

A terminal BitTorrent client: a vim-like TUI (bubbletea/lipgloss) on top of
a torrent engine ([anacrolix/torrent](https://github.com/anacrolix/torrent))
that runs as a background daemon. The TUI and CLI are both thin clients
that talk to that daemon over a local Unix socket, which is what makes
detached/background operation (start a download, close the terminal, it
keeps going) possible without a separate architecture.

```
cmd/torrnado        CLI entrypoint (cobra): daemon, add, remove, pause,
                       resume, recheck, priority, limit, move, list, and
                       the bare command (attach the TUI)
internal/engine        anacrolix/torrent client wrapper: Go API + an
                       event channel of state snapshots. No IPC or UI
                       code in here.
internal/ipc            gob-over-Unix-socket RPC: call/reply for commands,
                       server-pushed events for state -- what lets the
                       daemon run detached from any UI.
internal/tui            bubbletea model: a three-pane layout (status
                       sidebar, torrent list, docked Pieces/Peers/Files
                       detail pane) plus the command palette. Talks to the
                       engine only through an ipc.Client, so it doesn't
                       matter whether the daemon is a spawned subprocess
                       or one you started yourself.
internal/config          TOML config, XDG paths, validation.
internal/theme           built-in themes + TOML theme overrides.
internal/batch           expands add-command arguments (dirs, globs,
                       magnet-list files) into a flat source list.
internal/format          byte/rate/ratio/ETA formatting shared by the TUI
                       and `torrnado list`.
```

## Build

Requires Go 1.25+.

```sh
go build -o torrnado ./cmd/torrnado
```

Run the binary from wherever you like; there's no install step beyond
putting it on your `$PATH`.

## Daemon lifecycle

There is one long-running process, the daemon, and everything else is a
thin client that connects to it over a Unix domain socket
(`$XDG_DATA_HOME/torrnado/daemon.sock` by default, usually
`~/.local/share/torrnado/daemon.sock`).

- **`torrnado`** (no arguments) attaches the TUI to the daemon. If
  nothing is listening on the socket, it spawns `torrnado daemon` as a
  detached background process first (logging to
  `$XDG_DATA_HOME/torrnado/daemon.log`), waits for it to come up, then
  attaches. Quitting the TUI (`q`) does **not** stop the daemon -- your
  torrents keep running.
- **`torrnado daemon`** runs the engine in the foreground until
  interrupted (Ctrl-C) or sent SIGTERM. This is what gets spawned in the
  background automatically, but you can also run it yourself directly if
  you'd rather manage it with systemd/launchd/tmux than let torrnado
  manage it.
- **`torrnado add/remove/pause/resume/recheck/priority/limit/move/list`**
  are scriptable passthroughs to a running daemon (spawning one if needed,
  same as the bare command). Everything you can do in the TUI's command
  palette, you can also do from a shell script.

To actually stop the daemon: send it SIGTERM/SIGINT (`pkill -f "torrnado daemon"`,
or `kill` the pid from its log line) -- there's no `torrnado stop`
subcommand, since a daemon that's still seeding is a normal, intentional
state to leave it in.

## Config

TOML at `$XDG_CONFIG_HOME/torrnado/config.toml` (`~/.config/torrnado/config.toml`
if `$XDG_CONFIG_HOME` is unset -- honored on every platform this runs on,
not just Linux). Every key is optional; a missing file is not an error,
an invalid one is -- validation fails with the specific bad key rather
than silently ignoring it.

```toml
download_dir  = "~/Downloads/torrnado"                     # default download directory
daemon_socket = "~/.local/share/torrnado/daemon.sock"       # IPC socket path
state_dir     = "~/.local/share/torrnado"                    # session file + saved metainfo
theme         = "dracula"                                     # see Themes below
player        = "mpv"                                          # used by preview; may carry flags

[rate_limit]
upload   = "unlimited"   # or "500k", "2M", "1.5G", a bare byte count, "0"
download = "unlimited"

[port]
low  = 51413   # 0/0 = let the OS pick a random port
high = 51433    # a range is tried in order until one binds

[network]
dht        = true
pex        = true
lsd        = true    # accepted, but has no effect -- see Limitations
encryption = true
seed       = true     # keep uploading after a torrent completes

[log]
level         = "info"    # debug, info, warn, error
library_level = "warn"     # the torrent library's own messages, filtered separately
file          = ""          # empty = stderr, which is what a service manager wants

[keybinds]
# action = "key", overriding internal/tui's defaults. Values are matched
# against bubbletea's key names (bubbletea's KeyMsg.String()): most
# printable characters are themselves, others are e.g. "enter", "esc",
# "ctrl+c", "tab".
# quit = "Q"
```

Known actions for `[keybinds]`: `up`, `down`, `top`, `bottom`, `search`,
`command`, `select`, `remove`, `remove_data`, `pause`, `recheck`,
`detail`, `back`, `quit`, `help`, `preview`, `focus_next`, `focus_prev`, `tab_next`,
`tab_prev`, `filter_next`, `filter_prev`. (`dd`, vim's double-tap remove,
is a fixed alias for `remove` and isn't itself rebindable; nor are the
`1`/`2`/`3` detail-tab shortcuts or `+`/`-` file priority.)

## Themes

Built in: `dracula`, `nord`, `gruvbox`, `solarized-dark`, `solarized-light`,
`catppuccin`, `tokyo-night`, and `plain` (a 16-color-safe fallback with no
truecolor hex codes, for terminals with no real color support).

Truecolor-to-256/16-color degradation is handled automatically by
lipgloss/termenv based on the terminal's detected color profile (and
`$COLORTERM`) -- themes don't need separate variants per color depth.

To customize or add a theme, drop a TOML file at
`~/.config/torrnado/themes/<name>.toml` (matching `theme = "<name>"` in
config.toml) with all ten colors set:

```toml
background  = "#1a1b26"
foreground  = "#c0caf5"
muted       = "#565f89"
accent      = "#7aa2f7"
success     = "#9ece6a"
warning     = "#e0af68"
error       = "#f7768e"
border      = "#292e42"
selected_bg = "#292e42"
selected_fg = "#c0caf5"
```

A file matching a built-in theme's name overrides that built-in.

## TUI layout

Three panes plus a status line. The focused pane is the one with the
highlighted border, and it's where `j`/`k` go:

```
┌ sidebar ─┐┌ torrent list ─────────────────────────────────┐
│ torrnado ││  Name                Size Status  ↓ Speed  ETA │
│          ││> ubuntu-24.04.iso  5.9GiB downl…  ↓ 21M/s  3m56│
│ Status   ││  ━━━━━━━━━━━───────────                        │
│  All     ││                                                │
│  Downl…  │└────────────────────────────────────────────────┘
│  Seeding │┌ detail ────────────────────────────────────────┐
│  Complet…││ ─ [Pieces]  Peers   Files                      │
│  Stopped ││ 1950/24208 pieces × 256.0KiB                   │
└──────────┘└────────────────────────────────────────────────┘
 ↓ 21.4MiB/s  ↑ 0B/s  │  2 torrents        j/k select  h help
```

- **Sidebar** filters the list by status. It intersects with `/` search
  rather than replacing it.
- **List** shows one torrent per two lines: the data columns, and a thin
  progress underline beneath the name (absent once complete).
- **Detail pane** always tracks the cursor torrent -- there is no separate
  full-screen detail view. Its three tabs are the piece completion map,
  the connected-peer table, and the file list.

## Running it on a server

The daemon is the whole program; the TUI and the CLI are just clients. So
a headless box that downloads things is `torrnado daemon` running under
whatever supervises processes there (systemd, launchd, tmux -- it makes
no difference to torrnado), driven over SSH.

**It comes back after a restart.** Every change is written to
`<state_dir>/session.json` -- the torrent list, paused state, save paths,
per-torrent rate limits, per-file priorities, when each was added -- next
to a copy of each torrent's metainfo in `<state_dir>/torrents/`. On start
the daemon re-adds them all, and logs how many came back. Data already on
disk is not re-downloaded or re-verified: the piece-completion database
was always persistent, and this is what tells the daemon which torrents
to look it up for.

A session file that cannot be read is logged and skipped, one bad record
at a time. A server that refuses to boot over a malformed record is worse
than one that comes back with fewer torrents, so it starts either way --
`journalctl` is where you find out which happened.

**Logs** go to stderr as text with timestamps and levels, which is what a
service manager wants: journald timestamps and stores it with no further
configuration.

```
time=2026-08-05T13:26:28.488-04:00 level=INFO msg="daemon starting" pid=83154 ...
time=2026-08-05T13:26:28.491-04:00 level=INFO msg="session restored" torrents=2
time=2026-08-05T13:26:28.491-04:00 level=INFO msg="daemon ready" socket=... stream=127.0.0.1:64853
```

Set `log.file` to write to a file instead. `SIGHUP` reopens it, which is
what logrotate needs after renaming the old one away -- without that the
daemon goes on writing to a file that no longer has a name, and the disk
never gets the space back.

The torrent library's own output is captured into the same stream at
`log.library_level`, tagged `src=torrent`. It is much noisier than
torrnado is -- it reports every tracker that misbehaves -- which is why
it has its own level rather than sharing `log.level`. Panics still go
straight to stderr through the Go runtime, whatever `log.file` says, so
leave stderr attached to something.

**Two things to know before deploying:**

- **`CGO_ENABLED=0` changes the piece-completion database.** With cgo it
  is SQLite (`.torrent.db` in the download directory); without it, bbolt
  (`.torrent.bolt.db`). Neither reads the other's file, so a data
  directory populated by one build re-verifies from scratch under the
  other. Pin the choice deliberately when you build for a server, and
  keep it pinned.
- **Previews are loopback-only.** The stream server binds `127.0.0.1` on
  an OS-assigned port with a per-session token, so `torrnado preview`
  URLs from a remote daemon are only reachable through an SSH tunnel
  (`ssh -L 8080:127.0.0.1:<port> server`). That is the intended design,
  not an oversight: there is no authentication anywhere in torrnado
  beyond the filesystem permissions on the socket, so nothing it serves
  should be reachable from a network.

There is no remote control protocol. The socket is local by
construction, and SSH already solves the problem properly.

## Live preview (streaming while downloading)

Press `v` on a file in the detail pane's Files tab and it opens in your player
immediately -- no need to wait for the torrent to finish. Seeking works too:
jump to any point and the pieces you land on are fetched first.

```sh
torrnado preview <torrent-id> <file-index>          # print the stream URL
torrnado preview <torrent-id> <file-index> --play   # open it in the player
```

The daemon serves the file over a loopback HTTP URL backed by a
`torrent.Reader`, whose reads block until the data arrives and whose position
is what drives which pieces the client asks for. Requesting a preview resumes
the torrent and raises the file's priority, because a paused or unwanted file
cannot stream at all.

Handing the on-disk path to a player would not work, which is why this exists:
until every piece of a file is present it lives at `<name>.part`, sparse and
filled out of order, so a reader of it sees zeros wherever pieces haven't
landed yet.

The URL binds `127.0.0.1` and carries a token that lasts only as long as the
daemon that issued it. `player` in config.toml chooses the command (default
`mpv`); it may carry flags (`player = "mpv --no-terminal"`) and is split on
spaces rather than run through a shell, so the URL is never a shell-injection
surface. The player is detached, so it keeps playing after you quit the TUI.

> On macOS, a player installed as an `.app` may be blocked by Gatekeeper the
> first time torrnado launches it ("Apple could not verify..."). That's the OS,
> not torrnado: approve it once in System Settings → Privacy & Security, or
> `xattr -d com.apple.quarantine /Applications/<player>.app`.

## TUI keybinds

Vim-like navigation, not vim's editing model -- there's no insert/visual
mode, just the movement/action idioms.

| Key                | Action                                            |
|--------------------|----------------------------------------------------|
| `j` / `k`          | down / up within the focused pane                  |
| `g` / `G`          | top / bottom                                       |
| `tab` / `shift+tab`| move focus between list, detail pane and sidebar   |
| `]` / `[`, `1`-`3` | switch the detail pane's tab                       |
| `}` / `{`          | cycle the sidebar's status filter                  |
| `/`                | search / filter by name                            |
| `space`            | toggle selection (for batch operations)            |
| `x`, `dd`          | remove selected (or cursor row), keep data on disk |
| `D`                | remove selected (or cursor row), delete data too   |
| `p`                | toggle pause/resume on selected (or cursor row)    |
| `r`                | force recheck on selected (or cursor row)          |
| `enter`            | move focus into the detail pane                    |
| `esc`              | focus back to the list, then clear selection / search / filter |
| `:`                | open the command palette                           |
| `v`                | stream the selected file to your player (Files tab) |
| `h`                | keybind & command reference                        |
| `q`                | quit the TUI (the daemon keeps running)             |

With the detail pane focused on its Files tab, `j`/`k` move between files
and `+`/`-` raise/lower the selected file's priority. On the other tabs
`j`/`k` scroll. Actions (`p`, `r`, `x`, `D`, `:`) work from any pane and
always apply to the list's selection or cursor row.

### Command palette

`:`-prefixed, vim ex-mode style:

| Command                                              | Effect                                    |
|-------------------------------------------------------|--------------------------------------------|
| `:add <magnet\|file\|url\|dir\|glob\|magnet-list-file> ...` | add one or more torrents (see Batch add)  |
| `:remove` / `:remove!` `[id]`                          | remove (without/with data); selection or cursor row if no id |
| `:pause [id]` / `:resume [id]`                         | absolute pause/resume; selection or cursor row if no id |
| `:recheck`                                             | force recheck on selection or cursor row  |
| `:limit-up <rate>` / `:limit-down <rate>`               | set the *global* rate limit (`500k`, `2M`, `unlimited`) |
| `:move <dir>`                                           | move the cursor row's data to a new directory |
| `:sort name\|size\|progress\|ratio\|eta\|added\|down\|up [desc]` | change list sort order |
| `:q` / `:quit`                                          | quit the TUI                              |

### Batch add

`:add` (and `torrnado add` on the CLI) accepts any mix of:

- a magnet URI
- a `.torrent` file path
- an `http://` or `https://` URL to a `.torrent` file (downloaded to a
  temp file and added from there)
- a directory (every `.torrent` file directly inside it, non-recursive)
- a glob pattern (`~/torrents/*.torrent`) -- handled by torrnado itself
  as well as by your shell, so it also works quoted or on shells that
  don't glob
- a text file listing one magnet URI per line (blank lines and `#`
  comments ignored)

```sh
torrnado add 'magnet:?xt=urn:btih:...'
torrnado add ~/downloads/some.torrent
torrnado add https://torrent.fedoraproject.org/torrents/Fedora-COSMIC-Live-x86_64-44.torrent
torrnado add ~/torrents/*.torrent
torrnado add ~/torrents/            # every .torrent file in the directory
torrnado add magnets.txt             # one magnet uri per line
```

## CLI reference

```
torrnado                              attach the TUI (spawns a daemon if needed)
torrnado daemon                       run the engine in the foreground
torrnado add <sources...>             add torrent(s); --save-path, --paused
torrnado remove <id...>               remove; --delete-data
torrnado pause <id...>
torrnado resume <id...>
torrnado recheck <id...>
torrnado priority <id> <file-index> <none|low|normal|high|now>
torrnado limit <up|down> <rate>       global by default; --torrent <id> for per-torrent
torrnado move <id> <new-directory>
torrnado list                         tabular snapshot of every torrent
torrnado list --watch                 redraw live until interrupted (-w)
torrnado preview <id> <file-index>    print a stream URL; --play opens it
```

`list --watch` renders the daemon's pushed events rather than polling, so
it updates when state actually changes (~1s) and costs no extra requests.
On a terminal it redraws in place; piped to a file or a pager it appends
plain frames with no escape codes, so `torrnado list -w | tee log` works.

Every subcommand accepts `--config <path>` to use a config file other than
the XDG default. Torrent ids are hex-encoded info hashes, as printed by
`add` and `list`.

## anacrolix/torrent limitations hit while building this

The engine wrapper (`internal/engine`) exists specifically to absorb
these; they're documented here (and next to the relevant code) rather
than left as a surprise:

- **No per-torrent rate limiting.** `ClientConfig.UploadRateLimiter` /
  `DownloadRateLimiter` are client-wide only; there's no hook to throttle
  a single `Torrent`'s network I/O. Global limits (`:limit-up`,
  `:limit-down`, `torrnado limit`) are exact, enforced by the library
  itself via `golang.org/x/time/rate`. Per-torrent limits
  (`--torrent <id>`) are a **best-effort approximation**: each engine
  tick (~1s) compares a torrent's measured throughput against its
  configured cap and toggles `Allow/DisallowDataDownload/Upload`
  accordingly. This averages out close to the limit over ~1s windows but
  is bursty, not a smooth token bucket.
- **No native pause/resume.** Implemented via
  `Allow/DisallowDataDownload` and `Allow/DisallowDataUpload`, which stop
  a torrent from requesting or serving piece data while leaving peer
  connections intact.
- **No live "move storage" API.** The default file storage backend has
  no in-place relocate. `MoveStorage` pauses the torrent, moves files on
  disk itself, re-adds the torrent pointed at a new
  `storage.NewFile(newDir)`, and re-verifies data in the background (a
  hash check against files already on disk, not a re-download). This
  also means a move resets any custom per-file priorities back to normal
  -- the re-added `Torrent` is a fresh instance with no priority history.
- **`(*torrent.File).Priority()` doesn't reflect
  `(*torrent.Torrent).DownloadAll()`/`DownloadPieces()`.** Those set
  piece priorities directly; `File.Priority()` only ever returns what was
  last passed to `File.SetPriority()`, a separate bookkeeping field. A
  torrent started with `DownloadAll()` downloads correctly but would
  report every file's priority as "none" forever. The engine avoids
  `DownloadAll()` entirely and marks new torrents' files wanted via
  `File.SetPriority()` file-by-file instead, so the files list is
  accurate.
- **No level between "not wanted" and "wanted normally".** The piece
  priority scale is `None < Normal < High < Readahead < Next < Now`, so a
  "low priority, but still wanted" file (as most torrent clients offer)
  has no faithful equivalent; `priority ... low` degrades to `normal`.
- **`Files()` and `NumPieces()` require metadata to already be
  available**, and aren't merely slow or empty before then -- they
  dereference a nil pointer and crash the whole process (every torrent in
  the daemon, not just the one being touched) if called first. A magnet
  add can spend an arbitrary (even unbounded, for a dead swarm) amount of
  time in this state, so every engine call that touches a torrent's files
  checks `Info() != nil` first rather than assuming metadata has arrived.
- **No Local Service Discovery (LSD/BEP 14).** There's no config field or
  hook for local-network peer discovery anywhere in the library. The
  `network.lsd` config key and `:lsd`-adjacent toggles are accepted (so a
  config file written against this spec doesn't fail validation) but do
  nothing.
- **No live per-tracker status.** No last-announce time, last error, or
  tracker-reported seeder/leecher counts are exposed -- the tracker list
  is the static URL/tier list from the torrent's metainfo, not a live
  announce log.
- **Peer choke state isn't exported.** `Peer.choking` / `Peer.peerChoking`
  are unexported with no accessor; the only public path to them is
  `Client.WriteStatus`, which dumps the entire client's status as free
  text. The peers table therefore has no Choked column -- it shows how
  each peer was discovered instead, rather than scraping a debug dump or
  guessing.
- **No instantaneous per-peer transfer rate.** `PeerStats.DownloadRate` is
  a lifetime average (useful bytes / total time spent expecting data), so
  it barely moves once a connection has been up a while. The peers table's
  ↓/↑ figures are computed the same way the per-torrent speeds are: byte
  counter deltas between engine ticks (~1s).
- **Piece completion lags byte progress, by a lot.** A torrent's progress
  percentage comes from `bytesLeft()`, which counts *received* bytes
  (including unverified "dirty" chunks), while `PieceState.Complete` only
  goes true once a piece has been hash-checked and marked in storage.
  There is no API to observe the verification backlog, and it drains
  slowly: a torrent showing 100% routinely reports under half its pieces
  as verified for minutes afterwards. The Pieces tab therefore says
  "verified", not "complete" -- the same number under a "complete" label
  would read as data loss. Separately, `PieceState.Complete` is gated on
  `storage.Completion.Ok`, so a piece whose storage state has never been
  consulted is *unknown* rather than missing; `engine.PieceRun` carries
  that as its own flag and the map dims it rather than drawing it as a
  hole.
- **Force-recheck is quadratic on large torrents.** Verifying a piece ends
  in `filePieceImpl.MarkComplete`, which calls `allFilePiecesComplete` --
  and that scans *every* piece of the file through the SQLite piece
  completion database. A full recheck marks every piece, so an N-piece
  single-file torrent costs O(N²) completion lookups: a 5.9 GB ISO (24208
  pieces of 256 KiB) needs ~586 million of them and will sit in
  `checking` at ~170% CPU for hours. The hashing itself finishes quickly
  -- the Pieces tab shows all pieces verified long before
  `VerifyDataContext` returns. There is no fix in v1.61.0 (the newest
  release) and no way to cancel a running recheck; restarting the daemon
  is the only way out. **Avoid `r` / `torrnado recheck` on multi-GB
  torrents.**
- **Force-recheck can panic the whole daemon.** `checkPendingPiecesMatches‑
  RequestOrder` is an internal consistency assertion reached from
  `pieceHashed` → `setPieceCompletion` → `openNewConns` → `needData`. It
  panics with "piece request order has {} and pending pieces has {...}"
  when a recheck marks pieces pending that aren't in the request order.
  The panic happens on a library-owned goroutine, so no `recover()` in
  this codebase can catch it -- the daemon dies and takes every torrent
  with it.
- **No port-range binding.** `ClientConfig.ListenPort` is a single fixed
  port (0 = random); `[port] low`/`high` in config is implemented by
  retrying `torrent.NewClient` across the range until one port binds.

## Non-goals / caveats

- Built and tested on macOS/Linux. The daemon-detach mechanism
  (`SysProcAttr.Setsid`) is POSIX-only; on other platforms the daemon
  still runs in the background, just not in its own session. The
  Unix-socket IPC design itself doesn't extend to Windows without a
  different transport (named pipes), which is out of scope here.
