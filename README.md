# torrnado

A terminal BitTorrent client: a vim-like TUI ([bubbletea]/[lipgloss]) on
top of a torrent engine ([anacrolix/torrent]) that runs as a background
daemon. The TUI and CLI are both thin clients that talk to that daemon
over a local Unix socket — which is what makes detached operation (start
a download, close the terminal, it keeps going) possible without a
separate architecture.

[![ci](https://github.com/lestex/torrnado/actions/workflows/ci.yml/badge.svg)](https://github.com/lestex/torrnado/actions/workflows/ci.yml)

**Documentation: [torrnado.dev](https://torrnado.dev)**

```
┌──────────────────┐┌────────────────────────────────────────────────────────────────────┐
│ torrnado         ││    Name                  Progress            Size    Status        │
│                  ││ >  archlinux-2026.08.01… ━━━━━━━────── 42%   1.5GiB  downloading   │
│ Status           ││    Fedora-KDE-Desktop-L… ━━━━━━━━━━━━ 100%   3.1GiB  seeding       │
│  All             ││ *  ubuntu-26.04-desktop… ━━──────────  11%   6.1GiB  paused        │
│  Downloading     ││                                                                    │
│  Seeding         │└────────────────────────────────────────────────────────────────────┘
│  Completed       │┌────────────────────────────────────────────────────────────────────┐
│  Stopped         ││  ─ [Pieces]  Peers   Files                                         │
│                  ││  1950/24208 pieces verified × 256.0KiB                              │
└──────────────────┘└────────────────────────────────────────────────────────────────────┘
 ↓ 21.4MiB/s  ↑ 0B/s  │  3 torrents                              added 1 torrent(s)
```

## Install

Grab an archive for your platform from the [releases
page](https://github.com/lestex/torrnado/releases), or build it:

```sh
go build -o torrnado ./cmd/torrnado   # requires Go 1.25+
```

There is no install step beyond putting the binary on your `$PATH`.

## Use

```sh
torrnado add 'magnet:?xt=urn:btih:...'   # spawns a daemon if none is running
torrnado                                  # attach the TUI
torrnado list --watch                     # or just watch from the shell
```

Quitting the TUI (`q`) does not stop the daemon — your torrents keep
running. Press `h` in the TUI for every key, and `:` for the command
palette.

## What it does

- **Runs detached.** One daemon; the TUI and CLI attach and detach freely.
- **Survives restarts.** The torrent list, paused state, save paths, rate
  limits and per-file priorities are restored on start.
- **Streams while downloading.** `v` on a video opens it in your player
  immediately, seeking included.
- **Opens the folder.** `o` shows a torrent's files in Finder, your file
  manager, or whatever you configure.
- **Waits for your VPN.** Optionally holds every transfer until the system
  is on one, and lets them go again when it reconnects.
- **Scripts.** Every action is a subcommand.
- **Themes.** Eight built in, plus your own; `:theme` switches live.

## Documentation

Everything else lives at **[torrnado.dev](https://torrnado.dev)**:

| | |
|---|---|
| [Installation](https://torrnado.dev/getting-started/installation/) · [Quick start](https://torrnado.dev/getting-started/quick-start/) | build it and add a torrent |
| [The TUI](https://torrnado.dev/guide/tui/) · [Command line](https://torrnado.dev/guide/cli/) | panes, keys, palette, every subcommand |
| [Configuration](https://torrnado.dev/guide/configuration/) · [Themes](https://torrnado.dev/guide/themes/) | config schema, keybinds, palettes |
| [Streaming preview](https://torrnado.dev/guide/streaming/) | play a file while it downloads |
| [The daemon](https://torrnado.dev/server/daemon/) · [systemd](https://torrnado.dev/server/systemd/) · [Docker](https://torrnado.dev/server/docker/) | leave it running on a box |
| [Library limitations](https://torrnado.dev/reference/limitations/) | the anacrolix/torrent traps this works around |
| [Development](https://torrnado.dev/reference/development/) | layout, tests, how to work on it |

## Contributing

`make check` — gofmt, vet and the unit tests — is the gate every commit
has to pass. `make e2e` drives the built binary through the shell suites,
and `make docker-test` runs all of it on Linux, which is where the daemon
is meant to live. See [Development](https://torrnado.dev/reference/development/).

## License

MIT. See [LICENSE](LICENSE).

[bubbletea]: https://github.com/charmbracelet/bubbletea
[lipgloss]: https://github.com/charmbracelet/lipgloss
[anacrolix/torrent]: https://github.com/anacrolix/torrent
