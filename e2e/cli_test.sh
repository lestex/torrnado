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
# above never will - nothing is there to serve it - and a torrent
# without metadata cannot report a size, a file list, or anything but
# "checking".
SAMPLE="$REPO_ROOT/e2e/testdata/sample.torrent"
SAMPLE_IH=d134b832ac06546d2b8c85a59b0c4011a6910cdf

echo "daemon lifecycle"

# Nothing is running yet, so this command has to start a daemon before it
# can do anything - the behavior that lets a user never think about the
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
# the user is standing in - not the daemon's.
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
# blocks forever waiting for a peer that does not exist - which is
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
echo "machine-readable output"

# Pretty-printed with one key to a line, so a grep reads it without jq.
out=$("$TORRNADO" list --json 2>&1)
assert_success "list --json succeeds" "$TORRNADO" list --json
if [ "$(echo "$out" | head -1)" = "[" ]; then
	pass "list --json is an array"
else
	fail "list --json is an array" "starts with: $(echo "$out" | head -1)"
fi
assert_contains "a torrent is there by id" "$out" "\"id\": \"$IH_B\""
assert_contains "...and by name" "$out" '"name": "Second"'
assert_contains "a size is a number of bytes" "$out" '"size": 15,'
assert_contains "what is not known is null, not zero" "$out" '"eta_seconds": null'

rows=$("$TORRNADO" list | tail -n +2 | grep -c .)
objs=$(echo "$out" | grep -c '^    "id":')
if [ "$rows" -eq "$objs" ]; then
	pass "the JSON and the table list the same torrents"
else
	fail "the JSON and the table list the same torrents" "table has $rows rows, JSON has $objs"
fi

# Paused is its own field: state cannot say it for a torrent being checked.
"$TORRNADO" pause "$SAMPLE_IH" >/dev/null 2>&1
out=$("$TORRNADO" list --json 2>&1)
assert_contains "a paused torrent says so" "$(echo "$out" | grep -A2 '"name": "hello.txt"')" '"paused": true'
"$TORRNADO" resume "$SAMPLE_IH" >/dev/null 2>&1

if command -v jq >/dev/null 2>&1; then
	if echo "$out" | jq -e 'type == "array" and all(.[]; has("id") and has("state") and has("ratio"))' >/dev/null 2>&1; then
		pass "the output is valid JSON with the contracted keys"
	else
		fail "the output is valid JSON with the contracted keys" "jq rejected it"
	fi
fi

out=$("$TORRNADO" status --json 2>&1)
assert_contains "status --json says a daemon is running" "$out" '"running": true'
assert_contains "...and answering" "$out" '"answering": true'
assert_contains "...and which process it is" "$out" "\"pid\": $(daemon_pid),"
assert_contains "...and where it is listening" "$out" "\"socket\": \"$SOCKET\""
assert_failure "--json with --watch is refused" "$TORRNADO" list --json --watch
assert_failure "--json with --quiet is refused" "$TORRNADO" status --json --quiet

# With nothing running: an empty data directory, so there is no daemon to
# find. Asking must not start one, and is not itself a failure - the
# answer is in the document.
out=$(XDG_DATA_HOME="$E2E_TMP/empty-data" "$TORRNADO" status --json 2>&1)
rc=$?
assert_contains "with no daemon, running is false" "$out" '"running": false'
assert_contains "...and nothing is answering" "$out" '"answering": false'
if [ "$rc" -eq 0 ]; then
	pass "...and the exit status is still 0"
else
	fail "...and the exit status is still 0" "got $rc"
fi
if [ ! -S "$E2E_TMP/empty-data/torrnado/daemon.sock" ]; then
	pass "...and asking did not start a daemon"
else
	fail "...and asking did not start a daemon" "a socket appeared"
fi

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
echo "naming a torrent"

# Two torrents whose ids share a front, so there is something to be
# ambiguous about. Their names share a word for the same reason.
TWIN_1=ab12000000000000000000000000000000000001
TWIN_2=ab12000000000000000000000000000000000002
"$TORRNADO" add "magnet:?xt=urn:btih:${TWIN_1}&dn=Twin+One" "magnet:?xt=urn:btih:${TWIN_2}&dn=Twin+Two" >/dev/null 2>&1

# The front of an id. d134b8 is the sample torrent's and nobody else's.
#
# State is asserted on that torrent because it has metadata: one without
# reads as "checking" whatever else is true of it, so a paused magnet
# could not show that a pause took.
assert_success "the start of an id names a torrent" "$TORRNADO" pause d134b8
out=$("$TORRNADO" list 2>&1)
assert_contains "the prefix paused the torrent it names" "$(echo "$out" | grep "hello.txt")" "paused"
assert_not_contains "...and only that one" "$(echo "$out" | grep "Third")" "paused"
"$TORRNADO" resume d134b8 >/dev/null 2>&1

# A name, whole or part, in any case.
assert_success "a whole name names a torrent" "$TORRNADO" pause "hello.txt"
out=$("$TORRNADO" list 2>&1)
assert_contains "pausing by name took effect" "$(echo "$out" | grep "hello.txt")" "paused"
assert_success "part of a name, in any case, names a torrent" "$TORRNADO" resume "HELLO"
out=$("$TORRNADO" list 2>&1)
assert_not_contains "resuming by name took effect" "$(echo "$out" | grep "hello.txt")" "paused"

# Hex is case-insensitive and ids are stored in lower case.
assert_success "a full id in capitals still works" "$TORRNADO" pause "$(echo "$IH_B" | tr 'a-f' 'A-F')"
"$TORRNADO" resume "$IH_B" >/dev/null 2>&1

# What it picked is said on stderr, so a script reading stdout never sees it.
err=$("$TORRNADO" pause bbbb 2>&1 >/dev/null)
assert_contains "stderr says which torrent a prefix picked" "$err" "Second"
out=$("$TORRNADO" pause bbbb 2>/dev/null)
if [ -z "$out" ]; then
	pass "stdout carries nothing extra"
else
	fail "stdout carries nothing extra" "got: $out"
fi
"$TORRNADO" resume bbbb >/dev/null 2>&1

# Two matches is a question, not a coin toss.
out=$("$TORRNADO" pause ab12 2>&1)
assert_failure "an ambiguous prefix is refused" "$TORRNADO" pause ab12
assert_contains "the refusal says it matched two" "$out" "2 torrents"
assert_contains "...and names the first" "$out" "Twin One"
assert_contains "...and the second" "$out" "Twin Two"
out=$("$TORRNADO" pause Twin 2>&1)
assert_failure "an ambiguous name is refused" "$TORRNADO" pause Twin
assert_contains "the refusal lists the candidates" "$out" "Twin Two"
assert_failure "a name matching nothing is refused" "$TORRNADO" pause "no such torrent"

# One bad argument does not spoil the rest: the good one is still acted on,
# and the exit status still says something failed.
assert_failure "a batch with one bad argument fails" "$TORRNADO" pause d134b8 "no such torrent"
out=$("$TORRNADO" list 2>&1)
assert_contains "...but the resolvable one was paused" "$(echo "$out" | grep "hello.txt")" "paused"
"$TORRNADO" resume d134b8 >/dev/null 2>&1

# The other commands that take a torrent.
assert_success "limit --torrent takes a prefix" "$TORRNADO" limit up 1M --torrent d134b8
assert_success "priority takes a prefix" "$TORRNADO" priority d134b8 0 normal
assert_success "seed-limit takes a name" "$TORRNADO" seed-limit --ratio 2 hello
assert_success "label takes a name" "$TORRNADO" label tv hello
out=$("$TORRNADO" list 2>&1)
assert_contains "the label landed on the named torrent" "$(echo "$out" | grep "hello.txt")" "tv"

# remove most of all: it is the one that cannot be undone.
assert_success "remove takes a name" "$TORRNADO" remove "Twin Two"
out=$("$TORRNADO" list 2>&1)
assert_not_contains "the named torrent is gone" "$out" "Twin Two"
assert_contains "its twin is untouched" "$out" "Twin One"
"$TORRNADO" remove "Twin One" >/dev/null 2>&1

# Completion offers full ids, described by name, and no file names.
comp=$("$TORRNADO" __complete pause "" 2>/dev/null)
assert_contains "completion offers an id" "$comp" "$IH_C"
assert_contains "...described by its name" "$comp" "Third"
assert_contains "...and asks the shell for no file names" "$comp" ":4"
comp=$("$TORRNADO" __complete pause "bbbb" 2>/dev/null)
assert_contains "completion filters by what is typed" "$comp" "$IH_B"
assert_not_contains "...leaving out what does not match" "$comp" "$IH_C"
comp=$("$TORRNADO" __complete pause "$IH_B" "" 2>/dev/null)
assert_not_contains "completion leaves out a torrent already given" "$comp" "$IH_B"
comp=$("$TORRNADO" __complete priority "$SAMPLE_IH" "" 2>/dev/null)
assert_not_contains "a position that is not a torrent offers no ids" "$comp" "$IH_C"

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

# `config` reads the file and nothing else, so it has to work with no
# daemon anywhere - that is most of its value when something is wrong.
UNUSED_SOCK="$E2E_TMP/unused.sock"
CFG2="$E2E_TMP/inspect.toml"
cat >"$CFG2" <<EOF
download_dir  = "$E2E_TMP/inspect-downloads"
daemon_socket = "$UNUSED_SOCK"
state_dir     = "$E2E_TMP/inspect-state"
theme         = "nord"
EOF

out=$("$TORRNADO" --config "$CFG2" config 2>&1)
assert_contains "config reports the file it read" "$out" "$CFG2"
assert_contains "config reports the download directory" "$out" "$E2E_TMP/inspect-downloads"
assert_contains "config reports the socket path" "$out" "$UNUSED_SOCK"
assert_contains "config reports a setting from the file" "$out" "nord"
# Derived from state_dir rather than configured, so it is the one a user
# cannot work out by reading their own config file.
assert_contains "config reports where the session is kept" "$out" "$E2E_TMP/inspect-state/session.json"

if [ -S "$UNUSED_SOCK" ]; then
	fail "config does not start a daemon" "a daemon appeared at $UNUSED_SOCK"
else
	pass "config does not start a daemon"
fi

out=$("$TORRNADO" --config "$E2E_TMP/absent.toml" config 2>&1)
assert_contains "config says when there is no file" "$out" "not found"

assert_failure "an unknown config key is rejected" \
	sh -c "echo 'downlaod_dir = \"/tmp/x\"' > '$E2E_TMP/bad.toml'; '$TORRNADO' --config '$E2E_TMP/bad.toml' list"

# Stop the second daemon before the run ends; teardown only knows about
# the first one's socket.
custom_pid=$(lsof -t "$CUSTOM_SOCK" 2>/dev/null | head -1)
[ -n "$custom_pid" ] && kill "$custom_pid" 2>/dev/null

echo
echo "a daemon of another build"

# The daemon outlives its clients, so a client routinely meets one that is
# not its own build. A second binary from the same source, stamped as an
# earlier release, stands in for the daemon someone left running before an
# upgrade. `make e2e` builds it; run the script by hand without it and this
# section says so rather than failing.
OLD="$REPO_ROOT/torrnado-e2e-old"
if [ -x "$OLD" ]; then
	# Short names, and no nesting: a macOS socket path tops out near 104
	# characters, and this temp directory already spends most of them. The
	# data directory is $SKEW itself, so the socket is $SKEW/torrnado/.
	SKEW="$E2E_TMP/sk"
	mkdir -p "$SKEW" "$E2E_TMP/skh"
	skew_env() { env HOME="$E2E_TMP/skh" XDG_DATA_HOME="$SKEW" "$@"; }
	# This daemon is not the one setup_env knows about, so it needs its own
	# cleanup - and it must run even if the script is interrupted.
	stop_skew() { skew_env "$TORRNADO" stop >/dev/null 2>&1; }
	trap 'stop_skew; teardown' EXIT

	# The old binary starts a daemon from itself.
	skew_env "$OLD" list >/dev/null 2>&1
	out=$(skew_env "$TORRNADO" status 2>&1)
	assert_contains "status names the daemon's build" "$out" "0.0.1-e2e-old"
	assert_contains "status warns that it is not this build" "$out" "Warning"
	assert_contains "...and says how to restart it" "$out" "torrnado stop"
	assert_success "an older daemon can still be stopped by a newer client" skew_env "$TORRNADO" stop
	stop_skew

	# The same build is not warned about: the daemon the rest of this script
	# has been using is this binary's own.
	out=$("$TORRNADO" status 2>&1)
	assert_not_contains "a daemon of this build is not warned about" "$out" "Warning"
else
	echo "  skip  no $OLD - 'make e2e' builds it"
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

# Pressing TAB must not be what starts a daemon. The one that was running
# has just been stopped, so this is the moment to ask.
comp=$("$TORRNADO" __complete pause "" 2>/dev/null)
if [ ! -S "$SOCKET" ] && [ -z "$(daemon_pid)" ]; then
	pass "completing with no daemon running does not start one"
else
	fail "completing with no daemon running does not start one" "something holds $SOCKET"
fi
assert_not_contains "...and offers no ids" "$comp" "$IH_C"

summary
