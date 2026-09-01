# Quick start

Five minutes, from a built binary to a torrent downloading in the
background.

## Add something

```sh
torrnado add 'magnet:?xt=urn:btih:...'
```

!!! tip "Quote the magnet"

    In zsh the `?` and `&` in a magnet URI are glob and job-control
    characters. Quote it, or the shell mangles it before torrnado ever
    sees it. In the TUI's `:add` the quotes are harmless but unnecessary.

There was no daemon running, so that command started one and left it
running. It is still there after the command exits - that is the whole
point of the design.

`add` takes more than magnets: a `.torrent` file, an `http(s)` URL to one,
a directory, a glob, or a file listing magnets one per line. See
[batch add](../guide/cli.md#batch-add).

## Watch it

```sh
torrnado list          # a snapshot
torrnado list --watch  # redrawn until you interrupt it
```

Or open the interface:

```sh
torrnado
```

## Six keys to know

| Key | Does |
|---|---|
| ++j++ / ++k++ | move down / up |
| ++space++ | mark a torrent (actions apply to marks, else the cursor row) |
| ++p++ | pause or resume |
| ++colon++ | the command palette - `:add`, `:theme`, `:limit-down 2M` |
| ++h++ | every other key, generated from your live keymap |
| ++q++ | quit - **the daemon keeps running** |

## Try a theme

Press ++colon++ and type `theme`. The picker floats over the panes and
recolors the interface as you move through it, so you judge a theme on
your own torrents. ++enter++ keeps it, ++esc++ puts back the one you
started with.

## Leave it running

Quitting the TUI does not stop anything. To actually stop the daemon:

```sh
torrnado status     # is one running, and what is it doing
torrnado stop       # and shut it down
```

`stop` waits until the daemon has saved its session and closed cleanly.
It finds the daemon by the lock it holds rather than by process name, so
it works whatever the binary is called. More in
[The daemon](../server/daemon.md).

Worth saying once: a daemon that is still seeding is a normal state to
leave a machine in, so this is not a thing to run out of habit.

## Where things went

```sh
torrnado config
```

prints the config file it reads, the download and state directories, the
socket path, and every setting in effect. It works whether or not a
daemon is running.

## Next

- [The TUI](../guide/tui.md) - the three panes and every key
- [Configuration](../guide/configuration.md) - download directory, rate
  limits, ports, keybinds
- [Running a server](../server/daemon.md) - leave it on a box that is
  always on
