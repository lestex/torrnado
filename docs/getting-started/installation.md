---
description: >-
  Install torrnado on macOS or Linux: Homebrew, a one-line install script
  with a checksum check, a release archive, a container image, or a build
  from source.
---

# Installation

## Homebrew

```sh
brew install lestex/tap/torrnado
```

Or `brew tap lestex/tap` once and `brew install torrnado` after. Upgrades
come with `brew upgrade` like anything else, and the man page is installed
with the binary.

The tap is written by the release itself - the cask for a tag is generated
from the same archives and checksums that tag publishes, so it cannot
point at a build that does not exist. It is a cask rather than a formula
because these are pre-compiled binaries: a formula that installs one is
declaring a build it never does. Homebrew takes binary casks on Linux as
well as macOS.

!!! note "The first run on macOS"

    These binaries are not signed or notarized, so macOS quarantines them
    and Gatekeeper refuses to start one - it reports a damaged binary,
    which is not what happened. The cask clears the quarantine attribute
    on install, so `brew install` needs nothing from you here. Unpacking
    an archive by hand does: `xattr -dr com.apple.quarantine ./torrnado`.

## The one-liner

```sh
curl -fsSL https://torrnado.dev/install.sh | sh
```

It works out your platform, downloads that archive from the latest
release, checks it against the release's own `checksums.txt` before
unpacking anything, and puts the binary in `/usr/local/bin` - or
`~/.local/bin` when that is not writable, since a script you piped into a
shell should not be asking for your password. It tells you which, and
whether that directory is on your `PATH`. The man page goes in beside it,
so `man torrnado` works straight away.

Two knobs:

```sh
TORRNADO_VERSION=v0.1.0 curl -fsSL https://torrnado.dev/install.sh | sh   # pin a version
TORRNADO_INSTALL_DIR=~/bin curl -fsSL https://torrnado.dev/install.sh | sh
```

Piping a script into a shell is a real thing to be uneasy about. Read it
first if you would rather - it is
[one file](https://github.com/lestex/torrnado/blob/main/docs/install.sh),
under a hundred lines - or skip it entirely and use the archive directly:

## A released binary

Every tag publishes archives for Linux and macOS on both architectures,
plus `checksums.txt`, on the [releases
page](https://github.com/lestex/torrnado/releases):

```sh
tar xzf torrnado_0.8.2_linux_amd64.tar.gz
./torrnado version
```

Each archive carries the binary, the README, the changelog,
`contrib/torrnado.service` for a systemd install, and `torrnado.1`.

## The man page

The one-liner installs it for you. Doing it by hand from an archive, put
it beside the binary rather than anywhere central:

```sh
install -Dm644 torrnado.1 ~/.local/share/man/man1/torrnado.1   # for ~/.local/bin
sudo install -Dm644 torrnado.1 /usr/local/share/man/man1/torrnado.1
man torrnado
```

Beside it, because that is how `man` finds it: for a `PATH` entry
`<prefix>/bin`, man-db searches `<prefix>/share/man`. So a binary on your
`PATH` brings its own page with it, with nothing to configure and no
index to rebuild - `mandb` is only needed for `apropos`. On Arch in
particular, use `/usr/local`, never `/usr/share/man`, which belongs to
pacman.

It is generated from the command tree at release time rather than kept as
a file someone has to remember to edit, so it describes exactly the binary
it shipped beside - every subcommand, every flag, and the version in its
header. Building from source, `make man` writes the same page.

!!! note "Coming from a source build"

    Release binaries are built without cgo, which selects a pure-Go
    piece-completion database rather than the SQLite one a local `go build`
    produces. The two do not read each other's files, so the first run
    after switching re-verifies data already on disk - once. Nothing is
    lost; it just looks alarming.

## From source

Requires Go 1.26+.

```sh
go build -o torrnado ./cmd/torrnado    # or: make build
```

`make build` stamps the version, commit and date into the binary, so
`torrnado version` says something more useful than "dev".

Run the binary from wherever you like; there's no install step beyond
putting it on your `$PATH`.

## What you get

One binary. It is the daemon, the TUI and the CLI at once - which of the
three you get depends on how you invoke it:

```sh
torrnado              # the TUI, spawning a daemon if none is running
torrnado daemon       # the engine in the foreground
torrnado add <magnet> # a one-shot command against a running daemon
```

There is nothing to install beyond putting that binary on your `$PATH`,
and nothing to configure before the first run - a missing config file is
not an error, only an invalid one.

## Uninstalling

Stop the daemon first:

```sh
torrnado stop
```

Removing the binary does not stop a daemon already running from it - the
system keeps the file alive for as long as the process has it open - so
skipping this leaves torrnado seeding from a binary that no longer exists,
until you reboot or kill it. `torrnado stop` asks the daemon over its
socket, waits for it to save the session and exit, and does nothing when
none is running. The same goes before an upgrade: a daemon started by the
old version keeps serving the new CLI until it is stopped.

Then remove what the install put in place:

=== "Homebrew"

    ```sh
    brew uninstall torrnado         # binary and man page
    brew uninstall --zap torrnado   # ...and the config and daemon state
    ```

    Homebrew cannot stop the daemon for you: its uninstall hooks run
    sandboxed, away from your home directory, where the daemon's socket is.

=== "Install script or archive"

    ```sh
    rm "$(command -v torrnado)"
    rm -f /usr/local/share/man/man1/torrnado.1 ~/.local/share/man/man1/torrnado.1
    ```

    The man page is wherever the binary's `<prefix>/share/man/man1` is -
    beside it, as installed above.

=== "systemd"

    ```sh
    sudo systemctl disable --now torrnado
    sudo rm /etc/systemd/system/torrnado.service /usr/local/bin/torrnado
    sudo systemctl daemon-reload
    sudo rm -r /etc/torrnado /var/lib/torrnado   # config and state
    sudo userdel torrnado
    ```

    `systemctl disable --now` is what stops the daemon here, not
    `torrnado stop`: the service would only start it again.

### What is left behind

Nothing outside these, and none of them is removed by deleting the binary.
`torrnado config` prints where each one is on your machine, which is the
place to look when `$XDG_CONFIG_HOME`, `$XDG_DATA_HOME` or the config
file moved them - `brew uninstall --zap` only knows the defaults.

| What | Default location |
| --- | --- |
| Config and themes | `~/.config/torrnado/` |
| Daemon state: log, socket, session, saved `.torrent` files | `~/.local/share/torrnado/` |
| Piece-completion database | `.torrent.bolt.db` in the download directory (`.torrent.db` from a source build) |
| Your downloads | `~/Downloads/torrnado/` |

The piece-completion database is the easy one to miss, because it lives
in the download directory rather than with the rest of the state - no
uninstall touches it, `--zap` included, since that directory holds your
data. Deleting it only means the next install re-verifies whatever is
still on disk.
