# Running in Docker

Every release publishes an image to GitHub Container Registry, for
`linux/amd64` and `linux/arm64`:

```sh
docker pull ghcr.io/lestex/torrnado          # or :0.5.6, or :0.5
```

`latest` follows the newest release, `0.5.6` pins one exactly, and `0.5`
tracks that minor series. Pin a version anywhere the download directory
matters - see the completion-database note below.

```sh
docker run -d --name torrnado \
  -v torrnado-state:/var/lib/torrnado \
  -v "$PWD/downloads:/downloads" \
  -p 51413:51413 -p 51413:51413/udp \
  ghcr.io/lestex/torrnado

docker exec torrnado torrnado add <magnet>
docker exec torrnado torrnado list
docker logs -f torrnado
```

The container runs the daemon; every other command is a `docker exec`
into it, talking over the socket in the state volume. Both volumes have
to persist: `/downloads` holds the data, `/var/lib/torrnado` holds the
session file that tells the next container which torrents to resume.
`docker restart` puts them all back.

The image is built `CGO_ENABLED=0` - deliberately, and it is the trap
described above: a download directory filled by a cgo build (the default
for `go build` on your machine) uses a SQLite completion database this
image cannot read, and everything re-verifies. Keep a data directory with
one or the other, not both.

Config lives at `/home/torrnado/.config/torrnado/config.toml` in the
image; mount your own over it to change anything. `docker build --target
test .` runs the unit and e2e suites inside the image instead of
building it.

## Building it yourself

```sh
docker build -t torrnado .
```

That still works and is what CI checks on every push. An image built this
way reports `dev` from `torrnado version`, because the version is stamped
in from the tag: pass `--build-arg VERSION=... --build-arg COMMIT=...` if
you want it to say something. The published images are built the same
way, cross-compiled per architecture rather than emulated, which is what
`CGO_ENABLED=0` buys.
