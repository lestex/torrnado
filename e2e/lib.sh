#!/usr/bin/env bash
#
# Shared helpers for the end-to-end tests.
#
# These drive the real binary the way a user would - start a daemon, run
# commands, read what they print - rather than calling Go functions. That
# catches things unit tests structurally cannot: a subcommand not wired
# into the root command, a daemon that fails to detach, a socket path that
# is wrong.

set -uo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TORRNADO="$REPO_ROOT/torrnado"

TESTS_RUN=0
TESTS_FAILED=0

# setup_env gives this test its own HOME and data directory.
#
# This matters more than it looks: without it the tests would find the
# daemon you are actually using, list your torrents, and kill it on the
# way out. With it, the test's client can only ever reach the test's
# daemon.
setup_env() {
	if [ ! -x "$TORRNADO" ]; then
		echo "no binary at $TORRNADO - run 'make build' first" >&2
		exit 1
	fi

	# Check up front rather than discovering it through a failing
	# assertion. lsof identifies the test's daemon, and its absence
	# otherwise looks exactly like a daemon that would not start - the
	# error is swallowed and the pid simply comes back empty.
	if ! command -v lsof >/dev/null 2>&1; then
		echo "lsof is required by these tests but was not found on PATH" >&2
		exit 1
	fi

	# pwd -P resolves symlinks and collapses the doubled slash $TMPDIR's
	# trailing / would otherwise leave in the path. That matters because
	# stop_daemon looks the daemon up by socket path, and lsof compares
	# those as strings - a path that differs cosmetically finds nothing,
	# and the daemon survives the test run.
	E2E_TMP=$(cd "$(mktemp -d "${TMPDIR:-/tmp}/torrnado-e2e.XXXXXX")" && pwd -P)
	export HOME="$E2E_TMP/home"
	export XDG_DATA_HOME="$E2E_TMP/data"
	mkdir -p "$HOME" "$XDG_DATA_HOME"

	SOCKET="$XDG_DATA_HOME/torrnado/daemon.sock"
	DAEMON_LOG="$XDG_DATA_HOME/torrnado/daemon.log"

	# Runs on any exit, including a failed assertion or Ctrl-C, so a test
	# daemon is never left behind.
	trap teardown EXIT
}

teardown() {
	stop_daemon
	if [ -n "${E2E_TMP:-}" ]; then
		rm -rf "$E2E_TMP"
	fi
}

# stop_daemon kills the daemon holding *this test's* socket.
#
# Deliberately not `pkill -f 'torrnado daemon'`: the daemon runs from the
# same binary path as a real one, so a pattern match would kill the user's
# too. Asking which process holds this socket cannot make that mistake.
stop_daemon() {
	[ -S "${SOCKET:-}" ] || return 0

	local pid
	pid=$(lsof -t "$SOCKET" 2>/dev/null | head -1)
	[ -n "$pid" ] || return 0

	kill "$pid" 2>/dev/null
	for _ in 1 2 3 4 5 6 7 8 9 10; do
		kill -0 "$pid" 2>/dev/null || return 0
		sleep 0.2
	done
	kill -9 "$pid" 2>/dev/null
}

# daemon_pid prints the pid holding this test's socket, or nothing.
#
# It retries for a couple of seconds because lsof reports a snapshot of
# the kernel's open files, and a socket bound moments ago is sometimes not
# in it yet. Asking once made this look like a daemon that had failed to
# start - intermittently, which is worse than failing outright.
daemon_pid() {
	[ -S "${SOCKET:-}" ] || return 0

	local pid
	for _ in $(seq 1 20); do
		pid=$(lsof -t "$SOCKET" 2>/dev/null | head -1)
		if [ -n "$pid" ]; then
			echo "$pid"
			return 0
		fi
		sleep 0.1
	done
}

# wait_for_daemon waits for a daemon that is actually holding the socket,
# not merely for the socket file to appear.
wait_for_daemon() {
	for _ in $(seq 1 50); do
		if [ -S "${SOCKET:-}" ] && [ -n "$(daemon_pid)" ]; then
			return 0
		fi
		sleep 0.1
	done
	return 1
}

# --- assertions --------------------------------------------------------
#
# Each prints one line and counts itself, so a run reads as a checklist
# and the exit status still tells a script whether it all passed.

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

assert_not_contains() { # <description> <haystack> <needle>
	case "$2" in
	*"$3"*) fail "$1" "did not want to find: $3" "in output: $2" ;;
	*) pass "$1" ;;
	esac
}

assert_success() { # <description> <command...>
	local desc=$1
	shift
	if output=$("$@" 2>&1); then
		pass "$desc"
	else
		fail "$desc" "command failed: $*" "output: $output"
	fi
}

assert_failure() { # <description> <command...>
	local desc=$1
	shift
	if output=$("$@" 2>&1); then
		fail "$desc" "command unexpectedly succeeded: $*" "output: $output"
	else
		pass "$desc"
	fi
}

summary() {
	echo
	if [ "$TESTS_FAILED" -gt 0 ]; then
		printf '%d of %d checks failed\n' "$TESTS_FAILED" "$TESTS_RUN"
		exit 1
	fi
	printf 'all %d checks passed\n' "$TESTS_RUN"
}
