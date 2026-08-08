# A container image for running the daemon unattended.
#
#   docker build -t torrnado .
#   docker run -d --name torrnado \
#     -v torrnado-state:/var/lib/torrnado \
#     -v "$PWD/downloads:/downloads" \
#     -p 51413:51413 -p 51413:51413/udp \
#     torrnado
#   docker exec torrnado torrnado add <magnet>
#   docker logs -f torrnado
#
# The daemon is the whole program, so the container runs `torrnado
# daemon` and every other command is a `docker exec` into it - they talk
# over the Unix socket in the state volume, which never leaves the
# container.

# --- build -------------------------------------------------------------
FROM golang:1.25 AS build

WORKDIR /src

# Dependencies first, in their own layer: they change far less often than
# the source, so an edit to a .go file does not re-download the module
# cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 is a decision, not a default worth drifting on. It picks
# the pure-Go bbolt piece-completion database (.torrent.bolt.db) over the
# cgo SQLite one (.torrent.db), and the two do not read each other's
# files: a download directory populated by a cgo build re-verifies from
# scratch under this image, and vice versa. It also produces a static
# binary, which is what lets the runtime stage below be this small.
#
# Trimpath keeps build machine paths out of panic traces.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/torrnado ./cmd/torrnado

# --- test --------------------------------------------------------------
#
# `docker build --target test .` runs the whole suite on Linux, which is
# where this is meant to run and is not where it is developed. The e2e
# tests matter most here: they spawn a detached daemon and find it by
# socket, and both of those are the kind of thing that works on one
# platform and not another.
FROM build AS test

RUN apt-get update \
    && apt-get install -y --no-install-recommends lsof \
    && rm -rf /var/lib/apt/lists/*

# Through make rather than by hand, so this stage cannot drift from what
# the same targets do on a developer's machine - and so the list of e2e
# suites lives in exactly one place (the systemd one needs a booted
# system and is excluded there).
RUN make fmt-check vet test e2e

# --- runtime -----------------------------------------------------------
FROM alpine:3.21

# ca-certificates for https .torrent URLs (`torrnado add https://...`);
# tini so the daemon gets a real init as pid 1 - without one, SIGTERM
# from `docker stop` is delivered to a process that has no default
# handler for it, and the session is never saved on the way out.
RUN apk add --no-cache ca-certificates tini

# Runs as a normal user. Nothing here needs privilege, and the socket's
# file permissions are the only thing guarding the daemon.
RUN adduser -D -u 1000 torrnado

COPY --from=build /out/torrnado /usr/local/bin/torrnado

# XDG_DATA_HOME puts the socket, the session file and the saved metainfo
# under /var/lib/torrnado. The config goes at the path torrnado already
# looks for it under this user's home, so mounting your own file over it
# is all it takes to override anything here.
ENV XDG_DATA_HOME=/var/lib
RUN mkdir -p /var/lib/torrnado /downloads /home/torrnado/.config/torrnado \
    && printf 'download_dir = "/downloads"\nstate_dir = "/var/lib/torrnado"\n' \
       > /home/torrnado/.config/torrnado/config.toml \
    && chown -R torrnado:torrnado /var/lib/torrnado /downloads /home/torrnado

USER torrnado
VOLUME ["/downloads", "/var/lib/torrnado"]

# The BitTorrent listen port from the default config's range. Publish it
# (TCP and UDP) to be reachable by peers that cannot hole-punch.
EXPOSE 51413/tcp 51413/udp

# Logs go to stdout/stderr as timestamped text, which is what `docker
# logs` and every log driver expect. Set log.file only if you would
# rather have a file inside the state volume.
ENTRYPOINT ["/sbin/tini", "--", "torrnado"]
CMD ["daemon"]
