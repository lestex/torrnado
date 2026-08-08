# Themes

Built in: `dracula`, `nord`, `gruvbox`, `solarized-dark`, `solarized-light`,
`catppuccin`, `tokyo-night`, and `plain` (a 16-color-safe fallback with no
truecolor hex codes, for terminals with no real color support).

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

The choice lasts for the session. torrnado will not rewrite your
`config.toml` - doing so would re-encode the file and lose its comments
and ordering - so to keep a theme, put `theme = "nord"` in it yourself.
