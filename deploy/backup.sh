#!/usr/bin/env bash
# Consistent backup of the gallery data directory.
#
#   deploy/backup.sh /path/to/data /path/to/backup-destination
#
# 1. Snapshots the SQLite database with VACUUM INTO (safe while the server is
#    running and writing, thanks to WAL).
# 2. rsyncs photos/ and the snapshot to the destination (local path or
#    user@host:/path). Deleted photos are removed from the destination too.
#
# Requires sqlite3 and rsync. For continuous replication of the database,
# Litestream (https://litestream.io) is an optional addition.
set -euo pipefail

DATA_DIR=${1:?usage: backup.sh DATA_DIR DEST}
DEST=${2:?usage: backup.sh DATA_DIR DEST}

DB="$DATA_DIR/gallery.db"
SNAPSHOT_DIR=$(mktemp -d)
trap 'rm -rf "$SNAPSHOT_DIR"' EXIT

if [ ! -f "$DB" ]; then
  echo "no database at $DB" >&2
  exit 1
fi

sqlite3 "$DB" "VACUUM INTO '$SNAPSHOT_DIR/gallery.db'"
sqlite3 "$SNAPSHOT_DIR/gallery.db" "PRAGMA integrity_check" | grep -qx ok

rsync -a --delete "$DATA_DIR/photos/" "$DEST/photos/"
rsync -a "$SNAPSHOT_DIR/gallery.db" "$DEST/gallery.db"

echo "backup complete: $(du -sh "$DATA_DIR/photos" | cut -f1) of photos, db $(du -h "$SNAPSHOT_DIR/gallery.db" | cut -f1)"
