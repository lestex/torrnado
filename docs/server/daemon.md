# The daemon

There is one long-running process, the daemon, and everything else is a
thin client that connects to it over a Unix domain socket
(`$XDG_DATA_HOME/torrnado/daemon.sock` by default, usually
`~/.local/share/torrnado/daemon.sock`).

- **`torrnado`** (no arguments) attaches the TUI to the daemon. If
  nothing is listening on the socket, it spawns `torrnado daemon` as a
  detached background process first (logging to
  `$XDG_DATA_HOME/torrnado/daemon.log`), waits for it to come up, then
  attaches. Quitting the TUI (`q`) does **not** stop the daemon - your
  torrents keep running.
- **`torrnado daemon`** runs the engine in the foreground until
  interrupted (Ctrl-C) or sent SIGTERM. This is what gets spawned in the
  background automatically, but you can also run it yourself directly if
  you'd rather manage it with systemd/launchd/tmux than let torrnado
  manage it.
- **`torrnado add/remove/pause/resume/recheck/priority/limit/move/list`**
  are scriptable passthroughs to a running daemon (spawning one if needed,
  same as the bare command). Everything you can do in the TUI's command
  palette, you can also do from a shell script.

To actually stop the daemon, send it SIGTERM or SIGINT. There is no
`torrnado stop` subcommand, since a daemon that's still seeding is a
normal, intentional state to leave it in.

Find the pid by asking which process holds the lock file beside the
socket:

```sh
lsof -t ~/.local/share/torrnado/daemon.sock.lock       # the daemon's pid
kill "$(lsof -t ~/.local/share/torrnado/daemon.sock.lock)"
```

That file carries an exclusive lock for as long as the daemon runs - it
is what stops a second one claiming the same socket - so whoever holds it
*is* the daemon that owns this state directory, whatever the binary
happens to be called. `pkill -f "torrnado daemon"` matches on the command
line instead, which quietly finds nothing when the running copy was built
or installed under another name, and can just as easily match a daemon
belonging to a different state directory. The pid is also on the
`daemon starting` line in the log, and the same trick is how the
end-to-end suites find their own daemon without going near anybody
else's.

## Running it unattended

The daemon is the whole program; the TUI and the CLI are just clients. So
a headless box that downloads things is `torrnado daemon` running under
whatever supervises processes there (systemd, launchd, tmux - it makes
no difference to torrnado), driven over SSH.

**It comes back after a restart.** Every change is written to
`<state_dir>/session.json` - the torrent list, paused state, save paths,
per-torrent rate limits, per-file priorities, when each was added - next
to a copy of each torrent's metainfo in `<state_dir>/torrents/`. On start
the daemon re-adds them all, and logs how many came back. Data already on
disk is not re-downloaded or re-verified: the piece-completion database
was always persistent, and this is what tells the daemon which torrents
to look it up for.

A session file that cannot be read is logged and skipped, one bad record
at a time. A server that refuses to boot over a malformed record is worse
than one that comes back with fewer torrents, so it starts either way -
`journalctl` is where you find out which happened.

**It can require a VPN.** `vpn.required = true` holds every transfer while
the machine is not on one, and lets them go again by itself when it comes
back - which is the case a box left running for weeks actually hits, when
the VPN client reconnects at 4am and nobody is watching. It is off by
default. Read what it does and does not cover in
[Configuration](../guide/configuration.md#requiring-a-vpn) before relying
on it.

**Logs** go to stderr as text with timestamps and levels, which is what a
service manager wants: journald timestamps and stores it with no further
configuration.

```
time=2026-08-05T13:26:28.488-04:00 level=INFO msg="daemon starting" pid=83154 ...
time=2026-08-05T13:26:28.491-04:00 level=INFO msg="session restored" torrents=2
time=2026-08-05T13:26:28.491-04:00 level=INFO msg="daemon ready" socket=... stream=127.0.0.1:64853
```

Set `log.file` to write to a file instead. `SIGHUP` reopens it, which is
what logrotate needs after renaming the old one away - without that the
daemon goes on writing to a file that no longer has a name, and the disk
never gets the space back.

The torrent library's own output is captured into the same stream at
`log.library_level`, tagged `src=torrent`. It is much noisier than
torrnado is - it reports every tracker that misbehaves - which is why
it has its own level rather than sharing `log.level`. Panics still go
straight to stderr through the Go runtime, whatever `log.file` says, so
leave stderr attached to something.

**Two things to know before deploying:**

- **`CGO_ENABLED=0` changes the piece-completion database.** With cgo it
  is SQLite (`.torrent.db` in the download directory); without it, bbolt
  (`.torrent.bolt.db`). Neither reads the other's file, so a data
  directory populated by one build re-verifies from scratch under the
  other. Pin the choice deliberately when you build for a server, and
  keep it pinned.
- **Previews are loopback-only.** The stream server binds `127.0.0.1` on
  an OS-assigned port with a per-session token, so `torrnado preview`
  URLs from a remote daemon are only reachable through an SSH tunnel
  (`ssh -L 8080:127.0.0.1:<port> server`). That is the intended design,
  not an oversight: there is no authentication anywhere in torrnado
  beyond the filesystem permissions on the socket, so nothing it serves
  should be reachable from a network.

Running a second daemon alongside the first means giving it its own
`daemon_socket` **and** its own `state_dir`. Only the socket is guarded
against sharing (by a lock file); two daemons pointed at one state
directory will each restore the other's torrents and overwrite the
other's session file.

There is no remote control protocol. The socket is local by
construction, and SSH already solves the problem properly.


Deployment recipes have their own pages: [systemd](systemd.md) and
[Docker](docker.md).
