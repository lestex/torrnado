# anacrolix/torrent limitations hit while building this

The engine wrapper (`internal/engine`) exists specifically to absorb
these; they're documented here (and next to the relevant code) rather
than left as a surprise:

- **No per-torrent rate limiting.** `ClientConfig.UploadRateLimiter` /
  `DownloadRateLimiter` are client-wide only; there's no hook to throttle
  a single `Torrent`'s network I/O. Global limits (`:limit-up`,
  `:limit-down`, `torrnado limit`) are exact, enforced by the library
  itself via `golang.org/x/time/rate`. Per-torrent limits
  (`--torrent <id>`) are a **best-effort approximation**: each engine
  tick (~1s) compares a torrent's measured throughput against its
  configured cap and toggles `Allow/DisallowDataDownload/Upload`
  accordingly. This averages out close to the limit over ~1s windows but
  is bursty, not a smooth token bucket.
- **No native pause/resume.** Implemented via
  `Allow/DisallowDataDownload` and `Allow/DisallowDataUpload`, which stop
  a torrent from requesting or serving piece data while leaving peer
  connections intact.
- **No live "move storage" API.** The default file storage backend has
  no in-place relocate. `MoveStorage` pauses the torrent, moves files on
  disk itself, re-adds the torrent pointed at a new
  `storage.NewFile(newDir)`, and re-verifies data in the background (a
  hash check against files already on disk, not a re-download). This
  also means a move resets any custom per-file priorities back to normal
  - the re-added `Torrent` is a fresh instance with no priority history.
  Rebuilding the spec has one trap of its own: `Torrent.Metainfo()`
  returns an allocated-but-empty piece-layers map for a v1 torrent, and a
  non-nil map is what the library takes as "this is v2" - the re-add
  then fails with `no piece root set for file` for every file spanning
  more than one piece. `MoveStorage` clears it. A move that fails after
  the files have already been moved cannot be undone, so the error is
  recorded on the torrent rather than the list going on showing figures
  from a handle that is no longer running.
- **`(*torrent.File).Priority()` doesn't reflect
  `(*torrent.Torrent).DownloadAll()`/`DownloadPieces()`.** Those set
  piece priorities directly; `File.Priority()` only ever returns what was
  last passed to `File.SetPriority()`, a separate bookkeeping field. A
  torrent started with `DownloadAll()` downloads correctly but would
  report every file's priority as "none" forever. The engine avoids
  `DownloadAll()` entirely and marks new torrents' files wanted via
  `File.SetPriority()` file-by-file instead, so the files list is
  accurate.
- **No level between "not wanted" and "wanted normally".** The piece
  priority scale is `None < Normal < High < Readahead < Next < Now`, so a
  "low priority, but still wanted" file (as most torrent clients offer)
  has no faithful equivalent; `priority ... low` degrades to `normal`.
- **`Files()` and `NumPieces()` require metadata to already be
  available**, and aren't merely slow or empty before then - they
  dereference a nil pointer and crash the whole process (every torrent in
  the daemon, not just the one being touched) if called first. A magnet
  add can spend an arbitrary (even unbounded, for a dead swarm) amount of
  time in this state, so every engine call that touches a torrent's files
  checks `Info() != nil` first rather than assuming metadata has arrived.
- **No Local Service Discovery (LSD/BEP 14).** There's no config field or
  hook for local-network peer discovery anywhere in the library. The
  `network.lsd` config key and `:lsd`-adjacent toggles are accepted (so a
  config file written against this spec doesn't fail validation) but do
  nothing.
- **No live per-tracker status.** No last-announce time, last error, or
  tracker-reported seeder/leecher counts are exposed - the tracker list
  is the static URL/tier list from the torrent's metainfo, not a live
  announce log.
- **Peer choke state isn't exported.** `Peer.choking` / `Peer.peerChoking`
  are unexported with no accessor; the only public path to them is
  `Client.WriteStatus`, which dumps the entire client's status as free
  text. The peers table therefore has no Choked column - it shows how
  each peer was discovered instead, rather than scraping a debug dump or
  guessing.
- **No instantaneous per-peer transfer rate.** `PeerStats.DownloadRate` is
  a lifetime average (useful bytes / total time spent expecting data), so
  it barely moves once a connection has been up a while. The peers table's
  ↓/↑ figures are computed the same way the per-torrent speeds are: byte
  counter deltas between engine ticks (~1s).
- **Piece completion lags byte progress, by a lot.** A torrent's progress
  percentage comes from `bytesLeft()`, which counts *received* bytes
  (including unverified "dirty" chunks), while `PieceState.Complete` only
  goes true once a piece has been hash-checked and marked in storage.
  There is no API to observe the verification backlog, and it drains
  slowly: a torrent showing 100% routinely reports under half its pieces
  as verified for minutes afterwards. The Pieces tab therefore says
  "verified", not "complete" - the same number under a "complete" label
  would read as data loss. Separately, `PieceState.Complete` is gated on
  `storage.Completion.Ok`, so a piece whose storage state has never been
  consulted is *unknown* rather than missing; `engine.PieceRun` carries
  that as its own flag and the map dims it rather than drawing it as a
  hole.
- **Force-recheck is quadratic on large torrents.** Verifying a piece ends
  in `filePieceImpl.MarkComplete`, which calls `allFilePiecesComplete` -
  and that scans *every* piece of the file through the SQLite piece
  completion database. A full recheck marks every piece, so an N-piece
  single-file torrent costs O(N²) completion lookups: a 5.9 GB ISO (24208
  pieces of 256 KiB) needs ~586 million of them and will sit in
  `checking` at ~170% CPU for hours. The hashing itself finishes quickly
  - the Pieces tab shows all pieces verified long before
  `VerifyDataContext` returns. There is no fix in v1.61.0 (the newest
  release) and no way to cancel a running recheck; restarting the daemon
  is the only way out. **Avoid `r` / `torrnado recheck` on multi-GB
  torrents.**
- **Force-recheck can panic the whole daemon.** `checkPendingPiecesMatches‑
  RequestOrder` is an internal consistency assertion reached from
  `pieceHashed` → `setPieceCompletion` → `openNewConns` → `needData`. It
  panics with "piece request order has {} and pending pieces has {...}"
  when a recheck marks pieces pending that aren't in the request order.
  The panic happens on a library-owned goroutine, so no `recover()` in
  this codebase can catch it - the daemon dies and takes every torrent
  with it.
- **No port-range binding.** `ClientConfig.ListenPort` is a single fixed
  port (0 = random); `[port] low`/`high` in config is implemented by
  retrying `torrent.NewClient` across the range until one port binds.
