#!/bin/sh
# Restores a dump from .scripts/backup.sh into an EMPTY database, then
# rebuilds the derived rhyme keys (runbook: restore).
#   DATABASE_URL=… .scripts/restore.sh file.dump
set -eu
: "${DATABASE_URL:?set DATABASE_URL}"
dump=${1:?usage: restore.sh file.dump}
pg_restore --no-owner --exit-on-error --dbname="$DATABASE_URL" "$dump"
"${PLAYWORD_CLI:-playword-cli}" rebuild-keys
echo "restored $dump"
