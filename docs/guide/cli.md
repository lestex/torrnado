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
the XDG default.

All of this is also in `man torrnado`, which every release archive ships
as `torrnado.1` - see [Installation](../getting-started/installation.md#the-man-page).

## Naming a torrent

A torrent's id is its 40-character info hash, as printed by `add` and
`list`. Every command that takes one also takes whatever is enough to pick
a single torrent out of the list, so nobody has to paste a hash:

```sh
torrnado pause 6a66e9f7        # the start of its id, four characters or more
torrnado pause debian          # its name - or part of it, in any case
torrnado remove "Big Buck"     # quote a name that has a space in it
```

An argument is tried as the front of an id first, when it is at least four
hex characters, then as a name that is exactly what was typed, then as a
name that contains it. The first of those that matches anything decides,
and it has to be **exactly one torrent**. An argument that matches two is
refused rather than guessed at, with the candidates listed:

```
$ torrnado pause ab12
failed on ab12: "ab12" matches 2 torrents - name one of them:
  ab12000000000000000000000000000000000001  Twin One
  ab12000000000000000000000000000000000002  Twin Two
error: 1 of 1 failed
```

That rule is what makes `torrnado remove` safe to give a name. When an
argument was resolved, what it picked is printed to stderr - `debian ->
6a66e9f7ac06  debian-12.iso` - so a script reading stdout never sees it. A
full id is taken exactly as given, with no lookup, so existing scripts
behave as they always did.

Given several arguments, a command acts on the ones that resolved and
reports the rest, and its exit status says that something failed.

The lookup is done by the command itself against the daemon's list, so it
works against a daemon of any version.

### Shell completion

Pressing <kbd>Tab</kbd> after a command that takes a torrent offers the
full ids, each described by its name. Completion is generated by
`torrnado completion`; load it for the current session, or install it
once:

=== "zsh"

    ```sh
    source <(torrnado completion zsh)
    # or, once:
    torrnado completion zsh > "${fpath[1]}/_torrnado"                       # Linux
    torrnado completion zsh > $(brew --prefix)/share/zsh/site-functions/_torrnado  # macOS
    ```

=== "bash"

    ```sh
    source <(torrnado completion bash)
    # or, once:
    torrnado completion bash > /etc/bash_completion.d/torrnado                      # Linux
    torrnado completion bash > $(brew --prefix)/etc/bash_completion.d/torrnado      # macOS
    ```

=== "fish"

    ```sh
    torrnado completion fish | source
    # or, once:
    torrnado completion fish > ~/.config/fish/completions/torrnado.fish
    ```

A new shell is needed for an installed completion to take effect. It never
starts a daemon: with none running there is simply nothing to suggest, and
a daemon that does not answer within a second and a half is treated the
same way, so a hung one cannot freeze the prompt.

## Batch add

`:add` (and `torrnado add` on the CLI) accepts any mix of:

- a magnet URI
- a `.torrent` file path
- an `http://` or `https://` URL to a `.torrent` file (downloaded to a
  temp file, added from there, and the temp file removed once the daemon
  has read it) - or one that redirects to a magnet, which is what an
  indexer's download link is for a release with no `.torrent` of its own
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
