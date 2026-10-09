#!/usr/bin/env bash
# QA stand lifecycle: prepare, run, tear down.
#
# A browser-driven check needs three things that are easy to get wrong by
# hand: a throwaway Postgres, a server built from the current tree, and an
# account to sign in as. This script owns all three so a run is one command
# and nothing is left behind.
#
#   scripts/qa-env.sh up       bring the stand up, print BASE_URL and creds
#   scripts/qa-env.sh check    run the Playwright checks against it
#   scripts/qa-env.sh cycle    up, check, then tear down whatever was created
#   scripts/qa-env.sh down     tear down (no-op if nothing is running)
#   scripts/qa-env.sh status   what is currently up
#
# Everything it creates is named from QA_NAME, so it can never collide with
# the production container or the preview database. Override with:
#   QA_NAME, QA_PORT, QA_DB_PORT
set -euo pipefail
cd "$(dirname "$0")/.."

QA_NAME=${QA_NAME:-paratrack-qa}
QA_PORT=${QA_PORT:-8890}
QA_DB_PORT=${QA_DB_PORT:-15440}
STATE=/tmp/$QA_NAME.env

pg_container="$QA_NAME-pg"
db_url="postgres://t:t@127.0.0.1:$QA_DB_PORT/qa?sslmode=disable"
base_url="http://127.0.0.1:$QA_PORT"

log()  { printf '\033[1m%s\033[0m\n' "$*" >&2; }
fail() { printf '\033[31m%s\033[0m\n' "$*" >&2; exit 1; }

# Wait for a TCP port to answer, or give up. Sleeps are the usual reason a
# stand gets half-started: the first request lands before the listener exists.
wait_for_port() {
	local port=$1 tries=${2:-100}
	for _ in $(seq "$tries"); do
		if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
			exec 3<&- 3>&-
			return 0
		fi
		sleep 0.2
	done
	return 1
}

cmd_up() {
	cmd_down >/dev/null 2>&1 || true

	# From here on a failure must not leave a container or a server behind.
	# The trap is cleared at the end: `up` is meant to survive this script
	# exiting. `cycle` installs its own afterwards — and if `cmd_up` also
	# left a trap behind it would silently replace that one.
	trap 'cmd_down' ERR INT TERM

	command -v docker >/dev/null || fail "docker is required"

	docker rm -f "$pg_container" >/dev/null 2>&1 || true
	docker run -d --name "$pg_container" \
		-e POSTGRES_USER=t -e POSTGRES_PASSWORD=t -e POSTGRES_DB=qa \
		-p "127.0.0.1:$QA_DB_PORT:5432" postgres:17-alpine >/dev/null

	wait_for_port "$QA_DB_PORT" 150 || fail "postgres did not accept connections on $QA_DB_PORT"

	log "building server from the working tree"
	go build -o "/tmp/$QA_NAME-server" ./cmd/paratrack

	log "starting stand on $QA_PORT"
	PARATRACK_ENV=development PARATRACK_DATABASE_URL="$db_url" \
		"/tmp/$QA_NAME-server" web --addr "127.0.0.1:$QA_PORT" \
		>"/tmp/$QA_NAME.log" 2>&1 &
	server_pid=$!

	wait_for_port "$QA_PORT" 100 || { tail -20 "/tmp/$QA_NAME.log" >&2; fail "stand did not start on $QA_PORT"; }

	cat >"$STATE" <<EOF
QA_NAME=$QA_NAME
QA_PORT=$QA_PORT
QA_DB_PORT=$QA_DB_PORT
DATABASE_URL=$db_url
BASE_URL=$base_url
SERVER_PID=$server_pid
PG_CONTAINER=$pg_container
EOF

	log "stand up"
	printf '  BASE_URL=%s\n  logs=/tmp/%s.log\n' "$base_url" "$QA_NAME" >&2
	printf '  the suite registers its own account per run\n' >&2

	trap - ERR INT TERM
}

cmd_check() {
	[ -f "$STATE" ] || fail "nothing is up — run: scripts/qa-env.sh up"
	# shellcheck disable=SC1090
	. "$STATE"
	[ -d .venv ] || fail "no .venv — run: scripts/setup_e2e.sh"

	PARATRACK_BASE="$BASE_URL" .venv/bin/python e2e/test_dashboard.py
	PARATRACK_BASE="$BASE_URL" .venv/bin/python e2e/qa_full.py
}

cmd_down() {
	if [ -f "$STATE" ]; then
		# shellcheck disable=SC1090
		. "$STATE"
		[ -n "${SERVER_PID:-}" ] && kill "$SERVER_PID" 2>/dev/null || true
		rm -f "$STATE"
	fi
	docker rm -f "$QA_NAME-pg" >/dev/null 2>&1 || true
	rm -f "/tmp/$QA_NAME-server" "/tmp/$QA_NAME.log"
}

cmd_status() {
	if [ -f "$STATE" ]; then
		# shellcheck disable=SC1090
		. "$STATE"
		printf 'up   %s (pid %s, db :%s)\n' "$BASE_URL" "$SERVER_PID" "$QA_DB_PORT"
	else
		printf 'down\n'
	fi
}

case ${1:-} in
	up)    cmd_up ;;
	check) cmd_check ;;
	down)  cmd_down ;;
	status) cmd_status ;;
	cycle)
		# The whole point of a lifecycle: whatever happens above — pass, fail,
		# Ctrl-C — the stand and its database go away with it.
		trap 'cmd_down' EXIT INT TERM
		cmd_up
		cmd_check
		;;
	*) sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; exit 1 ;;
esac