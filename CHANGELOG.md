# Changelog

Every release, newest first. Generated from the commit log by
[git-cliff](https://git-cliff.org) - run `make changelog` rather than
editing this file by hand.

## 0.6.1 - 2026-09-02

### Bug fixes

- A label filter no longer hides a torrent you just added
- Adding a torrent clears every filter that would hide it

## 0.6.0 - 2026-09-02

### Features

- File torrents under labels, and filter the list by them

### Bug fixes

- Make the detail-tab digits rebindable, and honour a paused re-add

## 0.5.8 - 2026-09-02

### Features

- Stop the daemon over the socket instead of by signal
- **release:** Publish a Homebrew cask to lestex/homebrew-tap

## 0.5.7 - 2026-09-02

### Features

- Add torrents dropped into a watched directory

### Dependencies

- Take git-cliff's changelog on stdout

## 0.5.6 - 2026-09-02

### Features

- Complete paths with tab in the command palette

## 0.5.5 - 2026-09-02

### Features

- Hold transfers when the disk is nearly full
- Stop seeding at a ratio or after a time
- Run a command when a torrent finishes

### Bug fixes

- A torrent's ratio survives a restart
- A save cannot land stale, and none can outlive Close

## 0.5.4 - 2026-09-02

### Features

- Record the daemon's pid in its lock file
- Report the daemon's build and uptime in global stats
- Torrnado status
- Torrnado stop
- **BREAKING** Remove the network.lsd config key
- **BREAKING** Seed is off by default

### Bug fixes

- Stop the daemon wedging when a torrent is dropped mid-verification

## 0.5.3 - 2026-09-01

### Bug fixes

- Close attached clients when the daemon shuts down
- Read a torrent's fields under the lock in long operations
- A failed move no longer freezes the torrent
- Expand a leading ~ in config paths

### Dependencies

- Gorilla/websocket 1.5.0 -> 1.5.3 for GO-2026-6278

## 0.5.2 - 2026-08-18

### Features

- **config:** Render an annotated config.toml from a Config
- **cli:** Torrnado init writes a config file to edit
- **tui:** :help opens the reference screen
- **tui:** The help screen lists every palette command
- **tui:** ? opens the help screen alongside h
- **tui:** Point a new user at the palette and the help screen

## 0.5.1 - 2026-08-14

### Dependencies

- Go 1.25.13

## 0.5.0 - 2026-08-13

### Features

- **docs:** The install script puts the man page in too

## 0.4.0 - 2026-08-13

### Features

- **cli:** Ship a man page, generated from the command tree

### Bug fixes

- **docs:** The install script says which kind of download failure it hit
- **engine:** Pausing calls off a running recheck

## 0.3.1 - 2026-08-13

### Bug fixes

- **engine:** A move takes the unfinished data and the file priorities with it
- **engine:** Stop marking wanted the files someone switched off

## 0.3.0 - 2026-08-12

### Features

- **ci:** Publish the container image to ghcr.io on release
- **docs:** An install script at torrnado.dev/install.sh

## 0.2.0 - 2026-08-12

### Features

- A logo, drawn once and used in three places
- **theme:** Add alucard, a light palette from Dracula's own light variant
- **docs:** The site wears the terminal's mark

### Bug fixes

- **docs:** The logo was rendering black in the header
- **tui:** One mark, on the help screen

## 0.1.0 - 2026-08-07

### Features

- **format:** Human-readable bytes, rates, ratios and ETAs
- **engine:** Torrent id, state and priority types
- **engine:** Snapshot, detail and event types
- **engine:** In-memory engine with event subscriptions
- **engine:** Add, remove and pause torrents in memory
- **ipc:** Gob message envelope and request types
- **ipc:** Unix socket server
- **ipc:** Dispatch requests to the engine
- **ipc:** Push engine events to connected clients
- **ipc:** Client with call/reply multiplexing
- **cmd:** Root command and daemon subcommand
- **cmd:** Dial the daemon, spawning one if needed
- **cmd:** List subcommand
- **cmd:** Add subcommand
- **engine:** Start a real torrent client
- **engine:** Track real torrents instead of simulated ones
- **engine:** Real speeds, ratio, ETA and peer counts
- **engine:** Report files, peers and trackers
- **engine:** Pause by disallowing data transfer
- **engine:** Per-file download priorities
- **engine:** Global and per-torrent rate limits
- **engine:** Recheck, move storage, and daemon-wide stats
- **ipc:** Expose the remaining engine operations
- **cmd:** Remove, pause and resume subcommands
- **cmd:** Recheck and priority subcommands
- **cmd:** Limit and move subcommands
- **config:** XDG-aware paths
- **config:** Rate parsing as a TOML-decodable type
- **config:** Load and validate config.toml
- **cmd:** Read settings from config.toml
- **batch:** Expand directories, globs and magnet lists
- **batch:** Fetch .torrent files over http(s)
- **ipc:** Add torrents in one batch call
- **cmd:** Expand add arguments through batch
- **theme:** Named colour palettes with eight built-ins
- **theme:** User themes from TOML files
- **config:** Choose the theme from config.toml
- **tui:** Bubbletea model, messages and event loop
- **cmd:** Open the TUI when run with no arguments
- **tui:** Pane geometry computed from the terminal size
- **tui:** Torrent list pane and footer
- **tui:** Status filter sidebar
- **tui:** Keymap with config overrides
- **tui:** Cursor navigation and selection
- **tui:** Pause, remove and recheck from the list
- **tui:** Search torrents by name
- **tui:** Keybind reference on h
- **tui:** Command palette on :
- **engine:** Piece map and richer peer detail
- **tui:** Docked detail pane with tabs and pane focus
- **tui:** Peers tab
- **tui:** Files tab with priority editing
- **tui:** Piece completion map
- **engine:** Open a file for streaming while it downloads
- **stream:** Serve torrent files over loopback HTTP
- **ipc:** PreviewURL, and start the stream server with the daemon
- **player:** Launch a configurable media player, detached
- **tui:** Stream the selected file with v
- **cmd:** Torrnado preview
- **ipc:** One daemon per socket, enforced by a lock file
- **cli:** List --watch redraws until interrupted
- **tui:** Vim's dd chord for remove
- **tui:** Enter moves focus into the detail pane
- Show how far a hash check has got
- **config:** Logging settings and a state directory
- **logging:** Text logger with levels, to stderr or a file
- **engine:** Route the torrent library's logs through our logger
- **daemon:** Log lifecycle events with timestamps
- **engine:** Log torrent lifecycle events
- **engine:** Persist the torrent session to disk
- **daemon:** Restore torrents on start
- **daemon:** Reopen the log file on SIGHUP
- **docker:** Image for running the daemon, and a linux test target
- **systemd:** Unit file, tested against a real systemd
- **cli:** A config command
- **tui:** Breathing room inside every pane
- **tui:** Progress as a column, one line per torrent
- **tui:** A wider progress bar
- **tui:** The progress bar grows with the terminal
- **tui:** Names come before the progress bar
- **theme:** List every theme that can be loaded
- **tui:** Splice a floating box over the frame
- **tui:** A floating theme picker
- **tui:** A :theme command
- **tui:** A status dot and labelled values in the daemon block
- **vpn:** Detect whether traffic leaves through a tunnel
- **config:** A vpn.required flag
- **engine:** Hold transfers until the system is on a VPN
- **daemon:** Wire the VPN guard to the engine
- **tui:** Show the VPN guard in the sidebar
- **tui:** Fixed-width speeds in the footer
- **engine:** Delete a torrent's data and keep the torrent
- **ipc:** A PurgeData call
- **cli:** Torrnado purge
- **tui:** X deletes the data and keeps the torrent
- **engine:** Report the directory holding a torrent's files
- **config:** An opener command
- **tui:** O opens the torrent's folder
- **cli:** Torrnado open
- **cli:** Torrnado version

### Bug fixes

- **engine:** Reject a .torrent path that does not exist
- **tui:** Give the list the full column height
- **tui:** Bind the focus and tab keys
- **tui:** Give the list a stable order
- **engine:** Refuse a recheck before metadata arrives
- **build:** Ignore an inherited GOROOT
- **engine:** Closing the engine twice is safe
- **engine:** Port 0 really lets the OS choose
- **daemon:** Install signal handlers before the slow startup
- **e2e:** Keep the systemd test out of `make e2e`
- **tui:** Accept quoted arguments in the command palette
- **engine:** Report a hash check from its first tick
- **engine:** Move works, and shows how far its verify has got
- **engine:** Show check progress from a daemon that predates the flag
- **tui:** Status messages clear themselves
- **tui:** The command prompt is a prompt, not a highlight
- **tui:** The cursor and the selection stop hiding each other
- **tui:** The sidebar cursor is visible on the active filter
- **tui:** Don't rely on evaluation order when setting a status
- **tui:** A long status is shown rather than dropped
- **engine:** Delete the .part files too when deleting a torrent's data
- **tui:** V plays a torrent from anywhere, and says why when it cannot
- Fix readme
- **release:** Stop discarding the release notes

### Dependencies

- Silence Material's MkDocs 2.0 banner
- Generate CHANGELOG.md from the commit log
- Bump indirect deps off vulnerable versions
- Keep docs and refactor commits out of the changelog

