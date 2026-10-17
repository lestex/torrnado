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
torrnado find <query...>              search the daemon's indexers; --json, --link, --limit, --check
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

## Finding a torrent

`torrnado find` searches a self-hosted [Jackett](https://github.com/Jackett/Jackett)
or [Prowlarr](https://github.com/Prowlarr/Prowlarr) server, which fans one query
out to every tracker configured in it. Point torrnado at yours under
`[search.jackett]` or `[search.prowlarr]` in
[config.toml](configuration.md), then:

```sh
torrnado find ubuntu 24.04
```

```
#  TITLE                             SIZE    SEEDS  PEERS  TRACKER
1  ubuntu-24.04.1-desktop-amd64.iso  5.8GiB  1243   87     ubuntu-official
2  ubuntu 24.04 (dead)               5.0GiB  0      0      abandoned
3  Ubuntu 24.04 LTS Server           2.6GiB  -      -      quiet-tracker
```

Arguments are joined with spaces, so a multi-word query needs no quoting.
Results come back healthiest first, because an indexer returns its own
relevance guess and that routinely puts dead torrents at the top.

`0` seeders and `-` are different answers. `0` means a torrent nobody is
seeding, which will never finish. `-` means the tracker did not say, which
is common and holds nothing against it.

With an aggregator, torrnado never contacts a tracker itself: it makes
one HTTP GET against your indexer, usually on localhost, and the indexer
does the rest.

### rutracker.org

rutracker is the exception, and the one site torrnado reads directly. It
needs a login, and it sits behind a Cloudflare challenge, so it also
needs a [FlareSolverr](https://github.com/FlareSolverr/FlareSolverr)
server - which drives a real browser. Nothing else will do: Cloudflare
binds its clearance to the TLS fingerprint of whoever earned it, so a
cookie handed to an ordinary HTTP client is challenged all over again.

```toml
[search.rutracker]
username = "you"
password = "..."

[flaresolverr]
url = "http://localhost:8191"
```

If the login form answers with a captcha, which cannot be answered from
here, log in with a browser and copy its `bb_session` cookie instead:

```toml
[search.rutracker]
session = "..."
```

`torrnado config` reports which of these is set and never what they are,
so its output is safe to paste into a bug report. When the cookie
expires, the error says so and names the key - rather than coming back
with no results, which would look like nothing matched.

This is deliberately one site and not a framework for fifty. Fanning a
query out across many trackers is what Jackett and Prowlarr are for, and
they do it better than a handful of selectors here would.

The daemon does the searching, not the command, so a login and a result
cache outlive any one invocation. `find` dials or spawns a daemon like
every other subcommand.

### Adding what you found

Adding is a separate step, which is what keeps `find` usable in a
pipeline. There is no `LINK` column because a magnet runs past 200
characters and would push everything else off the screen, so the link
comes out through `--link` or `--json`:

```sh
torrnado add "$(torrnado find ubuntu --link 1)"
torrnado find ubuntu --json | jq -r '.[0].link' | xargs torrnado add
```

`--json` prints every field, including the link, and always an array -
`[]` rather than `null` when nothing matched, so `jq` has something to
work with either way.

The link is whatever the indexer gave: a magnet, or an `http(s)` URL to a
`.torrent`. `add` takes either, including the common case of a download
URL that redirects to a magnet, so the same command works whichever one a
result carries.

A rutracker result instead carries a token like `rutracker:6916386`,
because its search page publishes no magnet - only the topic page does.
`add` takes that too: it asks the daemon for the magnet first - the
daemon holds the login - which costs about two seconds for the one result
you picked rather than two minutes for all fifty.

### When it does not work

`--check` separates "nothing matched" from "this was never going to work"
by naming what the daemon will actually search:

```sh
torrnado find --check
# searching: jackett
```

A failure says which config key to look at rather than only what broke:

```
error: jackett: cannot reach http://localhost:9117 (connection refused) - is it running, and is search.jackett.url right?
error: jackett: http://localhost:9117 rejected the API key (HTTP 401) - check search.jackett.api_key
```

With more than one source configured, one being down does not sink the
search: the results that did come back are printed, and the source that
failed is reported as a warning on stderr.

Results are cached for five minutes, so repeating a query does not make
your indexer fan out again.

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
