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

## Checking VPN detection against a real device

The unit tests for `internal/vpn` classify synthetic interfaces and a fake
sysfs tree, which tests the rules but not the kernel's description of a
real tunnel. To exercise the whole path -- route lookup, source address,
sysfs -- make a tunnel and route the probe destination through it:

```sh
docker run --rm -it --cap-add=NET_ADMIN --device /dev/net/tun \
  -v "$PWD:/src" -w /src golang:1.25 bash
apt-get update && apt-get install -y iproute2

ip tuntap add mode tun dev tun0            # or: ip link add wg0 type wireguard
ip addr add 10.99.0.2/24 dev tun0
ip link set tun0 up
ip route add 192.0.2.0/24 dev tun0          # the address Detect probes
```

Then call `vpn.Detect(nil)` from a throwaway test in the package. Without
the route it reports the container's `eth0` and refuses; with it, `tun0`
(via `tun_flags`) or `wg0` (via `DEVTYPE=wireguard`) and allows.

On macOS there is nothing to fake -- connect a VPN and run the same
throwaway test, which should name the `utun` device carrying the traffic.
A Tailscale with no exit node should *not* satisfy it, since the default
route stays on `en0`.

## What CI runs

`.github/workflows/ci.yml`, on every push and pull request. Each job calls
a make target rather than its own `go` invocation, so a green run there and
a green `make check` here mean the same thing:

| job | what |
|---|---|
| `check` | `make check` and `make e2e`, on Linux **and** macOS |
| `race` | `make test-race` |
| `vuln` | `govulncheck ./...` -- only vulnerabilities the code reaches |
| `build` | `goreleaser build --snapshot`, the release build on every push |
| `docker` | builds the image and runs `torrnado version` inside it |
| `coverage` | a profile, with the totals in the run summary |

`.github/workflows/integration.yml` runs `make systemd-test` nightly and on
demand -- it needs a privileged container and takes minutes, which is too
slow for every push and too valuable to run only when someone remembers.

## Cutting a release

The tag is the version; nothing is committed anywhere to forget to bump.

```sh
make check && make e2e
make changelog TAG=v0.1.0       # files the pending commits under that version
git commit -am "chore: changelog for v0.1.0"

# main is protected: the changelog commit lands through a pull request.
git checkout -b release-v0.1.0 && git push -u origin release-v0.1.0
gh pr create --base main --fill && gh pr merge --merge

# Tag the commit that actually landed, not the local one you wrote --
# otherwise the archives and `torrnado version` name a commit that is not
# on the branch. Tags are not protected and push directly.
git checkout main && git pull
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The tag triggers `.github/workflows/release.yml`: it reruns `make check`
against that exact commit -- a tag can be pushed at a commit CI never saw --
then builds four archives with GoReleaser and creates the release, with
notes generated from the same `cliff.toml` that wrote `CHANGELOG.md`.

To rehearse any of it locally:

```sh
goreleaser check                       # validate .goreleaser.yaml
goreleaser build --snapshot --clean    # build all four targets into dist/
git cliff --latest --strip header      # exactly what the release page gets
```

`make changelog` uses `git-cliff` from `PATH` and falls back to its
container image, so it works on a machine that has never installed it.

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
