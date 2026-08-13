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

**Local Service Discovery does nothing.** `network.lsd` is accepted and
validated, but the library implements no LSD at all. The key exists so a
config written against the documented schema does not fail, and so the
gap is discoverable rather than silent.

**Rechecking is expensive.** Forcing a recheck is O(N²) in the piece
count against the completion database - hours on a large single-file
torrent - and can hit an assertion inside the library that kills the
daemon from a goroutine nothing here can recover from. Do not reach for
it as a diagnostic. Pausing the torrent calls off one you regret. See
[library limitations](limitations.md).

**A client and daemon of different versions can disagree.** The daemon
outliving its clients is the design, so a freshly built TUI may attach to
a daemon started days ago. The wire format has no version handshake: a
field the old daemon has never heard of arrives as a zero value, which
shows up as something quietly missing rather than an error. Restart the
daemon after upgrading.
