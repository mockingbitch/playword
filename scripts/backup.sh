#!/bin/sh
# Backs up the database to a compressed custom-format dump (runbook: backup).
#   DATABASE_URL=… scripts/backup.sh [file]
# rhyme_keys is derived data (rebuild with `playword-cli rebuild-keys`), so it
# is left out to keep backups small; pass FULL=1 to include it.
set -eu
: "${DATABASE_URL:?set DATABASE_URL}"
out=${1:-playword-$(date -u +%Y%m%dT%H%M%SZ).dump}
exclude="--exclude-table-data=rhyme_keys"
[ "${FULL:-0}" = 1 ] && exclude=""
pg_dump --format=custom --no-owner --compress=9 $exclude --file="$out" "$DATABASE_URL"
echo "$out"
