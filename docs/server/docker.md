# Running in Docker

```sh
docker build -t torrnado .
docker run -d --name torrnado \
  -v torrnado-state:/var/lib/torrnado \
  -v "$PWD/downloads:/downloads" \
  -p 51413:51413 -p 51413:51413/udp \
  torrnado

docker exec torrnado torrnado add <magnet>
docker exec torrnado torrnado list
docker logs -f torrnado
```

The container runs the daemon; every other command is a `docker exec`
into it, talking over the socket in the state volume. Both volumes have
to persist: `/downloads` holds the data, `/var/lib/torrnado` holds the
session file that tells the next container which torrents to resume.
`docker restart` puts them all back.

The image is built `CGO_ENABLED=0` -- deliberately, and it is the trap
described above: a download directory filled by a cgo build (the default
for `go build` on your machine) uses a SQLite completion database this
image cannot read, and everything re-verifies. Keep a data directory with
one or the other, not both.

Config lives at `/home/torrnado/.config/torrnado/config.toml` in the
image; mount your own over it to change anything. `docker build --target
test .` runs the unit and e2e suites inside the image instead of
building it.
