#!/bin/sh
# Run the Go tests against a throwaway Postgres in docker (port 55432).
# Extra args go to `go test`, e.g. scripts/test.sh -run TestPreferences ./internal/web
set -eu
cd "$(dirname "$0")/.."
NAME=pt-pgtest
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" -e POSTGRES_USER=t -e POSTGRES_PASSWORD=t -e POSTGRES_DB=t \
	-p 127.0.0.1:55432:5432 postgres:17-alpine >/dev/null
trap 'docker rm -f "$NAME" >/dev/null' EXIT
until docker exec "$NAME" pg_isready -U t -h 127.0.0.1 >/dev/null 2>&1; do sleep 0.5; done
[ $# -eq 0 ] && set -- ./...
PARATRACK_TEST_PG='postgres://t:t@127.0.0.1:55432/t?sslmode=disable' go test -p 1 "$@"
