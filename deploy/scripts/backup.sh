#!/usr/bin/env sh
# Back up what cannot be rebuilt: the PostgreSQL database and the uploaded files
# (school logos). Redis holds only live/temporary data and is not backed up.
#
#   deploy/scripts/backup.sh [backup-dir]      (default: /var/backups/sbts)
#
# Run daily from cron, e.g.  15 2 * * *  /opt/sbts/deploy/scripts/backup.sh >> /var/log/sbts-backup.log 2>&1
# Then copy the backup dir OFF the server (another machine or object storage): a backup
# on the same disk does not survive losing the server. deploy/.env is NOT included:
# keep a copy of it in a password manager or secret store (it holds DATA_ENCRYPTION_KEY).
set -eu

DEPLOY_DIR=$(cd "$(dirname "$0")/.." && pwd)
DEST=${1:-/var/backups/sbts}
KEEP_DAYS=${KEEP_DAYS:-14}
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
PROJECT=${COMPOSE_PROJECT_NAME:-sbts} # the "name:" in docker-compose.yml

mkdir -p "$DEST"
chmod 700 "$DEST"
cd "$DEPLOY_DIR"

echo "[$STAMP] database..."
docker compose exec -T postgres pg_dump -U sbts -d sbts --format=custom --no-owner > "$DEST/db-$STAMP.dump.tmp"
mv "$DEST/db-$STAMP.dump.tmp" "$DEST/db-$STAMP.dump"

echo "[$STAMP] uploads..."
docker run --rm -v "${PROJECT}_uploads:/data:ro" -v "$DEST:/backup" alpine:3.22 \
  tar -czf "/backup/uploads-$STAMP.tar.gz.tmp" -C /data .
mv "$DEST/uploads-$STAMP.tar.gz.tmp" "$DEST/uploads-$STAMP.tar.gz"

chmod 600 "$DEST"/db-"$STAMP".dump "$DEST"/uploads-"$STAMP".tar.gz
find "$DEST" -maxdepth 1 -type f \( -name 'db-*.dump' -o -name 'uploads-*.tar.gz' \) -mtime +"$KEEP_DAYS" -delete

echo "[$STAMP] done: $DEST/db-$STAMP.dump, $DEST/uploads-$STAMP.tar.gz"
