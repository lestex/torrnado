#!/usr/bin/env bash
#
# End-to-end tests for the daemon lifecycle and the add/list commands.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
setup_env

# A real client validates the infohash, so these have to be genuine
# 40-character hex strings. They point at nothing, which is fine: adding
# and listing a torrent does not require a single peer.
IH_A=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
IH_B=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
IH_C=cccccccccccccccccccccccccccccccccccccccc

# A real torrent file, so at least one torrent has metadata. The magnets
# above never will -- nothing is there to serve it -- and a torrent
# without metadata cannot report a size, a file list, or anything but
# "checking".
SAMPLE="$REPO_ROOT/e2e/testdata/sample.torrent"
SAMPLE_IH=d134b832ac06546d2b8c85a59b0c4011a6910cdf

echo "daemon lifecycle"

# Nothing is running yet, so this command has to start a daemon before it
# can do anything -- the behaviour that lets a user never think about the
# daemon at all.
out=$("$TORRNADO" list 2>&1)
assert_contains "list starts a daemon when none is running" "$out" "NAME"

if wait_for_daemon; then
	pass "daemon is listening on its socket"
else
	fail "daemon is listening on its socket" "nothing holds $SOCKET"
fi

# The command that started the daemon has already exited, so anything
# still holding the socket proves the daemon detached from it.
if [ -n "$(daemon_pid)" ]; then
	pass "daemon outlived the command that spawned it"
else
	fail "daemon outlived the command that spawned it" "nothing holds $SOCKET"
fi

assert_contains "daemon logged where it is listening" "$(cat "$DAEMON_LOG")" "socket"

echo
echo "adding torrents"

out=$("$TORRNADO" add "magnet:?xt=urn:btih:${IH_A}&dn=First+Torrent" 2>&1)
assert_contains "add reports the new torrent id" "$out" "added:"

# The id is the infohash out of the magnet, so it is knowable up front.
id=$(echo "$out" | awk '/added:/ {print $2}')
if [ "$id" = "$IH_A" ]; then
	pass "id is the magnet's infohash"
else
	fail "id is the magnet's infohash" "got $id, want $IH_A"
fi

"$TORRNADO" add "magnet:?xt=urn:btih:${IH_A}&dn=First+Torrent" >/dev/null 2>&1
count=$("$TORRNADO" list 2>/dev/null | grep -c "$id")
if [ "$count" -eq 1 ]; then
	pass "adding the same magnet twice does not duplicate it"
else
	fail "adding the same magnet twice does not duplicate it" "found $count rows for $id"
fi

assert_failure "adding a non-magnet, non-file source fails" \
	"$TORRNADO" add "not-a-torrent-at-all"

assert_success "several sources can be added at once" \
	"$TORRNADO" add "magnet:?xt=urn:btih:${IH_B}&dn=Second" "magnet:?xt=urn:btih:${IH_C}&dn=Third"

echo
echo "listing"

out=$("$TORRNADO" list 2>&1)
assert_contains "list shows the display name from the magnet" "$out" "First Torrent"
assert_contains "list shows a second torrent" "$out" "Second"
assert_contains "list shows a third torrent" "$out" "Third"
assert_contains "list prints a header row" "$out" "PROGRESS"

echo
echo "metadata"

# A magnet carries no file list until a peer supplies it, and these have
# no peers, so they stay in that state rather than reporting a size.
out=$("$TORRNADO" list 2>&1)
assert_contains "a magnet without metadata reports checking" "$out" "checking"
assert_not_contains "no torrent claims to be seeding" "$out" "seeding"

echo
echo "torrent files"

out=$("$TORRNADO" add "$SAMPLE" 2>&1)
assert_contains "a .torrent file can be added" "$out" "$SAMPLE_IH"

out=$("$TORRNADO" list 2>&1)
assert_contains "its name comes from the metadata" "$out" "hello.txt"
assert_not_contains "with metadata it is no longer checking" "$out" "$SAMPLE_IH  hello.txt  checking"

echo
echo "batch add"

# A directory means every .torrent inside it, and a text file means one
# magnet per line. Both are resolved by the client, against the directory
# the user is standing in -- not the daemon's.
BATCH_DIR="$E2E_TMP/batch"
mkdir -p "$BATCH_DIR"
cp "$SAMPLE" "$BATCH_DIR/one.torrent"

cat >"$E2E_TMP/magnets.txt" <<EOF
# torrents to fetch later
magnet:?xt=urn:btih:dddddddddddddddddddddddddddddddddddddddd&dn=FromList

magnet:?xt=urn:btih:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee&dn=AlsoFromList
EOF

assert_success "a directory of torrents can be added" "$TORRNADO" add "$BATCH_DIR"

out=$("$TORRNADO" add "$E2E_TMP/magnets.txt" 2>&1)
assert_contains "a magnet list file is expanded" "$out" "dddddddddddddddddddddddddddddddddddddddd"
assert_contains "every line of the list is added" "$out" "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

out=$("$TORRNADO" list 2>&1)
assert_contains "torrents from the list are tracked" "$out" "FromList"

assert_success "a glob is expanded" "$TORRNADO" add "$BATCH_DIR/*.torrent"
assert_failure "a glob matching nothing fails" "$TORRNADO" add "$E2E_TMP/nothing-here/*.torrent"
assert_failure "an empty directory fails" "$TORRNADO" add "$E2E_TMP/custom-downloads"

echo
echo "streaming preview"

# Put the data where the torrent expects it, so the client verifies it as
# complete and the stream has something to serve. Without this a read
# blocks forever waiting for a peer that does not exist -- which is
# correct, but not testable.
# The torrent was added earlier, when the file was not there, so it is
# recorded as empty. A recheck is what makes the client look again.
mkdir -p "$HOME/Downloads/torrnado"
printf 'hello torrnado\n' > "$HOME/Downloads/torrnado/hello.txt"
"$TORRNADO" recheck "$SAMPLE_IH" >/dev/null 2>&1

for _ in $(seq 1 50); do
	case "$("$TORRNADO" list 2>/dev/null | grep "$SAMPLE_IH")" in
	*100.0%*) break ;;
	esac
	sleep 0.2
done

URL=$("$TORRNADO" preview "$SAMPLE_IH" 0 2>&1)
assert_contains "preview returns a loopback URL" "$URL" "http://127.0.0.1:"

body=$(curl -s --max-time 10 "$URL")
assert_contains "the stream serves the file's contents" "$body" "hello torrnado"

# Range support is what lets a player seek; ServeContent gives it for
# free, but only if the reader really is seekable.
headers=$(curl -s --max-time 10 -o /dev/null -D - -r 0-4 "$URL")
assert_contains "a range request is answered with 206" "$headers" "206 Partial Content"
assert_contains "the range covers what was asked for" "$headers" "bytes 0-4/15"

# The token is the only thing standing between a local process and the
# user's torrents.
bad=$(echo "$URL" | sed 's|/stream/[0-9a-f]*/|/stream/deadbeef/|')
code=$(curl -s --max-time 10 -o /dev/null -w '%{http_code}' "$bad")
if [ "$code" = "404" ]; then
	pass "a wrong token is refused"
else
	fail "a wrong token is refused" "got HTTP $code, want 404"
fi

assert_failure "previewing an unknown torrent fails" "$TORRNADO" preview "not-a-real-id" 0

echo
echo "pause and resume"

# Pause is not just a flag: the daemon tells the torrent to stop asking
# for and serving data. The state reported back is the evidence it took.
assert_success "pause succeeds" "$TORRNADO" pause "$SAMPLE_IH"
out=$("$TORRNADO" list 2>&1)
assert_contains "a paused torrent reports paused" "$out" "paused"

assert_success "resume succeeds" "$TORRNADO" resume "$SAMPLE_IH"
out=$("$TORRNADO" list 2>&1)
assert_not_contains "resuming clears the paused state" "$out" "paused"

# Absolute rather than a toggle: pausing twice leaves it paused.
"$TORRNADO" pause "$SAMPLE_IH" >/dev/null 2>&1
"$TORRNADO" pause "$SAMPLE_IH" >/dev/null 2>&1
out=$("$TORRNADO" list 2>&1)
assert_contains "pause is absolute, not a toggle" "$out" "paused"
"$TORRNADO" resume "$SAMPLE_IH" >/dev/null 2>&1

assert_failure "pausing an unknown id fails" "$TORRNADO" pause "not-a-real-id"

echo
echo "rate limits"

assert_success "a global limit is accepted" "$TORRNADO" limit down 500k
assert_success "a limit can be cleared" "$TORRNADO" limit down unlimited
assert_success "a per-torrent limit is accepted" "$TORRNADO" limit up 1M --torrent "$SAMPLE_IH"
assert_failure "a bad rate is rejected" "$TORRNADO" limit down "fast"
assert_failure "a bad direction is rejected" "$TORRNADO" limit sideways 1M

echo
echo "removing"

assert_success "remove succeeds" "$TORRNADO" remove "$id"
out=$("$TORRNADO" list 2>&1)
assert_not_contains "a removed torrent is gone from the list" "$out" "$id"
assert_contains "the other torrents are untouched" "$out" "Second"
assert_failure "removing an unknown id fails" "$TORRNADO" remove "not-a-real-id"

echo
echo "configuration"

# A config file names the socket, so a daemon started with one is only
# reachable by a client using the same file. That is the whole test: the
# flag has to reach the spawned daemon, not just the client.
CFG="$E2E_TMP/custom.toml"
CUSTOM_SOCK="$E2E_TMP/custom.sock"
# state_dir has to be separate too: the session file is what a daemon
# restores its torrent list from, so two daemons sharing one would each
# come back holding the other's torrents.
cat >"$CFG" <<EOF
download_dir  = "$E2E_TMP/custom-downloads"
daemon_socket = "$CUSTOM_SOCK"
state_dir     = "$E2E_TMP/custom-state"
EOF

assert_success "a config file is accepted" "$TORRNADO" --config "$CFG" list

if [ -S "$CUSTOM_SOCK" ]; then
	pass "the daemon bound the socket named in the config"
else
	fail "the daemon bound the socket named in the config" "no socket at $CUSTOM_SOCK"
fi

# The two daemons are separate, so the torrents added earlier are not
# visible through the other config.
out=$("$TORRNADO" --config "$CFG" list 2>&1)
assert_not_contains "a separate config sees its own daemon only" "$out" "$SAMPLE_IH"

assert_failure "an unknown config key is rejected" \
	sh -c "echo 'downlaod_dir = \"/tmp/x\"' > '$E2E_TMP/bad.toml'; '$TORRNADO' --config '$E2E_TMP/bad.toml' list"

# Stop the second daemon before the run ends; teardown only knows about
# the first one's socket.
custom_pid=$(lsof -t "$CUSTOM_SOCK" 2>/dev/null | head -1)
[ -n "$custom_pid" ] && kill "$custom_pid" 2>/dev/null

echo
echo "shutdown"

pid=$(daemon_pid)
kill "$pid" 2>/dev/null
for _ in $(seq 1 25); do
	kill -0 "$pid" 2>/dev/null || break
	sleep 0.2
done

if kill -0 "$pid" 2>/dev/null; then
	fail "daemon stops on SIGTERM" "pid $pid still running"
else
	pass "daemon stops on SIGTERM"
fi

if [ -S "$SOCKET" ]; then
	fail "daemon removes its socket on the way out" "$SOCKET still exists"
else
	pass "daemon removes its socket on the way out"
fi

summary
