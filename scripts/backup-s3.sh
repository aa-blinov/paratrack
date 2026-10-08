#!/usr/bin/env bash
# Nightly Postgres dump to S3 (Backblaze B2 on the prod host), run by cron.
# Streams pg_dump | gzip straight into the bucket, then drops dumps older
# than KEEP_DAYS. Reads S3_* and the compose project from .env (gitignored).
#
# Restore (into the running stack, destructive):
#   aws s3 cp s3://$S3_BUCKET/daily/<stamp>.sql.gz - | gunzip | \
#     docker compose exec -T db psql -U paratrack -d paratrack
# Check a dump without touching prod: scripts/backup-s3.sh --verify
set -Eeuo pipefail # -E: the ERR trap fires inside functions too
cd "$(dirname "$0")/.."
set -a; . ./.env; set +a

KEEP_DAYS=${KEEP_DAYS:-30}
STAMP=$(date -u +%Y%m%d-%H%M%S)
KEY="daily/$STAMP.sql.gz"

# The MinIO client image this used is gone from Docker Hub, so every nightly
# run failed at the alias step and the bucket silently stopped receiving
# dumps — 27 September was the last one. The AWS CLI is still published and
# speaks the same S3 API, so the transfer runs through it instead.
AWS_CLI_IMAGE=${AWS_CLI_IMAGE:-amazon/aws-cli:2.27.0}

# -i keeps stdin attached: the dump is piped in through it. Without it the
# upload "succeeds" and writes a zero-byte object that looks like a backup.
aws() {
	docker run --rm -i -e AWS_ACCESS_KEY_ID="$S3_KEY_ID" -e AWS_SECRET_ACCESS_KEY="$S3_SECRET_KEY" \
		"$AWS_CLI_IMAGE" --endpoint-url "$S3_ENDPOINT" "$@"
}

if [ "${1:-}" = "--verify" ]; then
	# Restore the newest dump into a throwaway Postgres and compare row counts.
	latest=$(aws s3 ls "s3://$S3_BUCKET/daily/" | awk '{print $NF}' | sort | tail -1)
	[ -n "$latest" ] || { echo "no dumps in b2/$S3_BUCKET/daily/" >&2; exit 1; }
	name=paratrack-restore-check
	docker rm -f "$name" >/dev/null 2>&1 || true
	docker run -d --name "$name" -e POSTGRES_PASSWORD=x -e POSTGRES_USER=paratrack -e POSTGRES_DB=paratrack postgres:17-alpine >/dev/null
	trap 'docker rm -f "$name" >/dev/null 2>&1' EXIT
	until docker exec "$name" pg_isready -U paratrack -d paratrack >/dev/null 2>&1; do sleep 1; done
	sleep 2
	aws s3 cp "s3://$S3_BUCKET/daily/$latest" - | gunzip | docker exec -i "$name" psql -q -v ON_ERROR_STOP=1 -U paratrack -d paratrack >/dev/null
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

docker compose exec -T db pg_dump -U paratrack -d paratrack | gzip | aws s3 cp - "s3://$S3_BUCKET/$KEY" --quiet
# Retention without mc's --older-than: the stamps are YYYYMMDD-HHMMSS, which
# sorts as text, so the cutoff is a plain string comparison.
cutoff=$(date -u -d "${KEEP_DAYS} days ago" +%Y%m%d-%H%M%S)
aws s3 ls "s3://$S3_BUCKET/daily/" | awk '{print $NF}' | sort | while read -r old; do
	case "$old" in
		*.sql.gz) ;;
		*) continue ;;
	esac
	[ "${old%%.sql.gz}" \< "$cutoff" ] || continue
	aws s3 rm "s3://$S3_BUCKET/daily/$old" --quiet
done
checkin ok $(( $(date +%s) - T0 ))
echo "$(date -u +%FT%TZ) uploaded $KEY"
