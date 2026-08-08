# Running under systemd

`contrib/torrnado.service` runs the daemon as a system service under its
own unprivileged user, with `StateDirectory=torrnado` for the session
file and socket. Its header carries the install steps; `make
systemd-test` runs them and the unit against a real systemd in a
container (`Dockerfile.systemd`), including `systemctl restart` keeping
the torrent list, `systemctl reload` reopening the log, and a SIGKILLed
daemon being brought back.

Worth knowing on a systemd box: every client starts a daemon itself when
nothing answers the socket. Run `torrnado list` while the service is
stopped, or during the seconds it takes to restart, and you get a second
daemon that systemd does not manage - holding the lock the service is
about to want, which makes the service fail to start with
`another daemon already holds .../daemon.sock.lock` in the journal. Kill
that one (`pkill -u torrnado torrnado`) and start the service again.
