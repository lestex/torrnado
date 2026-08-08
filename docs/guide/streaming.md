# Streaming preview

Press `v` on a file in the detail pane's Files tab and it opens in your player
immediately - no need to wait for the torrent to finish. Seeking works too:
jump to any point and the pieces you land on are fetched first.

```sh
torrnado preview <torrent-id> <file-index>          # print the stream URL
torrnado preview <torrent-id> <file-index> --play   # open it in the player
```

The daemon serves the file over a loopback HTTP URL backed by a
`torrent.Reader`, whose reads block until the data arrives and whose position
is what drives which pieces the client asks for. Requesting a preview resumes
the torrent and raises the file's priority, because a paused or unwanted file
cannot stream at all.

Handing the on-disk path to a player would not work, which is why this exists:
until every piece of a file is present it lives at `<name>.part`, sparse and
filled out of order, so a reader of it sees zeros wherever pieces haven't
landed yet.

The URL binds `127.0.0.1` and carries a token that lasts only as long as the
daemon that issued it. `player` in config.toml chooses the command (default
`mpv`); it may carry flags (`player = "mpv --no-terminal"`) and is split on
spaces rather than run through a shell, so the URL is never a shell-injection
surface. The player is detached, so it keeps playing after you quit the TUI.

> On macOS, a player installed as an `.app` may be blocked by Gatekeeper the
> first time torrnado launches it ("Apple could not verify..."). That's the OS,
> not torrnado: approve it once in System Settings → Privacy & Security, or
> `xattr -d com.apple.quarantine /Applications/<player>.app`.

### Switching themes

`:theme` opens a floating picker over the panes. Moving through it
applies each theme as you go - the list, sidebar and detail pane
underneath recolor live, so you judge a theme on your own torrents
rather than on a swatch. `enter` keeps it, `esc` puts back the one you
started with. Your own themes from the themes directory are listed
alongside the built-ins and marked `(user)`; one that fails to parse is
reported and stepped over rather than applied.

`:theme nord` switches straight to a named theme without opening the
picker.

The choice lasts for the session. torrnado will not rewrite your
`config.toml` - doing so would re-encode the file and lose its comments
and ordering - so to keep a theme, put `theme = "nord"` in it yourself.
