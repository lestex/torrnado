#!/usr/bin/env bash
#
# End-to-end tests for what survives a daemon restart.
#
# This is the one behaviour a server depends on and no unit test can
# prove: kill the process, start it again, and find the same torrents in
# the same state. The engine's own tests restart an Engine within one
# process; here the process really does go away.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
setup_env

SAMPLE="$REPO_ROOT/e2e/testdata/sample.torrent"
SAMPLE_IH=d134b832ac06546d2b8c85a59b0c4011a6910cdf
IH_A=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

STATE_DIR="$XDG_DATA_HOME/torrnado"
SESSION="$STATE_DIR/session.json"

# restart_daemon stops the running daemon and starts a fresh one, the way
# a reboot or a `systemctl restart` would.
restart_daemon() {
	stop_daemon
	# The spawn-on-demand path starts the next one, so nothing here needs
	# to know how a daemon is launched.
	"$TORRNADO" list >/dev/null 2>&1
	wait_for_daemon
}

echo "session file"

"$TORRNADO" add "$SAMPLE" >/dev/null 2>&1
"$TORRNADO" add "magnet:?xt=urn:btih:${IH_A}&dn=Magnet+Only" >/dev/null 2>&1
wait_for_daemon

if [ -f "$SESSION" ]; then
	pass "adding a torrent writes the session file"
else
	fail "adding a torrent writes the session file" "no file at $SESSION"
fi

session=$(cat "$SESSION" 2>/dev/null)
assert_contains "session records the torrent from a file" "$session" "$SAMPLE_IH"
assert_contains "session records the torrent from a magnet" "$session" "$IH_A"

# A magnet whose metadata never arrives has no metainfo to be re-added
# from -- the URI is the only way back.
assert_contains "session keeps the magnet uri" "$session" "magnet:?xt=urn:btih:${IH_A}"

if [ -f "$STATE_DIR/torrents/$SAMPLE_IH.torrent" ]; then
	pass "metainfo is saved for a torrent that has it"
else
	fail "metainfo is saved for a torrent that has it" \
		"no file at $STATE_DIR/torrents/$SAMPLE_IH.torrent"
fi

echo
echo "restarting"

"$TORRNADO" pause "$SAMPLE_IH" >/dev/null 2>&1
"$TORRNADO" limit down 50K --torrent "$SAMPLE_IH" >/dev/null 2>&1

if restart_daemon; then
	pass "a new daemon starts after the old one is killed"
else
	fail "a new daemon starts after the old one is killed" "nothing holds $SOCKET"
fi

out=$("$TORRNADO" list 2>&1)
assert_contains "the torrent added from a file came back" "$out" "$SAMPLE_IH"
assert_contains "the torrent added from a magnet came back" "$out" "$IH_A"
assert_contains "a paused torrent came back paused" "$out" "paused"

log=$(cat "$DAEMON_LOG" 2>/dev/null)
assert_contains "the daemon logged the restore" "$log" "session restored"

# Its own timestamps, not the service manager's -- these have to be
# readable when the daemon is not running under one.
assert_contains "log lines carry a timestamp" "$log" "time="
assert_contains "log lines carry a level" "$log" "level=INFO"

# The limit is not shown by list, so read it back from the file the
# restarted daemon rewrote: if it had been lost, this rewrite is what
# would have dropped it.
assert_contains "a per-torrent limit survived the restart" "$(cat "$SESSION")" '"download_limit": 51200'

echo
echo "removing"

"$TORRNADO" remove "$IH_A" >/dev/null 2>&1
assert_not_contains "a removed torrent leaves the session" "$(cat "$SESSION")" "$IH_A"

restart_daemon
assert_not_contains "a removed torrent does not come back" "$("$TORRNADO" list 2>&1)" "$IH_A"

echo
echo "a broken session file"

stop_daemon
echo '{ this is not json' >"$SESSION"

if "$TORRNADO" list >/dev/null 2>&1 && wait_for_daemon; then
	pass "the daemon starts with an unreadable session"
else
	fail "the daemon starts with an unreadable session" "no daemon after a corrupt session file"
fi

assert_contains "the failure is logged, not silent" "$(cat "$DAEMON_LOG")" "restoring the session failed"

# And it must still be usable, not merely running.
assert_success "the daemon still accepts a torrent" \
	"$TORRNADO" add "magnet:?xt=urn:btih:${IH_A}&dn=After+Corruption"

summary
