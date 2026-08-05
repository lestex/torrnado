# Configuration

TOML at `$XDG_CONFIG_HOME/torrnado/config.toml` (`~/.config/torrnado/config.toml`
if `$XDG_CONFIG_HOME` is unset -- honored on every platform this runs on,
not just Linux). Every key is optional; a missing file is not an error,
an invalid one is -- validation fails with the specific bad key rather
than silently ignoring it.

```toml
download_dir  = "~/Downloads/torrnado"                     # default download directory
daemon_socket = "~/.local/share/torrnado/daemon.sock"       # IPC socket path
state_dir     = "~/.local/share/torrnado"                    # session file + saved metainfo
                                                              # (a second daemon needs its own)
theme         = "dracula"                                     # see Themes below
player        = "mpv"                                          # used by preview; may carry flags

[rate_limit]
upload   = "unlimited"   # or "500k", "2M", "1.5G", a bare byte count, "0"
download = "unlimited"

[port]
low  = 51413   # 0/0 = let the OS pick a random port
high = 51433    # a range is tried in order until one binds

[network]
dht        = true
pex        = true
lsd        = true    # accepted, but has no effect -- see Limitations
encryption = true
seed       = true     # keep uploading after a torrent completes

[log]
level         = "info"    # debug, info, warn, error
library_level = "warn"     # the torrent library's own messages, filtered separately
file          = ""          # empty = stderr, which is what a service manager wants

[keybinds]

## Seeing what is in effect

`torrnado config` prints the file it would read -- saying so when there
is none -- every path derived from it, and the settings actually in
effect, defaults and overrides together:

```
Paths
  config          ~/.config/torrnado/config.toml  (not found -- built-in defaults in use)
  themes          ~/.config/torrnado/themes
  download_dir    ~/Downloads/torrnado
  state_dir       ~/.local/share/torrnado
  daemon_socket   ~/.local/share/torrnado/daemon.sock
  session         ~/.local/share/torrnado/session.json
  saved metainfo  ~/.local/share/torrnado/torrents

Settings
  theme                dracula
  player               mpv
  rate_limit.upload    unlimited
  rate_limit.download  2.0MiB/s
  port                 51413-51433
  ...
```

It is the one command that never contacts the daemon, which is deliberate:
its value is highest when something is wrong and nothing is running. The
flip side is that it prints what a daemon started *now* would use -- one
already running may have been started with something else.
