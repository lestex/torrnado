#!/usr/bin/env bash
#
# Tests contrib/torrnado.service against a real systemd.
#
# Run inside the container built from Dockerfile.systemd (`make
# systemd-test`), never on your own machine -- it starts, stops, kills and
# restarts the torrnado service, and would do all of that to yours.
#
# What is being tested is the unit file, not the daemon: that the service
# comes up under an unprivileged user through the sandbox, that its logs
# reach the journal, that the session survives `systemctl restart`, that a
# crash brings it back and a clean stop does not.

set -uo pipefail

# This script stops, kills and restarts the torrnado service, so running
# it on a machine that has one would do all of that to yours. Only ever
# runs inside the throwaway container built from Dockerfile.systemd.
if [ ! -f /.dockerenv ] && [ ! -f /run/.containerenv ]; then
	echo "systemd_test.sh only runs inside the Dockerfile.systemd container -- use 'make systemd-test'" >&2
	exit 1
fi

TESTS_RUN=0
TESTS_FAILED=0

pass() {
	TESTS_RUN=$((TESTS_RUN + 1))
	printf '  ok    %s\n' "$1"
}

fail() {
	TESTS_RUN=$((TESTS_RUN + 1))
	TESTS_FAILED=$((TESTS_FAILED + 1))
	printf '  FAIL  %s\n' "$1"
	shift
	for line in "$@"; do
		printf '        %s\n' "$line"
	done
}

assert_contains() { # <description> <haystack> <needle>
	case "$2" in
	*"$3"*) pass "$1" ;;
	*) fail "$1" "wanted to find: $3" "in output: $2" ;;
	esac
}

# torrnado runs the CLI as the service user, so it finds the same socket
# the daemon opened. Root would look under a different XDG_DATA_HOME and
# start a second daemon of its own.
torrnado() {
	runuser -u torrnado -- env XDG_DATA_HOME=/var/lib XDG_CONFIG_HOME=/etc \
		/usr/local/bin/torrnado "$@"
}

# wait_active waits for systemd to report the service running, rather
# than sleeping a guessed amount.
wait_active() {
	for _ in $(seq 1 50); do
		[ "$(systemctl is-active torrnado)" = "active" ] && return 0
		sleep 0.2
	done
	return 1
}

# wait_ready waits for the daemon inside the service to be listening,
# which is not the same as systemd calling the unit active: systemd says
# active the moment it has forked, and the daemon takes a moment longer
# to open its socket.
#
# Asked of the journal rather than by running a client, because a client
# that finds nothing listening starts a daemon of its own -- during a
# restart that daemon takes the lock the service is about to want, and
# the service then fails to start. Probing must not change what it is
# probing.
wait_ready() {
	local pid
	for _ in $(seq 1 150); do
		pid=$(main_pid)
		if [ -n "$pid" ] &&
			journalctl -u torrnado --no-pager _PID="$pid" 2>/dev/null |
			grep -q "daemon ready"; then
			return 0
		fi
		sleep 0.2
	done
	return 1
}

# main_pid prints the service's main pid, or nothing when it has none.
# Never let this return 0: `kill -9 0` signals the caller's whole process
# group, which here means killing this script and the shell running it.
main_pid() {
	local pid
	pid=$(systemctl show -p MainPID --value torrnado)
	[ -n "$pid" ] && [ "$pid" != "0" ] && echo "$pid"
}

IH=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

echo "boot"

# The container's pid 1 is systemd itself; if it has not finished
# starting, nothing below means anything.
for _ in $(seq 1 100); do
	systemctl is-system-running --quiet --wait >/dev/null 2>&1 && break
	sleep 0.2
done

if systemctl is-enabled --quiet torrnado; then
	pass "the unit is enabled"
else
	fail "the unit is enabled" "systemctl is-enabled says $(systemctl is-enabled torrnado 2>&1)"
fi

if wait_active; then
	pass "the service started on boot"
else
	fail "the service started on boot" "$(systemctl status torrnado --no-pager 2>&1 | tail -20)"
fi

# The unit runs it as an unprivileged user; a service that only works as
# root would still look fine in `systemctl status`.
wait_ready
owner=$(ps -o user= -p "$(main_pid)" | tr -d ' ')
if [ "$owner" = "torrnado" ]; then
	pass "the daemon runs as the torrnado user"
else
	fail "the daemon runs as the torrnado user" "running as: $owner"
fi

echo
echo "journal"

log=$(journalctl -u torrnado --no-pager 2>&1)
assert_contains "the daemon's start line reached the journal" "$log" "daemon starting"
assert_contains "the daemon reported itself ready" "$log" "daemon ready"
# Its own timestamps and levels, not just the journal's -- the format has
# to be readable when the logs are read any other way.
assert_contains "log lines carry a level" "$log" "level=INFO"

# StateDirectory= plus XDG_DATA_HOME= is what puts these here; if either
# were wrong the daemon would still start, somewhere useless.
if [ -S /var/lib/torrnado/daemon.sock ]; then
	pass "the socket is in the state directory"
else
	fail "the socket is in the state directory" "$(ls -la /var/lib/torrnado 2>&1)"
fi

echo
echo "using it"

out=$(torrnado add "magnet:?xt=urn:btih:${IH}&dn=Systemd+Test" 2>&1)
assert_contains "a client can reach the service's daemon" "$out" "added:"

# ProtectSystem=strict makes everything read-only except ReadWritePaths,
# so a torrent that cannot be saved is the sandbox being wrong.
if [ -f /var/lib/torrnado/session.json ]; then
	pass "the session file is written through the sandbox"
else
	fail "the session file is written through the sandbox" "$(ls -la /var/lib/torrnado 2>&1)"
fi

echo
echo "restart"

systemctl restart torrnado
if wait_active && wait_ready; then
	pass "systemctl restart brings it back"
else
	fail "systemctl restart brings it back" "$(systemctl status torrnado --no-pager 2>&1 | tail -20)"
fi

for _ in $(seq 1 50); do
	torrnado list 2>/dev/null | grep -q "$IH" && break
	sleep 0.2
done
assert_contains "the torrent survived the restart" "$(torrnado list 2>&1)" "$IH"
assert_contains "the restore was logged" "$(journalctl -u torrnado --no-pager 2>&1)" "session restored"

# KillSignal=SIGTERM, and the daemon saves its session on the way out. A
# unit that killed it any other way would lose the last changes.
assert_contains "the stop was a clean SIGTERM" \
	"$(journalctl -u torrnado --no-pager 2>&1)" 'msg="daemon shutting down" signal=terminated'

echo
echo "reload"

# After the service is actually up, not merely forked. A signal that
# lands before the daemon has installed its handlers gets the default
# action, which for SIGHUP is death.
wait_ready
systemctl reload torrnado
sleep 1
if [ "$(systemctl is-active torrnado)" = "active" ]; then
	pass "systemctl reload does not stop the service"
else
	fail "systemctl reload does not stop the service" "$(systemctl status torrnado --no-pager 2>&1 | tail -20)"
fi
assert_contains "reload reopened the log" \
	"$(journalctl -u torrnado --no-pager 2>&1)" "log file reopened"

echo
echo "crashing"

before=$(main_pid)
if [ -z "$before" ]; then
	fail "a killed daemon is restarted" "the service has no main pid to kill"
else
	kill -9 "$before"

	# Restart=on-failure with RestartSec=5, so this is a real wait.
	after=""
	for _ in $(seq 1 100); do
		after=$(main_pid)
		[ -n "$after" ] && [ "$after" != "$before" ] && break
		sleep 0.2
	done

	if wait_active && [ -n "$after" ] && [ "$after" != "$before" ]; then
		pass "a killed daemon is restarted"
	else
		fail "a killed daemon is restarted" "pid before $before, after $after" \
			"$(systemctl status torrnado --no-pager 2>&1 | tail -20)"
	fi

	wait_ready
	assert_contains "the torrent came back after the crash too" "$(torrnado list 2>&1)" "$IH"
fi

echo
echo "stopping"

systemctl stop torrnado
if [ "$(systemctl is-active torrnado)" = "inactive" ]; then
	pass "systemctl stop stops it"
else
	fail "systemctl stop stops it" "is-active says $(systemctl is-active torrnado)"
fi

# Restart=on-failure, not always: a service stopped on purpose has to
# stay stopped.
sleep 6
if [ "$(systemctl is-active torrnado)" = "inactive" ]; then
	pass "it stays stopped, rather than being restarted"
else
	fail "it stays stopped, rather than being restarted" "is-active says $(systemctl is-active torrnado)"
fi

echo
if [ "$TESTS_FAILED" -gt 0 ]; then
	printf '%d of %d checks failed\n' "$TESTS_FAILED" "$TESTS_RUN"
	exit 1
fi
printf 'all %d checks passed\n' "$TESTS_RUN"
