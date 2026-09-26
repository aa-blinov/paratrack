#!/bin/sh
# Deploy the working tree to paratrack.duckdns.org (this VPS) with compose.
# Build (the test suite runs inside the image build), dump Postgres,
# recreate the app container, health-check.
# Rollback: the previous image stays tagged paratrack:prev-<stamp>.
#
# Host wiring (not committed): .env holds POSTGRES_PASSWORD, and
# docker-compose.override.yml puts the app on the machine-wide `edge`
# network at 172.40.0.20 for ~/infra/caddy.
set -eu
cd "$(dirname "$0")/.."

BACKUPS=/home/ubuntu/paratrack-backups
URL=https://paratrack.duckdns.org/login
STAMP=$(date -u +%Y%m%d-%H%M%S)

# ponytail: every deploy keeps one prev-<stamp> image and one dump; prune by hand when disk matters.
if docker image inspect paratrack:latest >/dev/null 2>&1; then
	docker tag paratrack:latest "paratrack:prev-$STAMP"
fi
docker compose build app

docker compose up -d --wait db
mkdir -p "$BACKUPS"
docker compose exec -T db pg_dump -U paratrack -d paratrack | gzip >"$BACKUPS/$STAMP.sql.gz"

docker compose up -d --no-build app

for _ in $(seq 1 20); do
	if curl -fsS -o /dev/null "$URL"; then
		echo "deployed $STAMP, db dump in $BACKUPS/$STAMP.sql.gz"
		exit 0
	fi
	sleep 1
done
echo "health check failed. Roll back with:" >&2
echo "  docker tag paratrack:prev-$STAMP paratrack:latest && docker compose up -d --no-build app" >&2
exit 1
