#!/usr/bin/env bash
#
# End-to-end tests for the daemon lifecycle and the add/list commands.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
setup_env

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

out=$("$TORRNADO" add 'magnet:?xt=urn:btih:aaaa&dn=First+Torrent' 2>&1)
assert_contains "add reports the new torrent id" "$out" "added:"

# The id is the hex infohash: 40 characters, and stable for a given
# source, so adding the same magnet twice must not create a second entry.
id=$(echo "$out" | awk '/added:/ {print $2}')
if [ ${#id} -eq 40 ]; then
	pass "id is a 40-character infohash"
else
	fail "id is a 40-character infohash" "got ${#id} characters: $id"
fi

"$TORRNADO" add 'magnet:?xt=urn:btih:aaaa&dn=First+Torrent' >/dev/null 2>&1
count=$("$TORRNADO" list 2>/dev/null | grep -c "$id")
if [ "$count" -eq 1 ]; then
	pass "adding the same magnet twice does not duplicate it"
else
	fail "adding the same magnet twice does not duplicate it" "found $count rows for $id"
fi

assert_failure "adding a non-magnet, non-file source fails" \
	"$TORRNADO" add "not-a-torrent-at-all"

assert_success "several sources can be added at once" \
	"$TORRNADO" add 'magnet:?xt=urn:btih:bbbb&dn=Second' 'magnet:?xt=urn:btih:cccc&dn=Third'

echo
echo "listing"

out=$("$TORRNADO" list 2>&1)
assert_contains "list shows the display name from the magnet" "$out" "First Torrent"
assert_contains "list shows a second torrent" "$out" "Second"
assert_contains "list shows a third torrent" "$out" "Third"
assert_contains "list shows the state" "$out" "downloading"

echo
echo "progress"

# The engine ticks once a second, so a couple of seconds apart the same
# torrent must report more done than before. This is what proves the
# daemon is doing work between commands rather than only when asked.
before=$("$TORRNADO" list 2>/dev/null | awk -v id="$id" '$1 == id {print $(NF-2)}')
sleep 2
after=$("$TORRNADO" list 2>/dev/null | awk -v id="$id" '$1 == id {print $(NF-2)}')

if [ "$before" != "$after" ]; then
	pass "progress advances between calls ($before -> $after)"
else
	fail "progress advances between calls" "still $after after 2 seconds"
fi

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
