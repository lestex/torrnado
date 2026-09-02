---
description: >-
  torrnado's terminal interface: three panes, vim keys, a command palette,
  and a detail pane showing the piece map, connected peers and files.
---

# The TUI

Run `torrnado` with no arguments. It attaches to the daemon, spawning one
if nothing is listening.

## Layout

Three panes plus a status line. The focused pane is the one with the
highlighted border, and it's where `j`/`k` go:

```
┌ sidebar ─┐┌ torrent list ──────────────────────────────────┐
│ torrnado ││  Name          Progress      Size   Status ETA │
│          ││> ubuntu-24.04… ━━━━━──── 62% 5.9GiB downl… 3m56│
│ Status   ││                                                │
│  All     ││                                                │
│  Downl…  │└────────────────────────────────────────────────┘
│  Seeding │┌ detail ────────────────────────────────────────┐
│  Complet…││ ─ [Pieces]  Peers   Files                      │
│  Stopped ││ 1950/24208 pieces × 256.0KiB                   │
└──────────┘└────────────────────────────────────────────────┘
 ↓ 21.4MiB/s  ↑ 0B/s  │  2 torrents                     ? help
```

- **Sidebar** filters the list, by status and by label. Whichever is
  selected intersects with `/` search rather than replacing it.
- **List** shows one torrent per line. Progress is a column - a bar
  followed by its percentage - rather than an underline beneath the name.
  A wide enough pane also shows size, status, both speeds and the ETA; a
  narrow one drops those and keeps the name and progress.
- **Detail pane** always tracks the cursor torrent - there is no separate
  full-screen detail view. Its three tabs are the piece completion map,
  the connected-peer table, and the file list.

## Keys

Vim-like navigation, not vim's editing model - there's no insert/visual
mode, just the movement/action idioms.

`?` and `h` both open the reference, and the footer says so in its right
corner whenever it has nothing else to report - so the keys are reachable
without knowing a key. With no torrents yet, the list says what to do
next rather than only that it is empty:

```
 no torrents yet

   :add <magnet|file|dir>  add a torrent without leaving here
   torrnado add <magnet>   or from a shell, interface or not
   h / ?                   every key and command
```

| Key                | Action                                            |
|--------------------|----------------------------------------------------|
| `j` / `k`          | down / up within the focused pane                  |
| `g` / `G`          | top / bottom                                       |
| `tab` / `shift+tab`| move focus between list, detail pane and sidebar   |
| `]` / `[`, `1`/`2`/`3` | switch the detail pane's tab                    |
| `}` / `{`          | cycle the sidebar's filter, statuses and labels    |
| `/`                | search / filter by name                            |
| `space`            | toggle selection (for batch operations)            |
| `x`, `dd`          | remove selected (or cursor row), keep data on disk |
| `D`                | remove selected (or cursor row), delete data too   |
| `X`                | delete the data, keep the torrent in the list      |
| `p`                | toggle pause/resume; also stops a running recheck   |
| `r`                | force recheck on selected (or cursor row)          |
| `enter`            | move focus into the detail pane                    |
| `esc`              | focus back to the list, then clear selection / search / label / status filter |
| `:`                | open the command palette                           |
| `v`                | stream to your player: the file under the cursor, or the torrent's biggest |
| `o`                | open the torrent's folder in your file manager      |
| `h`, `?`           | keybind & command reference                        |
| `q`                | quit the TUI (the daemon keeps running)             |

With the detail pane focused on its Files tab, `j`/`k` move between files
and `+`/`-` raise/lower the selected file's priority (`=` and `_` do the
same, so neither needs shift). On the other tabs
`j`/`k` scroll. Actions (`p`, `r`, `x`, `D`, `:`) work from any pane and
always apply to the list's selection or cursor row.

### Command palette

`:`-prefixed, vim ex-mode style:

| Command                                              | Effect                                    |
|-------------------------------------------------------|--------------------------------------------|
| `:add <magnet\|file\|url\|dir\|glob\|magnet-list-file> ...` | add one or more torrents (see Batch add)  |
| `:remove` / `:remove!` (`:rm` / `:rm!`)                | remove (without/with data); acts on the selection, or the cursor row |
| `:purge`                                               | delete the data, keep the torrent; selection or cursor row |
| `:pause` / `:resume`                                   | absolute pause/resume; selection or cursor row |
| `:recheck`                                             | force recheck on selection or cursor row  |
| `:limit-up <rate>` / `:limit-down <rate>`               | set the *global* rate limit (`500k`, `2M`, `unlimited`) |
| `:move <dir>`                                           | move the cursor row's data to a new directory |
| `:label [name]`                                         | file the selection (or cursor row) under a label; no name clears it |
| `:sort name\|size\|progress\|ratio\|eta\|added\|down\|up [desc]` | change list sort order |
| `:theme [name]`                                         | open the theme picker, or switch straight to a named theme |
| `:help`                                                 | the same reference `h` opens, for when you are already at the prompt |
| `:q` / `:quit`                                          | quit the TUI                              |

## Labels

A torrent can be filed under one label, and the sidebar grows a **Labels**
section listing the ones in use, most-used first. Selecting one filters
the list to it, exactly as a status does.

```
:label tv shows      # the selection, or the row under the cursor
:label               # no name clears it
```

There is deliberately no step that creates a label and none that deletes
one. A label exists exactly while some torrent carries it: applying one to
the first torrent brings it into being, and clearing it from the last
takes it away again. So there is nothing to tidy up, nothing to
accumulate, and the sidebar can only ever list labels something is
actually filed under - which is also why there is no limit on how many you
may have.

More labels than the sidebar has room for are elided to a `+3 more` line
rather than clipped, so a filter that exists is never invisible. How many
fit is a property of your terminal's height, not a fixed number.

If the label you are filtering by stops existing - you relabelled or
removed the last torrent carrying it - the filter falls back to **All**
rather than leaving you in front of an empty list.

Adding a torrent clears whatever would hide it, and says which:

```
added 1 torrent(s) - cleared the tv label and the search to show them
```

Adding is the one action whose whole purpose is to put something in the
list, so reporting success while the list does not change is the wrong
answer however correct the filter was. Only the filters actually hiding
the new torrent are cleared - a search it already matches is left alone.

`torrnado label` does the same thing from a shell, and `torrnado list`
grows a LABEL column when anything is labelled.

The reference screen lists this table too, generated from the same
definitions the palette reads, so it cannot describe a command that isn't
there or miss one that is. It stacks into a single column when the
terminal is tall enough and splits into two when it isn't, which is what
keeps the commands on screen at 24 rows.

Arguments may be quoted with `'` or `"`, which is what makes an argument
containing a space possible (`:move '/media/big disk'`). Quoting a magnet
is unnecessary here but harmless, which matters because quoting one *is*
necessary in zsh and the habit follows you into the palette.

**++tab++ completes paths** for `:add` and `:move`, the two commands that
take one. It follows a shell's rules: a single match completes, a
directory gains a trailing `/`, and several extend to their common prefix
and list themselves beside the cursor. A name containing a space comes
back quoted, so it survives as one argument. Dotfiles stay out of the way
until you type the dot. A magnet or a URL is left alone - a stray ++tab++
in the middle of one should not mangle it.

A leading `~` is expanded, whether you completed the path or typed it
out. Everything else is literal: the palette is not a shell, and nothing
else expands.

### Batch add

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
