#!/usr/bin/env bash
# Nightly Postgres dump to S3 (Backblaze B2 on the prod host), run by cron.
# Streams pg_dump | gzip straight into the bucket, then drops dumps older
# than KEEP_DAYS. Reads S3_* and the compose project from .env (gitignored).
#
# Restore (into the running stack, destructive):
#   mc cat b2/$S3_BUCKET/daily/<stamp>.sql.gz | gunzip | \
#     docker compose exec -T db psql -U paratrack -d paratrack
# Check a dump without touching prod: scripts/backup-s3.sh --verify
set -Eeuo pipefail # -E: the ERR trap fires inside functions too
cd "$(dirname "$0")/.."
set -a; . ./.env; set +a

KEEP_DAYS=${KEEP_DAYS:-30}
STAMP=$(date -u +%Y%m%d-%H%M%S)
KEY="daily/$STAMP.sql.gz"

mc() {
	docker run --rm -i -e K="$S3_KEY_ID" -e SK="$S3_SECRET_KEY" -e EP="$S3_ENDPOINT" \
		--entrypoint sh minio/mc:latest -c \
		'mc alias set b2 "$EP" "$K" "$SK" --api S3v4 >/dev/null && mc "$@"' mc "$@"
}

if [ "${1:-}" = "--verify" ]; then
	# Restore the newest dump into a throwaway Postgres and compare row counts.
	latest=$(mc ls "b2/$S3_BUCKET/daily/" | awk '{print $NF}' | sort | tail -1)
	[ -n "$latest" ] || { echo "no dumps in b2/$S3_BUCKET/daily/" >&2; exit 1; }
	name=paratrack-restore-check
	docker rm -f "$name" >/dev/null 2>&1 || true
	docker run -d --name "$name" -e POSTGRES_PASSWORD=x -e POSTGRES_USER=paratrack -e POSTGRES_DB=paratrack postgres:17-alpine >/dev/null
	trap 'docker rm -f "$name" >/dev/null 2>&1' EXIT
	until docker exec "$name" pg_isready -U paratrack -d paratrack >/dev/null 2>&1; do sleep 1; done
	sleep 2
	mc cat "b2/$S3_BUCKET/daily/$latest" | gunzip | docker exec -i "$name" psql -q -v ON_ERROR_STOP=1 -U paratrack -d paratrack >/dev/null
	q="SELECT (SELECT count(*) FROM users)||' users, '||(SELECT count(*) FROM sessions)||' sessions'"
	echo "dump $latest: $(docker exec "$name" psql -tA -U paratrack -d paratrack -c "$q")"
	echo "live now:     $(docker compose exec -T db psql -tA -U paratrack -d paratrack -c "$q")"
	exit 0
fi

# Sentry Cron Monitor (monitor "paratrack-backup"): check in at start and
# end. A failed run reports error; a run that never starts is flagged by
# Sentry itself from the schedule below. No DSN: silently skipped.
checkin() { # status [duration]
	[ -n "${PARATRACK_SENTRY_DSN:-}" ] || return 0
	local rest="${PARATRACK_SENTRY_DSN#https://}" key host proj
	key="${rest%%@*}"; rest="${rest#*@}"; host="${rest%%/*}"; proj="${rest##*/}"
	printf '{}\n{"type":"check_in"}\n{"check_in_id":"%s","monitor_slug":"paratrack-backup","status":"%s"%s,"monitor_config":{"schedule":{"type":"crontab","value":"30 3 * * *"},"checkin_margin":30,"max_runtime":30,"timezone":"UTC"}}\n' \
		"$CHECKIN_ID" "$1" "${2:+,\"duration\":$2}" |
		curl -fsS -m 10 -o /dev/null -X POST "https://$host/api/$proj/envelope/" \
			-H "Content-Type: application/x-sentry-envelope" \
			-H "X-Sentry-Auth: Sentry sentry_version=7, sentry_key=$key, sentry_client=paratrack-backup/1.0" --data-binary @- || true
}
CHECKIN_ID=$(cat /proc/sys/kernel/random/uuid | tr -d -)
T0=$(date +%s)
trap 'checkin error $(( $(date +%s) - T0 ))' ERR
checkin in_progress

docker compose exec -T db pg_dump -U paratrack -d paratrack | gzip | mc pipe --quiet "b2/$S3_BUCKET/$KEY"
mc rm --recursive --force --older-than "${KEEP_DAYS}d" "b2/$S3_BUCKET/daily/" >/dev/null || true
checkin ok $(( $(date +%s) - T0 ))
echo "$(date -u +%FT%TZ) uploaded $KEY"
