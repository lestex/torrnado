# Themes

Built in: `dracula`, `alucard` (Dracula's light counterpart), `nord`,
`gruvbox`, `solarized-dark`, `solarized-light`, `catppuccin`,
`tokyo-night`, and `plain` (a 16-color-safe fallback with no truecolor hex
codes, for terminals with no real color support).

Truecolor-to-256/16-color degradation is handled automatically by
lipgloss/termenv based on the terminal's detected color profile (and
`$COLORTERM`) - themes don't need separate variants per color depth.

To customize or add a theme, drop a TOML file at
`~/.config/torrnado/themes/<name>.toml` (matching `theme = "<name>"` in
config.toml) with all ten colors set:

```toml
background  = "#1a1b26"
foreground  = "#c0caf5"
muted       = "#565f89"
accent      = "#7aa2f7"
success     = "#9ece6a"
warning     = "#e0af68"
error       = "#f7768e"
border      = "#292e42"
selected_bg = "#292e42"
selected_fg = "#c0caf5"
```

A file matching a built-in theme's name overrides that built-in.

`border` draws the panes' outlines, one cell wide, and every built-in
uses the same colour for it as for `muted`. The palettes these come from
name a "surface" or "current line" shade that looks like the obvious
choice, but it is meant to fill a panel rather than draw a line - as a
border it disappears into the background. Only the focused pane escapes
that, because its border is drawn in `accent`.

`accent` is the one worth choosing carefully. It marks the row under the
cursor, the focused pane's border, the active tab and the progress fill,
and it does all of that as a foreground colour with nothing behind it -
unlike `selected_bg`, which brings its own contrast. Pick something that
reads clearly against `background`, or the one row you are meant to be
looking at ends up harder to see than the rest.

## Switching themes at runtime

`:theme` opens a floating picker over the panes. Moving through it
applies each theme as you go - the list, sidebar and detail pane
underneath recolor live, so you judge a theme on your own torrents
rather than on a swatch. `enter` keeps it, `esc` puts back the one you
started with. Your own themes from the themes directory are listed
alongside the built-ins and marked `(user)`; one that fails to parse is
reported and stepped over rather than applied.

`:theme nord` switches straight to a named theme without opening the
picker.

## Editing a theme while torrnado runs

The TUI checks the active theme's file about once a second, and when the
file has changed it applies it straight away, without a restart. Save an
edit to `~/.config/torrnado/themes/<name>.toml` and the open TUI recolors.
If the file is a symlink, the change that counts is to the file it points
at. That lets a theme switcher keep one file, say `current.toml` with
`theme = "current"`, linked to a palette it regenerates on every switch,
and every running torrnado follows it.

It follows the theme in use, so after `:theme nord` it is nord's file
that counts, and a built-in theme has no file to follow. Nothing is
reloaded while the `:theme` picker is open. A file that fails to load is
reported once and the last good theme stays, until the file changes
again.

The choice lasts for the session. torrnado will not rewrite your
`config.toml` - doing so would re-encode the file and lose its comments
and ordering - so to keep a theme, put `theme = "nord"` in it yourself.
