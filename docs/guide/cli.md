---
description: >-
  Every torrnado subcommand: add, remove, pause, recheck, priority, limit,
  move, list and preview - each one a scriptable client of the daemon.
---

# Command line

Every subcommand is a thin client: it dials the daemon's socket, spawns a
daemon if nothing answers, makes one call and exits. Whatever you can do
in the TUI you can do from a shell script.

## Reference

```
torrnado                              attach the TUI (spawns a daemon if needed)
torrnado daemon                       run the engine in the foreground
torrnado status                       is a daemon running, and what is it doing; --quiet
torrnado stop                         shut the running daemon down; --timeout
torrnado add <sources...>             add torrent(s); --save-path, --paused, --files
torrnado remove <id...>               remove; --delete-data
torrnado purge <id...>                delete the data, keep the torrent
torrnado pause <id...>
torrnado resume <id...>
torrnado recheck <id...>
torrnado priority <id> <file-index> <none|low|normal|high|now>
torrnado limit <up|down> <rate>       global by default; --torrent <id> for per-torrent
torrnado seed-limit <id...>           --ratio / --time; when this torrent stops seeding
torrnado move <id> <new-directory>
torrnado label <name> <id...>         file torrents under a label (--clear removes it)
torrnado list                         tabular snapshot of every torrent
torrnado list --watch                 redraw live until interrupted (-w)
torrnado preview <id> <file-index>    print a stream URL; --play opens it
torrnado open <id...>                 open the torrent's folder
torrnado config                       where the config lives, and what is in effect
torrnado init                         write a config file of the defaults to edit
```

`torrnado label` files torrents under a label the interface can filter by.
The label comes first so the ids can be a list, which is what makes
`torrnado label tv $(...)` work; `--clear` takes the label off instead and
so reads ids only. There is no step that creates a label and none that
deletes one - a label exists while some torrent carries it - and a torrent
has one at a time, so setting another replaces it. `torrnado list` grows a
LABEL column when anything is labelled.

`torrnado status` and `torrnado config` are the two commands that never
start a daemon. That matters more than it sounds: every other subcommand
dials the socket and spawns one when nothing answers, so asking any of
them whether a daemon is running would make the answer yes.

`status` finds the daemon by the exclusive lock it holds beside its
socket, not by process name, so it is right about a daemon running from a
renamed or relocated binary and never confuses one belonging to a
different state directory. It separates three states rather than two:
not running, running and answering, and running but not answering - the
last being a daemon busy enough with a hash check to miss a connection,
which is worth knowing precisely because mistaking it for a dead one is
how two engines end up on a single data directory. `--quiet` prints
nothing and reports the answer as its exit status, for shell conditions.

`torrnado stop` asks that same daemon to shut down - over the socket it
is already listening on - and waits for it to exit, which is when it has
saved its session and closed the engine cleanly. `--timeout 0` returns without waiting. It is deliberately not a
thing to run out of habit: a daemon that is still seeding is doing its
job.

`torrnado config` is the other command that never touches the daemon: it
prints the config file it would read (saying so when there isn't one),
every path derived from it - downloads, state, socket, session file,
saved metainfo - and the settings actually in effect, defaults and
overrides together. Useful when a setting seems to be ignored, since the
first answer is usually that the file is somewhere other than where it
was written. What it prints is what a daemon started *now* would use; one
already running may have been started with something else.

`torrnado init` writes that config file for you, with every key set to
the default it already has and this machine's resolved paths in it - so
configuring something is editing a line rather than transcribing the key
from the documentation. It never overwrites an existing file without
`--force`, and `--print` sends it to stdout instead of to disk. Neither
it nor the file is required: torrnado runs on the built-in defaults, and
deleting a line goes back to one.

`list --watch` renders the daemon's pushed events rather than polling, so
it updates when state actually changes (~1s) and costs no extra requests.
On a terminal it redraws in place; piped to a file or a pager it appends
plain frames with no escape codes, so `torrnado list -w | tee log` works.

Every subcommand accepts `--config <path>` to use a config file other than
the XDG default. Torrent ids are hex-encoded info hashes, as printed by
`add` and `list`.

All of this is also in `man torrnado`, which every release archive ships
as `torrnado.1` - see [Installation](../getting-started/installation.md#the-man-page).

## Batch add

`:add` (and `torrnado add` on the CLI) accepts any mix of:

- a magnet URI
- a `.torrent` file path
- an `http://` or `https://` URL to a `.torrent` file (downloaded to a
  temp file and added from there)
- a directory (every `.torrent` file directly inside it, non-recursive)
- a glob pattern (`~/torrents/*.torrent`) - handled by torrnado itself
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
torrnado add magnets.txt            # one magnet uri per line
```

### Picking files at add time

`--files` says which files inside a torrent to download; everything else
is marked not wanted. Without it a season pack starts pulling the extras
and the sample immediately, and fixing that by hand afterwards is a race
you have already lost.

```sh
torrnado add --files '*.mkv' <magnet>              # videos, at any depth
torrnado add --files '*.mkv,*.srt' <magnet>        # or repeat the flag
torrnado add --files 'Season 1/*' <magnet>         # one folder, not the rest
```

A pattern containing a slash is matched against a file's whole path
inside the torrent; one without is matched against its base name. That is
what makes `*.mkv` find videos in subfolders - the `*` in a glob does not
cross a `/`, so matched against full paths it would find nothing in a
torrent with any folder structure at all.

The choice is applied when the torrent's file list arrives, which for a
magnet is after a peer supplies it - there is nothing to pick from before
that, which is why this is a pattern rather than a picker. It survives a
restart, and the daemon logs what it did:

```
msg="files selected" name="Season 1" selected=3 of=4 patterns=*.mkv
```

If nothing matches, nothing is downloaded and the log says so at warning
level. Widening it back to everything would defeat the point of asking,
and a torrent quietly fetching what you excluded is worse than one that
fetches nothing and tells you.
