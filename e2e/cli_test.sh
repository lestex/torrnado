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
