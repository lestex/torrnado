# Non-goals and caveats

Things torrnado deliberately does not do, and things it does imperfectly
for reasons worth knowing before you hit them.

## Deliberately not done

**No remote control protocol.** The daemon listens on a Unix socket and
nothing else. There is no TCP listener, no authentication and no TLS,
because adding a remote protocol means adding all three and getting them
right. SSH already solves the problem: forward the socket, or run the CLI
on the box.

**No web UI.** The interface is a terminal and a shell.

**No Windows.** The daemon detaches with `SysProcAttr.Setsid`, which is
POSIX-only; on other platforms it still runs in the background, just not
in its own session. The Unix-socket IPC would need a different transport
(named pipes) to work there at all, which is out of scope.

**No watch directory, health endpoint or metrics.** A `.torrent` landing
in a folder can be handled by `torrnado add` in a cron job or an inotify
one-liner, which is less machinery than a watcher inside the daemon.

**No config rewriting.** `:theme` changes the theme for the session and
tells you what to add to `config.toml`; it will not edit that file for
you. Re-encoding it from the parsed struct would lose the comments and
ordering of something you wrote by hand.

**No kill switch.** `vpn.required` holds transfers while the system is
off-VPN, but tracker announces and DHT traffic keep going - both are
client-wide settings the library only reads when the client is built, so
changing them means tearing the client down and re-adding every torrent
every time the VPN moves. Blocking traffic outright is the firewall's job,
and it does it better than a torrent client can. See
[Configuration](../guide/configuration.md#requiring-a-vpn).

## Rough edges

**Per-torrent rate limits are approximate.** The library throttles the
whole client and offers no per-torrent hook, so the daemon toggles a
torrent's data transfer off and on around the cap each second. It
averages out near the limit but is bursty. Global limits are exact.

**A moved torrent re-verifies.** Moving re-adds the torrent against new
storage, so it hashes what it just moved rather than trusting it. That is
a read of the data already on disk, not a re-download, and the file
priorities set before the move are put back afterwards.

**The completion hook runs once, and is not waited for.** `on_complete`
runs a command the first time a torrent finishes, given the torrent's
folder in place of `%f` - or appended, when the command names no
placeholder. The name, id, size and path also arrive as `TORRNADO_NAME`,
`TORRNADO_ID`, `TORRNADO_SIZE` and `TORRNADO_PATH`, so a notification
script need not parse them back out of a path.

It is split on spaces and run directly, never through a shell, so a
folder containing a space still arrives as one argument. Put anything
needing pipes or globbing in a script and point at that.

The command is detached: a hook that runs for an hour does not hold up
the daemon, and it outlives it. Nothing waits for it, so nothing reports
its exit status - a failure shows up in whatever the hook itself writes.

It fires once per torrent for the life of that torrent, not once per
daemon start. A torrent that was already finished when the daemon started
does not re-run it, which is what stops a restart re-notifying or
re-unpacking everything ever downloaded.

**Seeding limits stop a torrent, they do not hold it.** `[seed_limit]`
pauses a torrent once it has finished downloading and then reaches a
ratio or a seeding time, whichever comes first. That is deliberately a
pause rather than the kind of hold the guards below use: a guard is a
condition of the machine that comes and goes, while a seeding limit is a
decision that this torrent is done, so it survives a restart instead of
being undone by one. Resuming the torrent starts it seeding again.

The ratio is over the torrent's life, not the current run - the totals
are kept in the session file, because the library counts per instance and
a limit that reset on every restart would never come due on the machine
it is for. The seeding clock runs from completion, not from when the
torrent was added.

`torrnado seed-limit --ratio none <id>` seeds one torrent without a
limit however the config is set, which is the only way to opt a single
torrent out of a default.

**A full disk stops transfers rather than filling it.** `min_free_space`
holds every transfer while the download directory's filesystem is below
the floor you set, and lets them go again by itself once space comes
back. It is off by default. Like the VPN guard it is a condition of the
machine rather than something you asked for, so it never touches a
torrent's own paused flag - the torrents show as `low disk` and resume on
their own. It fails open: a filesystem it cannot read is not treated as
full.

**There is no Local Service Discovery.** Peers on your own LAN are found
the same way as any others - trackers, DHT, PEX - and not by multicast
(BEP 14), which the torrent library does not implement. `network.lsd` was
accepted as a no-op until v0.5.4 and has been **removed**, rather than
left in place pretending to be a setting. A config file still carrying it
will not load; see below.

**Rechecking is expensive.** Forcing a recheck is O(N²) in the piece
count against the completion database - hours on a large single-file
torrent - and can hit an assertion inside the library that kills the
daemon from a goroutine nothing here can recover from. Do not reach for
it as a diagnostic. Pausing the torrent calls off one you regret.

**A client and daemon of different versions can disagree.** The daemon
outliving its clients is the design, so a freshly built TUI may attach to
a daemon started days ago. The wire format has no version handshake: a
field the old daemon has never heard of arrives as a zero value, which
shows up as something quietly missing rather than an error. Restart the
daemon after upgrading.
