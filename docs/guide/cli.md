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
torrnado status                       is a daemon running, and what is it doing; --quiet, --json
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
torrnado find <query...>              search through Jackett; --limit, --json, --link N
torrnado list                         tabular snapshot of every torrent
torrnado list --watch                 redraw live until interrupted (-w)
torrnado list --json                  the same as a JSON array, for scripts
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
When the daemon is not the build running the command, it ends with a
warning saying so - see [Non-goals and caveats](../reference/caveats.md).

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

## Machine-readable output

`list` and `status` take `--json`, for a script to read instead of a table to
scrape. The shapes below are a contract: a key is not renamed or removed
except as a change the changelog marks as breaking, and a test fails if one
changes by accident.

A few conventions hold everywhere. Sizes are bytes and rates are bytes per
second, as plain numbers. A fraction is 0 to 1, not a percentage. Times are
RFC 3339, in UTC. Keys are never left out. And `null` means *not known*,
used wherever zero would be a different answer: no ratio yet is not a ratio
of nought, and an unknown ETA is not an ETA of now.

## Searching

`torrnado find` searches through [Jackett](https://github.com/Jackett/Jackett),
a service you run yourself that holds the per-tracker setup and
credentials. torrnado never talks to a tracker, so a tracker that needs a
login or a browser challenge is Jackett's concern, not ours.

Search is off until you turn it on, and says so when it is not:

```
$ torrnado find ubuntu
error: search is not configured - set search.jackett.enabled, url and api_key in the config, then restart the daemon (torrnado stop)
```

See [Searching](configuration.md#searching) for the keys. The daemon does the
searching, so it has to be restarted after the keys change.

```sh
torrnado find ubuntu 24.04              # a table, best-seeded first
torrnado find ubuntu --limit 0          # every result, not the first 25
torrnado find ubuntu --link 1           # one result's magnet or URL, nothing else
torrnado add "$(torrnado find ubuntu --link 1)"
torrnado find ubuntu --json | jq -r '.[0].link' | xargs torrnado add
```

A count a tracker did not report prints as `-`, not `0`: zero seeders means
a torrent that will never finish, and a tracker that keeps its counts to
itself should not be made to say so. In `--json` the fields are `title`,
`size` (bytes), `seeders`, `leechers` (`-1` when not reported), `tracker`
and `link` (a `magnet:` URI or a `.torrent` URL, empty if there was
neither).

### `torrnado list --json`

An array with one object per torrent - `[]` when there are none, never
`null`. For example:

```json
[
  {
    "id": "6a66e9f7ac06546d2b8c85a59b0c4011a6910cdf",
    "name": "debian-12.iso",
    "state": "downloading",
    "paused": false,
    "progress": 0.4213,
    "size": 662700032,
    "completed": 279191552,
    "downloaded": 279191552,
    "uploaded": 41943040,
    "download_rate": 9740800,
    "upload_rate": 262144,
    "ratio": 0.15,
    "peers": 31,
    "seeds": 12,
    "eta_seconds": 39,
    "check_progress": null,
    "label": "linux",
    "save_path": "/home/you/Downloads/torrnado",
    "data_dir": "/home/you/Downloads/torrnado",
    "added_at": "2026-11-11T01:13:44.06344Z",
    "download_limit": 0,
    "upload_limit": 0,
    "error": ""
  }
]
```

| Key | Meaning |
| --- | --- |
| `id` | the hex info hash, as every command takes it |
| `state` | one of `checking`, `queued`, `paused`, `downloading`, `seeding`, `error`, `blocked` (held by the VPN guard), `low_disk` (held by the free-space guard), `unknown` |
| `paused` | whether the torrent is paused. **Read this, not `state`**: a paused torrent being hash-checked reports `checking`, so `state` alone would call it running |
| `progress` | 0 to 1 |
| `size` | total bytes; `0` until a magnet's metadata arrives |
| `completed` | bytes on disk and wanted |
| `downloaded`, `uploaded` | totals for the life of the torrent, across restarts |
| `download_rate`, `upload_rate` | current bytes per second |
| `ratio` | uploaded over downloaded. `null` when nothing has been downloaded, which is also how a torrent seeding data that was already on disk looks |
| `peers`, `seeds` | connected peers, and how many of them are seeds |
| `eta_seconds` | `null` when it is not known: the speed is negligible, or there is nothing left to wait for |
| `check_progress` | 0 to 1 while a hash check is running, otherwise `null` |
| `label` | `""` when unlabelled |
| `save_path`, `data_dir` | where it saves, and the directory holding its files |
| `added_at` | `null` for a daemon too old to record it |
| `download_limit`, `upload_limit` | this torrent's cap in bytes per second; `0` for none |
| `error` | `""` when there is none |

`--json` cannot be combined with `--watch`: a watch redraws a table, and
there is no single document to print.

```sh
# names of everything downloading
torrnado list --json | jq -r '.[] | select(.state == "downloading") | .name'

# remove what has finished, by id
torrnado list --json | jq -r '.[] | select(.progress == 1) | .id' | xargs torrnado remove
```

### `torrnado status --json`

One object. Whether a daemon is *running* and whether it is *answering* are
separate fields, because they are separate facts: a daemon that holds its
lock but does not answer is most likely busy hash-checking, and taking it for
a dead one means two engines on one data directory. Everything the daemon
reports about itself is `null` unless it answered.

```json
{
  "running": true,
  "answering": true,
  "pid": 83427,
  "socket": "/home/you/.local/share/torrnado/daemon.sock",
  "lock": "/home/you/.local/share/torrnado/daemon.sock.lock",
  "version": "0.10.1 (da4134de07be)",
  "client_version": "0.10.1 (da4134de07be)",
  "version_skew": false,
  "started_at": "2026-11-11T01:12:42.574174Z",
  "uptime_seconds": 3600,
  "listen_port": 51413,
  "transfers": {
    "torrents": 2,
    "download_rate": 0,
    "upload_rate": 0,
    "download_limit": 0,
    "upload_limit": 0
  },
  "vpn": { "required": false, "active": false, "interface": "" },
  "disk": { "free_bytes": 234632028160, "total_bytes": 494384795648, "low": false, "min_free_bytes": 0 }
}
```

`version`, `started_at`, `uptime_seconds` and `listen_port` are `null` from a
daemon too old to report them, which is ordinary: a daemon outlives the
clients that talk to it.

`client_version` is the build of the torrnado that ran the command, and is
always there. `version_skew` compares it with the daemon's, the same check the
TUI and the text report make: `true` when the daemon is a different build -
which includes one too old to report a version at all - `false` when it is the
same, and `null` when there is nothing to compare, because no daemon answered.
That makes the stale-daemon-after-an-upgrade case something a script can act on:

```sh
# restart a daemon left running across an upgrade (the next command starts a fresh one)
torrnado status --json | jq -e '.version_skew' >/dev/null && torrnado stop
``` `pid` is `null` when it cannot be named, and `disk`
when the filesystem cannot be read. `vpn.active` only means something while
`vpn.required` is true.

Like the text report, it never starts a daemon, and the exit status is `0`
whether or not one is running - the answer is in the document. For a shell
condition use `--quiet` instead, which prints nothing and exits `1` when
nothing is running. The two cannot be combined.

```sh
torrnado status --json | jq -e '.answering' >/dev/null || echo "daemon is not answering"
```
