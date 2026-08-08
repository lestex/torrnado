# Configuration

TOML at `$XDG_CONFIG_HOME/torrnado/config.toml` (`~/.config/torrnado/config.toml`
if `$XDG_CONFIG_HOME` is unset - honored on every platform this runs on,
not just Linux). Every key is optional; a missing file is not an error,
an invalid one is - validation fails with the specific bad key rather
than silently ignoring it.

```toml
download_dir  = "~/Downloads/torrnado"                     # default download directory
daemon_socket = "~/.local/share/torrnado/daemon.sock"       # IPC socket path
state_dir     = "~/.local/share/torrnado"                    # session file + saved metainfo
                                                              # (a second daemon needs its own)
theme         = "dracula"                                     # see Themes below
player        = "mpv"                                          # used by preview; may carry flags
opener        = "open"                                          # xdg-open on Linux; used by `o` / `torrnado open`

[rate_limit]
upload   = "unlimited"   # or "500k", "2M", "1.5G", a bare byte count, "0"
download = "unlimited"

[port]
low  = 51413   # 0/0 = let the OS pick a random port
high = 51433    # a range is tried in order until one binds

[network]
dht        = true
pex        = true
lsd        = true    # accepted, but has no effect - see Limitations
encryption = true
seed       = true     # keep uploading after a torrent completes

[vpn]
required   = false   # hold every transfer while the system is not on a VPN
interfaces = []       # extra interfaces to count as a VPN - see below

[log]
level         = "info"    # debug, info, warn, error
library_level = "warn"     # the torrent library's own messages, filtered separately
file          = ""          # empty = stderr, which is what a service manager wants

[keybinds]
```

## Seeing what is in effect

`torrnado config` prints the file it would read - saying so when there
is none - every path derived from it, and the settings actually in
effect, defaults and overrides together:

```
Paths
  config          ~/.config/torrnado/config.toml  (not found - built-in defaults in use)
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
flip side is that it prints what a daemon started *now* would use - one
already running may have been started with something else.

## Opening a torrent's folder

`o` in the TUI, or `torrnado open <id>`, shows the directory holding a
torrent's files - its own folder for a multi-file torrent, the save path
for a single-file one. The program it runs is yours to choose:

```toml
opener = "open"                                 # macOS: Finder (the default)
opener = "xdg-open"                              # Linux: your file manager (the default)
opener = "alacritty --working-directory %f"       # a terminal in the folder instead
opener = "nautilus"                                # or name one directly
```

`%f` is replaced by the directory. A command that does not mention it gets
the directory appended, which is why the bare `open` and `xdg-open` above
work. The same is true of `player`, so `mpv --title %f` places the stream
URL rather than appending it.

The command is split on spaces and run directly - there is no shell, so
nothing in a path is interpreted, and the substitution happens after the
split, which keeps a directory with a space in its name a single argument.

The program is detached: quitting the TUI does not close the window it
opened.

## Requiring a VPN

```toml
[vpn]
required = true
```

With this set, no torrent moves piece data - in either direction - while
the system is not on a VPN. Nothing is paused: the daemon holds the
transfers, keeps everything exactly as you left it, and lets them go again
by themselves when the VPN comes back. A held torrent reads `blocked` in
the list, and the sidebar says `vpn: blocked` where it otherwise names the
interface you are on.

It is off by default, and while it is off the check is never run.

### What it stops, and what it does not

It stops piece data. It does **not** stop tracker announces or DHT
traffic: those are client-wide and can only be turned off when the torrent
client is built, so a blocked daemon still announces the torrents it is
holding, from your real address. Peers will also still connect to it -
they just get nothing.

So this is "nothing transfers off-VPN", not a network kill switch. If what
you need is that nothing at all leaves the machine outside the tunnel,
that belongs at the firewall, or in a VPN client that has a kill switch of
its own.

### How it decides

It asks the kernel which interface a packet to the internet would leave
by, then what kind of device that is - a WireGuard, tun/tap, ppp or
IPsec device counts; Ethernet and Wi-Fi do not. There is nothing to
configure per VPN client: WireGuard, OpenVPN, IKEv2, IPsec and every
macOS NetworkExtension client are all detected the same way, including
ones this was never tested against.

Two consequences worth knowing:

- **Having a tunnel up is not enough** - it has to be the one carrying
  your traffic. A Mac usually has several `utun` devices up at all times,
  and a Tailscale with no exit node is one of them; none of those carry
  general traffic, so none of them satisfy the guard.
- **A split tunnel that does not carry the default route reads as no
  VPN**, because for anything not in its routes, it is.

Anything that cannot be answered - no route at all, an interface the
kernel will not describe - counts as *not* on a VPN. A guard that fails
open on a lookup it could not finish is not a guard.

### When detection cannot see it

A policy-based IPsec tunnel moves traffic over the physical interface with
no tunnel device of its own, and nothing above can tell that apart from
having no VPN. Name the interface instead:

```toml
[vpn]
required   = true
interfaces = ["utun4"]
```

Anything listed there counts as a VPN whatever the kernel says about it.
Exact names, not prefixes. Note that macOS renumbers `utun` devices
between connections, so a pinned name can go stale - prefer letting
detection do its job unless it cannot.
