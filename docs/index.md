# torrnado

A terminal BitTorrent client: a vim-like TUI on top of a torrent engine
that runs as a background daemon.

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

## The idea

The daemon keeps running after the TUI exits. Start a download, close the
terminal, come back tomorrow — it is still going, and the interface
reattaches to it.

That is the one design decision everything else follows from. The TUI and
the CLI are both thin clients that talk to the engine over a local Unix
socket, so neither of them owns the torrents; quitting either one is not
an event the daemon notices.

<div class="grid cards" markdown>

-   __Start here__

    ---

    Build it, add a torrent, learn six keys.

    [Installation](getting-started/installation.md) ·
    [Quick start](getting-started/quick-start.md)

-   __Drive it__

    ---

    The three panes, the command palette, and every key.

    [The TUI](guide/tui.md) · [Command line](guide/cli.md)

-   __Leave it running__

    ---

    A headless box that downloads things, under systemd or Docker.

    [The daemon](server/daemon.md) · [systemd](server/systemd.md) ·
    [Docker](server/docker.md)

-   __Make it yours__

    ---

    Config, keybinds, eight built-in themes and your own.

    [Configuration](guide/configuration.md) · [Themes](guide/themes.md)

</div>

## What it does

- **Runs detached.** One daemon; the TUI and CLI attach and detach freely.
- **Survives restarts.** The torrent list, paused state, save paths, rate
  limits and per-file priorities are written to disk and restored.
- **Streams while downloading.** Press ++v++ on a video and it opens in
  your player at once, seeking included — the read position drives which
  pieces are fetched.
- **Scripts.** Every action is a subcommand, so `torrnado add`,
  `torrnado list` and friends work in a shell script or a cron job.
- **Stays out of the way.** Vim-like keys, no mouse, no configuration
  required to start.

## What it does not do

No remote control protocol, no web UI, no Windows. The socket is local by
construction and SSH already solves the remote problem properly — see
[non-goals](reference/caveats.md) for the full list and the reasoning.

## Requirements

Go 1.25+ to build, a POSIX system to run (Linux and macOS are tested), and
a media player if you want the streaming preview.
