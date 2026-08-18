---
description: >-
  Install torrnado on macOS or Linux: a one-line install script with a
  checksum check, a release archive, a container image, or a build from
  source.
---

# Installation

## Arch Linux

```sh
yay -S torrnado-bin       # or paru -S torrnado-bin, or makepkg it by hand
```

[`torrnado-bin`](https://aur.archlinux.org/packages/torrnado-bin) on the
AUR. `-bin` because it installs the released binary rather than compiling
one - it is the same archive the releases page serves, with the checksums
that release published, and the PKGBUILD is written by the release itself
rather than kept up to date by hand.

It brings the man page, and `contrib/torrnado.service` as a real unit at
`/usr/lib/systemd/system/torrnado.service` with its paths corrected for a
packaged install. The `torrnado` service account it runs as is created at
install time through `sysusers.d`. To use it, point `download_dir` at
something the service can write and add that path to `ReadWritePaths=`:

```sh
sudoedit /etc/torrnado/config.toml     # the unit sets XDG_CONFIG_HOME=/etc
sudo systemctl enable --now torrnado
```

Everything the unit expects, and why, is in [The
daemon](../server/daemon.md) and in the comments at the top of the unit
itself.

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
tar xzf torrnado_0.5.1_linux_amd64.tar.gz
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

Requires Go 1.25+.

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
