# Development

## Layout

```
cmd/torrnado        CLI entrypoint (cobra): daemon, add, remove, pause,
                       resume, recheck, priority, limit, move, list, and
                       the bare command (attach the TUI)
internal/engine        anacrolix/torrent client wrapper: Go API + an
                       event channel of state snapshots. No IPC or UI
                       code in here.
internal/ipc            gob-over-Unix-socket RPC: call/reply for commands,
                       server-pushed events for state -- what lets the
                       daemon run detached from any UI.
internal/tui            bubbletea model: a three-pane layout (status
                       sidebar, torrent list, docked Pieces/Peers/Files
                       detail pane) plus the command palette. Talks to the
                       engine only through an ipc.Client, so it doesn't
                       matter whether the daemon is a spawned subprocess
                       or one you started yourself.
internal/config          TOML config, XDG paths, validation.
internal/theme           built-in themes + TOML theme overrides.
internal/batch           expands add-command arguments (dirs, globs,
                       magnet-list files) into a flat source list.
internal/format          byte/rate/ratio/ETA formatting shared by the TUI
                       and `torrnado list`.
```

Dependency order is strict and one-directional: `format` and `engine` have
no internal dependencies; `ipc` depends only on `engine`; `config` depends
on `format`; `tui` depends on `engine`, `ipc`, `config`, `theme` and
`batch`; `cmd` depends on everything. Nothing in `engine` or `ipc` imports
`tui` or `cmd` -- if you find yourself wanting to, the abstraction has
leaked.

## Tasks

```sh
make            # list every target
make build      # build ./torrnado
make test       # go test ./...
make test-race  # the same with the race detector
make check      # gofmt -l, go vet, go test -- the gate before a commit
make e2e        # drive the built binary through the shell suites
```

`make check` is what every commit has to pass. It is deliberately the
same three things CI would run, so a green local run means a green build.

## Testing on Linux

The daemon is meant to run on Linux and is developed on macOS, so both
suites also run in a container:

```sh
make docker-test    # gofmt, vet, unit tests and both e2e suites, on linux
make systemd-test   # the unit file against a real systemd, in a container
```

`make systemd-test` boots systemd as pid 1 in a privileged container and
drives the service: enabled, started, running as an unprivileged user,
logs reaching the journal, a torrent surviving `systemctl restart`,
reload reopening the log, a `SIGKILL`ed daemon coming back, and a stopped
one staying stopped.

Both have caught real bugs that macOS hid -- a fixed listen port that made
parallel test packages collide, and a signal handler installed too late.

## The end-to-end suites

`e2e/` drives the real binary the way a user would rather than calling Go
functions, which catches what unit tests structurally cannot: a subcommand
not wired into the root command, a daemon that fails to detach, a socket
path that is wrong.

Each suite gives itself its own `HOME` and `XDG_DATA_HOME`, and finds its
daemon by asking which process holds *its* socket -- never by pattern
matching on the process name, which would kill the daemon you are
actually using.

## Docs

This site is MkDocs Material. To work on it:

```sh
make docs-serve   # http://127.0.0.1:8000, live reload
make docs-build   # a strict build, the same one CI runs
```

Requirements are pinned in `docs-requirements.txt` so a local build and
the CI build produce the same site.
