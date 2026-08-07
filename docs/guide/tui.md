# The TUI

Run `torrnado` with no arguments. It attaches to the daemon, spawning one
if nothing is listening.

## Layout

Three panes plus a status line. The focused pane is the one with the
highlighted border, and it's where `j`/`k` go:

```
┌ sidebar ─┐┌ torrent list ─────────────────────────────────┐
│ torrnado ││  Name                Size Status  ↓ Speed  ETA │
│          ││> ubuntu-24.04.iso  5.9GiB downl…  ↓ 21M/s  3m56│
│ Status   ││  ━━━━━━━━━━━───────────                        │
│  All     ││                                                │
│  Downl…  │└────────────────────────────────────────────────┘
│  Seeding │┌ detail ────────────────────────────────────────┐
│  Complet…││ ─ [Pieces]  Peers   Files                      │
│  Stopped ││ 1950/24208 pieces × 256.0KiB                   │
└──────────┘└────────────────────────────────────────────────┘
 ↓ 21.4MiB/s  ↑ 0B/s  │  2 torrents        j/k select  h help
```

- **Sidebar** filters the list by status. It intersects with `/` search
  rather than replacing it.
- **List** shows one torrent per two lines: the data columns, and a thin
  progress underline beneath the name (absent once complete).
- **Detail pane** always tracks the cursor torrent -- there is no separate
  full-screen detail view. Its three tabs are the piece completion map,
  the connected-peer table, and the file list.

## Keys

Vim-like navigation, not vim's editing model -- there's no insert/visual
mode, just the movement/action idioms.

| Key                | Action                                            |
|--------------------|----------------------------------------------------|
| `j` / `k`          | down / up within the focused pane                  |
| `g` / `G`          | top / bottom                                       |
| `tab` / `shift+tab`| move focus between list, detail pane and sidebar   |
| `]` / `[`, `1`-`3` | switch the detail pane's tab                       |
| `}` / `{`          | cycle the sidebar's status filter                  |
| `/`                | search / filter by name                            |
| `space`            | toggle selection (for batch operations)            |
| `x`, `dd`          | remove selected (or cursor row), keep data on disk |
| `D`                | remove selected (or cursor row), delete data too   |
| `X`                | delete the data, keep the torrent in the list      |
| `p`                | toggle pause/resume on selected (or cursor row)    |
| `r`                | force recheck on selected (or cursor row)          |
| `enter`            | move focus into the detail pane                    |
| `esc`              | focus back to the list, then clear selection / search / filter |
| `:`                | open the command palette                           |
| `v`                | stream the selected file to your player (Files tab) |
| `o`                | open the torrent's folder in your file manager      |
| `h`                | keybind & command reference                        |
| `q`                | quit the TUI (the daemon keeps running)             |

With the detail pane focused on its Files tab, `j`/`k` move between files
and `+`/`-` raise/lower the selected file's priority. On the other tabs
`j`/`k` scroll. Actions (`p`, `r`, `x`, `D`, `:`) work from any pane and
always apply to the list's selection or cursor row.

### Command palette

`:`-prefixed, vim ex-mode style:

| Command                                              | Effect                                    |
|-------------------------------------------------------|--------------------------------------------|
| `:add <magnet\|file\|url\|dir\|glob\|magnet-list-file> ...` | add one or more torrents (see Batch add)  |
| `:remove` / `:remove!`                                 | remove (without/with data); acts on the selection, or the cursor row |
| `:purge`                                               | delete the data, keep the torrent; selection or cursor row |
| `:pause` / `:resume`                                   | absolute pause/resume; selection or cursor row |
| `:recheck`                                             | force recheck on selection or cursor row  |
| `:limit-up <rate>` / `:limit-down <rate>`               | set the *global* rate limit (`500k`, `2M`, `unlimited`) |
| `:move <dir>`                                           | move the cursor row's data to a new directory |
| `:sort name\|size\|progress\|ratio\|eta\|added\|down\|up [desc]` | change list sort order |
| `:theme [name]`                                         | open the theme picker, or switch straight to a named theme |
| `:q` / `:quit`                                          | quit the TUI                              |

### Batch add

`:add` (and `torrnado add` on the CLI) accepts any mix of:

- a magnet URI
- a `.torrent` file path
- an `http://` or `https://` URL to a `.torrent` file (downloaded to a
  temp file and added from there)
- a directory (every `.torrent` file directly inside it, non-recursive)
- a glob pattern (`~/torrents/*.torrent`) -- handled by torrnado itself
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
torrnado add magnets.txt             # one magnet uri per line
```

## Command palette

`:`-prefixed, vim ex-mode style:

| Command                                              | Effect                                    |
|-------------------------------------------------------|--------------------------------------------|
| `:add <magnet\|file\|url\|dir\|glob\|magnet-list-file> ...` | add one or more torrents (see Batch add)  |
| `:remove` / `:remove!`                                 | remove (without/with data); acts on the selection, or the cursor row |
| `:purge`                                               | delete the data, keep the torrent; selection or cursor row |
| `:pause` / `:resume`                                   | absolute pause/resume; selection or cursor row |
| `:recheck`                                             | force recheck on selection or cursor row  |
| `:limit-up <rate>` / `:limit-down <rate>`               | set the *global* rate limit (`500k`, `2M`, `unlimited`) |
| `:move <dir>`                                           | move the cursor row's data to a new directory |
| `:sort name\|size\|progress\|ratio\|eta\|added\|down\|up [desc]` | change list sort order |
| `:theme [name]`                                         | open the theme picker, or switch straight to a named theme |
| `:q` / `:quit`                                          | quit the TUI                              |

Arguments may be quoted with `'` or `"`, which is what makes an argument
containing a space possible (`:move '/media/big disk'`). Quoting a magnet
is unnecessary here -- the palette is not a shell, so nothing expands --
but harmless, which matters because quoting one *is* necessary in zsh and
the habit follows you into the palette.

Arguments may be quoted with `'` or `"`, which is what makes an argument
containing a space possible (`:move '/media/big disk'`). Quoting a magnet
is unnecessary here -- the palette is not a shell, so nothing expands --
but harmless, which matters because quoting one *is* necessary in zsh and
the habit follows you into the palette.
