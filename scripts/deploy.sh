#!/bin/sh
# Deploy the working tree to paratrack.duckdns.org (this VPS).
# Tests, build, back the DB up, swap the container, health-check.
# Rollback: the previous image stays tagged paratrack:prev-<stamp>.
set -eu
cd "$(dirname "$0")/.."

DATA=/home/ubuntu/paratrack-data
BACKUPS=/home/ubuntu/paratrack-backups
URL=https://paratrack.duckdns.org/login
STAMP=$(date -u +%Y%m%d-%H%M%S)
RUN="--restart unless-stopped --network bitrix-kanban_default -p 8001:8000 -v $DATA:/data"

go test ./...
(cd web && npm run build >/dev/null)
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o paratrack ./cmd/paratrack

# ponytail: every deploy keeps one prev-<stamp> image; prune by hand when disk matters.
docker tag paratrack:latest "paratrack:prev-$STAMP"
docker build -q -t paratrack:latest . >/dev/null

# Stop first so the SQLite WAL is checkpointed and the copy is consistent.
docker stop paratrack >/dev/null
mkdir -p "$BACKUPS/$STAMP"
cp -p "$DATA"/.track/* "$BACKUPS/$STAMP/"
docker rm paratrack >/dev/null
# shellcheck disable=SC2086
docker run -d --name paratrack $RUN paratrack:latest >/dev/null

for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -fsS -o /dev/null "$URL"; then
		echo "deployed $STAMP, backup in $BACKUPS/$STAMP"
		exit 0
	fi
	sleep 1
done
echo "health check failed. Roll back with:" >&2
echo "  docker rm -f paratrack && docker run -d --name paratrack $RUN paratrack:prev-$STAMP" >&2
exit 1
