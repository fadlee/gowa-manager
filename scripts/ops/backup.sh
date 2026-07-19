#!/bin/sh
# shellcheck shell=sh
#
# GOWA Manager — pre-cutover backup (POSIX sh).
#
# Run AFTER stopping Bun but BEFORE starting Go.  Creates a consistent
# backup of the SQLite database (online backup for WAL, safe file copy
# otherwise), instance/version metadata, and a SHA-256 manifest that is
# verified immediately after writing.
#
# This script does NOT stop the manager — the operator must ensure Bun is
# stopped and child processes are stabilised before invoking it.
#
# Usage:
#   backup.sh [-d|--data-dir DIR] [-o|--backup-dir DIR] [--sqlite-bin PATH]
#
# Never prints passwords, instance config, or webhook URLs.
# Never claims atomicity across multiple files.

set -u

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
DATA_DIR="./data"
BACKUP_DIR=""
SQLITE_BIN="${SQLITE_BIN:-sqlite3}"
VERIFY=false

# ---------------------------------------------------------------------------
# Helpers (same conventions as preflight.sh)
# ---------------------------------------------------------------------------
jstr() {
  _v=$1
  _v=$(printf '%s' "$_v" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')
  printf '"%s"' "$_v"
}
now_iso() { date -u '+%Y-%m-%dT%H:%M:%SZ'; }
log() { printf '%s\n' "$1" >&2; }

# Compute SHA-256 of a file and print just the hex digest.  Strips the
# leading backslash that some sha256sum implementations emit in text mode.
# Uses a safe two-branch pattern (no eval) to avoid command injection when
# file paths contain special characters.
compute_sha() {
  if [ "$SHA_CMD" = "sha256sum" ]; then
    _h=$(sha256sum "$1" 2>/dev/null | awk '{print $1}')
  else
    _h=$(shasum -a 256 "$1" 2>/dev/null | awk '{print $1}')
  fi
  # Strip a leading backslash (text-mode indicator on some platforms).
  _h=${_h#\\}
  [ -n "$_h" ] || _h="error"
  printf '%s' "$_h"
}

# File size in bytes.
file_size() {
  _s=$(wc -c < "$1" 2>/dev/null | tr -d ' ')
  [ -n "$_s" ] || _s=0
  printf '%s' "$_s"
}

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    -d|--data-dir)  DATA_DIR=$2; shift 2 ;;
    -o|--backup-dir) BACKUP_DIR=$2; shift 2 ;;
    --sqlite-bin)   SQLITE_BIN=$2; shift 2 ;;
    --verify)       VERIFY=true; shift ;;
    -h|--help)
      cat >&2 <<'EOF'
GOWA Manager pre-cutover backup.

Usage: backup.sh [-d|--data-dir DIR] [-o|--backup-dir DIR] [--sqlite-bin PATH]
                 [--verify]

Options:
  -d, --data-dir DIR     Data directory to back up (default: ./data).
  -o, --backup-dir DIR   Destination directory for the backup.
                          (default: ./backup/<timestamp>)
  --sqlite-bin PATH      Path to sqlite3 CLI (default: sqlite3 from PATH).
  --verify               Re-read the manifest in the backup dir and re-hash
                          all files; exit non-zero on mismatch.  No backup
                          is performed in this mode.
EOF
      exit 0 ;;
    *) log "Unknown option: $1"; exit 2 ;;
  esac
done

START_TS=$(now_iso)

# Default backup dir with timestamp.
if [ -z "$BACKUP_DIR" ]; then
  BACKUP_DIR="./backup/$(date -u '+%Y%m%d-%H%M%S')"
fi

# ---------------------------------------------------------------------------
# Initialise output accumulators (defined before verify mode so that the
# verify branch can call add_error before its own definitions would appear).
# ---------------------------------------------------------------------------
ERRORS=""
FILES_JSON=""
add_error() {
  if [ -z "$ERRORS" ]; then
    ERRORS=$(jstr "$1")
  else
    ERRORS="$ERRORS,$(jstr "$1")"
  fi
}
add_file() {
  # $1=relative path  $2=sha256  $3=size
  _entry="{\"path\":$(jstr "$1"),\"sha256\":$(jstr "$2"),\"size\":$3}"
  if [ -z "$FILES_JSON" ]; then
    FILES_JSON="$_entry"
  else
    FILES_JSON="$FILES_JSON,$_entry"
  fi
}

# ---------------------------------------------------------------------------
# Verify mode: re-read the manifest and re-hash all files.  No backup is
# performed.  Exits non-zero on any mismatch or missing file.
# ---------------------------------------------------------------------------
if [ "$VERIFY" = true ]; then
  ERRORS=""
  manifest_name="manifest.sha256"
  manifest_path="$BACKUP_DIR/$manifest_name"
  manifest_verified=true
  file_count=0

  # Detect hashing command.
  SHA_CMD=""
  if command -v sha256sum >/dev/null 2>&1; then
    SHA_CMD=sha256sum
  elif command -v shasum >/dev/null 2>&1; then
    SHA_CMD="shasum -a 256"
  fi
  if [ -z "$SHA_CMD" ]; then
    add_error "neither sha256sum nor shasum is available"
    manifest_verified=false
  elif [ ! -f "$manifest_path" ]; then
    add_error "manifest not found: $manifest_path"
    manifest_verified=false
  else
    while IFS= read -r line; do
      [ -z "$line" ] && continue
      file_count=$((file_count + 1))
      _expected_sha=$(printf '%s' "$line" | awk '{print $1}')
      _rel_path=$(printf '%s' "$line" | awk '{print $2}')
      _full_path="$BACKUP_DIR/$_rel_path"
      if [ ! -f "$_full_path" ]; then
        manifest_verified=false
        add_error "manifest verify: file missing: $_rel_path"
        break
      fi
      _actual_sha=$(compute_sha "$_full_path")
      if [ "$_expected_sha" != "$_actual_sha" ]; then
        manifest_verified=false
        add_error "manifest verify: checksum mismatch for $_rel_path"
        break
      fi
    done < "$manifest_path"
  fi

  verify_exit=0
  [ "$manifest_verified" = false ] && verify_exit=1
  END_TS=$(now_iso)
  cat <<EOF
{
  "tool": "backup",
  "schema_version": 1,
  "mode": "verify",
  "start_timestamp": $(jstr "$START_TS"),
  "end_timestamp": $(jstr "$END_TS"),
  "data_dir": $(jstr "$DATA_DIR"),
  "backup_dir": $(jstr "$BACKUP_DIR"),
  "manager_downtime": {
    "state": "assumed_stopped",
    "note": "verify mode - no backup performed"
  },
  "journal_mode": "unknown",
  "method": "verify",
  "files": [],
  "manifest": {
    "path": $(jstr "$manifest_name"),
    "verified": $( [ "$manifest_verified" = true ] && echo true || echo false ),
    "file_count": $file_count
  },
  "metadata": {"instances_copied": 0, "versions_copied": 0},
  "errors": [$ERRORS],
  "exit_code": $verify_exit
}
EOF
  log ""
  log "=== GOWA Manager Backup Verify ==="
  log "Backup dir:   $BACKUP_DIR"
  log "Manifest:     $( [ "$manifest_verified" = true ] && echo 'verified' || echo 'VERIFICATION FAILED')"
  log "Files:        $file_count checked"
  if [ -n "$ERRORS" ]; then
    log ""
    log "Errors:"
    echo "$ERRORS" | tr ',' '\n' | sed 's/"//g' | while read -r e; do
      [ -n "$e" ] && log "  - $e"
    done
  fi
  log ""
  if [ "$verify_exit" -eq 0 ]; then
    log "Result: VERIFY OK — manifest verified"
  else
    log "Result: VERIFY FAILED — see errors above"
  fi
  exit $verify_exit
fi

# ---------------------------------------------------------------------------
# Validate prerequisites
# ---------------------------------------------------------------------------
exit_code=0

if [ ! -d "$DATA_DIR" ]; then
  add_error "data directory does not exist: $DATA_DIR"
  exit_code=1
fi

# sqlite3 CLI is required for journal-mode detection and online backup.
sqlite3_available=false
command -v "$SQLITE_BIN" >/dev/null 2>&1 && sqlite3_available=true
if [ "$sqlite3_available" = false ]; then
  add_error "sqlite3 CLI not found: $SQLITE_BIN"
  exit_code=1
fi

# sha256sum or shasum for manifest generation.
SHA_CMD=""
if command -v sha256sum >/dev/null 2>&1; then
  SHA_CMD=sha256sum
elif command -v shasum >/dev/null 2>&1; then
  SHA_CMD="shasum -a 256"
fi
if [ -z "$SHA_CMD" ]; then
  add_error "neither sha256sum nor shasum is available"
  exit_code=1
fi

# If prerequisites failed, emit JSON and exit early.
if [ "$exit_code" -ne 0 ]; then
  END_TS=$(now_iso)
  cat <<EOF
{
  "tool": "backup",
  "schema_version": 1,
  "start_timestamp": $(jstr "$START_TS"),
  "end_timestamp": $(jstr "$END_TS"),
  "data_dir": $(jstr "$DATA_DIR"),
  "backup_dir": $(jstr "$BACKUP_DIR"),
  "manager_downtime": {
    "state": "assumed_stopped",
    "note": "script does not stop manager; ensure Bun is stopped before running"
  },
  "journal_mode": "unknown",
  "method": "none",
  "files": [],
  "manifest": {"path": "", "verified": false, "file_count": 0},
  "metadata": {"instances_copied": 0, "versions_copied": 0},
  "errors": [$ERRORS],
  "exit_code": $exit_code
}
EOF
  log "Backup failed: prerequisites not met"
  exit $exit_code
fi

# ---------------------------------------------------------------------------
# Create backup directory
# ---------------------------------------------------------------------------
if ! mkdir -p "$BACKUP_DIR"; then
  add_error "cannot create backup directory: $BACKUP_DIR"
  exit_code=1
  END_TS=$(now_iso)
  cat <<EOF
{
  "tool": "backup",
  "schema_version": 1,
  "start_timestamp": $(jstr "$START_TS"),
  "end_timestamp": $(jstr "$END_TS"),
  "data_dir": $(jstr "$DATA_DIR"),
  "backup_dir": $(jstr "$BACKUP_DIR"),
  "manager_downtime": {
    "state": "assumed_stopped",
    "note": "script does not stop manager; ensure Bun is stopped before running"
  },
  "journal_mode": "unknown",
  "method": "none",
  "files": [],
  "manifest": {"path": "", "verified": false, "file_count": 0},
  "metadata": {"instances_copied": 0, "versions_copied": 0},
  "errors": [$ERRORS],
  "exit_code": $exit_code
}
EOF
  exit $exit_code
fi

# ---------------------------------------------------------------------------
# Determine journal mode and choose backup method
# ---------------------------------------------------------------------------
DB_PATH="$DATA_DIR/gowa.db"
journal_mode="unknown"
method="none"

if [ -f "$DB_PATH" ]; then
  journal_mode=$("$SQLITE_BIN" "$DB_PATH" "PRAGMA journal_mode;" 2>/dev/null | head -1)
  [ -n "$journal_mode" ] || journal_mode="unknown"
fi

# ---------------------------------------------------------------------------
# Back up the SQLite database
# ---------------------------------------------------------------------------
db_backed_up=false
db_backup_name="gowa.db"

if [ -f "$DB_PATH" ]; then
  db_dest="$BACKUP_DIR/$db_backup_name"
  if [ "$journal_mode" = "wal" ]; then
    # Use SQLite online backup API via the .backup command.
    # This produces a consistent snapshot even if WAL files exist.
    if "$SQLITE_BIN" "$DB_PATH" ".backup '$db_dest'" 2>/dev/null; then
      method="online_backup"
      db_backed_up=true
    else
      add_error "SQLite online backup failed"
      exit_code=1
    fi
  else
    # Safe file copy for non-WAL modes (delete/rollback/truncate).
    # The caller must ensure no writer is active.
    if cp "$DB_PATH" "$db_dest" 2>/dev/null; then
      method="file_copy"
      db_backed_up=true
    else
      add_error "file copy of database failed"
      exit_code=1
    fi
  fi

  if [ "$db_backed_up" = true ]; then
    db_sha=$(compute_sha "$db_dest")
    db_size=$(file_size "$db_dest")
    add_file "$db_backup_name" "$db_sha" "$db_size"
  fi
else
  add_error "database file not found: $DB_PATH"
  exit_code=1
fi

# ---------------------------------------------------------------------------
# Copy instance metadata (JSON export from DB — never includes config column)
# ---------------------------------------------------------------------------
instances_copied=0
instances_meta_name="instances.json"
instances_meta_path="$BACKUP_DIR/$instances_meta_name"

if [ "$db_backed_up" = true ] || [ -f "$DB_PATH" ]; then
  # Export key, name, port, status, gowa_version, created_at, updated_at.
  # Deliberately omit config (may contain tokens) and error_message.
  _q="SELECT key,name,port,status,gowa_version,created_at,updated_at FROM instances;"
  _data=$("$SQLITE_BIN" -json "$DB_PATH" "$_q" 2>/dev/null)
  # sqlite3 -json outputs nothing (not []) when there are zero rows.
  [ -n "$_data" ] || _data="[]"
  if printf '%s' "$_data" > "$instances_meta_path" 2>/dev/null; then
    im_sha=$(compute_sha "$instances_meta_path")
    im_size=$(file_size "$instances_meta_path")
    add_file "$instances_meta_name" "$im_sha" "$im_size"
    # Count instances by counting "key" fields in the JSON.
    instances_copied=$(printf '%s' "$_data" | grep -o '"key"' 2>/dev/null | wc -l | tr -d ' ')
  else
    add_error "failed to write instances metadata"
  fi
fi

# ---------------------------------------------------------------------------
# Copy version metadata (list of installed versions)
# ---------------------------------------------------------------------------
versions_copied=0
versions_meta_name="versions.json"
versions_meta_path="$BACKUP_DIR/$versions_meta_name"

versions_dir="$DATA_DIR/bin/versions"
_vdata="["
_vfirst=1
if [ -d "$versions_dir" ]; then
  for vdir in "$versions_dir"/*/; do
    [ -d "$vdir" ] || continue
    vname=$(basename "$vdir")
    case "$vname" in
      .install-*) continue ;;
    esac
    gowa_path="$vdir/gowa"
    [ -f "$gowa_path" ] || gowa_path="$vdir/gowa.exe"
    vsize=0
    [ -f "$gowa_path" ] && vsize=$(file_size "$gowa_path")
    [ -n "$vsize" ] || vsize=0
    if [ $_vfirst -eq 1 ]; then
      _vfirst=0
    else
      _vdata="$_vdata,"
    fi
    _vdata="$_vdata{\"version\":$(jstr "$vname"),\"path\":$(jstr "$gowa_path"),\"size\":$vsize}"
    versions_copied=$((versions_copied + 1))
  done
fi
_vdata="$_vdata]"
if printf '%s' "$_vdata" > "$versions_meta_path" 2>/dev/null; then
  vm_sha=$(compute_sha "$versions_meta_path")
  vm_size=$(file_size "$versions_meta_path")
  add_file "$versions_meta_name" "$vm_sha" "$vm_size"
else
  add_error "failed to write versions metadata"
fi

# ---------------------------------------------------------------------------
# Generate SHA-256 manifest and verify immediately
# ---------------------------------------------------------------------------
manifest_name="manifest.sha256"
manifest_path="$BACKUP_DIR/$manifest_name"
manifest_verified=false
file_count=0

# Build manifest content: "<sha256>  <relative_path>" per line.
# We compute each hash with compute_sha (which strips the leading backslash
# that some sha256sum implementations emit) and write the line directly,
# avoiding fragile sed substitutions on Windows-style paths.
_manifest_tmp="$BACKUP_DIR/.manifest.tmp"
: > "$_manifest_tmp"
if [ "$db_backed_up" = true ]; then
  _h=$(compute_sha "$BACKUP_DIR/$db_backup_name")
  printf '%s  %s\n' "$_h" "$db_backup_name" >> "$_manifest_tmp"
fi
if [ -f "$instances_meta_path" ]; then
  _h=$(compute_sha "$instances_meta_path")
  printf '%s  %s\n' "$_h" "$instances_meta_name" >> "$_manifest_tmp"
fi
if [ -f "$versions_meta_path" ]; then
  _h=$(compute_sha "$versions_meta_path")
  printf '%s  %s\n' "$_h" "$versions_meta_name" >> "$_manifest_tmp"
fi

# Write manifest.
mv "$_manifest_tmp" "$manifest_path" 2>/dev/null

# Verify manifest: re-hash each file and compare.
if [ -f "$manifest_path" ]; then
  manifest_verified=true
  file_count=0
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    file_count=$((file_count + 1))
    _expected_sha=$(printf '%s' "$line" | awk '{print $1}')
    _rel_path=$(printf '%s' "$line" | awk '{print $2}')
    _full_path="$BACKUP_DIR/$_rel_path"
    if [ ! -f "$_full_path" ]; then
      manifest_verified=false
      add_error "manifest verify: file missing: $_rel_path"
      break
    fi
    _actual_sha=$(compute_sha "$_full_path")
    if [ "$_expected_sha" != "$_actual_sha" ]; then
      manifest_verified=false
      add_error "manifest verify: checksum mismatch for $_rel_path"
      break
    fi
  done < "$manifest_path"
fi

if [ "$manifest_verified" = false ]; then
  exit_code=1
fi

# Add manifest itself to the files list.
if [ -f "$manifest_path" ]; then
  m_sha=$(compute_sha "$manifest_path")
  m_size=$(file_size "$manifest_path")
  add_file "$manifest_name" "$m_sha" "$m_size"
fi

END_TS=$(now_iso)

# ---------------------------------------------------------------------------
# Emit JSON
# ---------------------------------------------------------------------------
cat <<EOF
{
  "tool": "backup",
  "schema_version": 1,
  "start_timestamp": $(jstr "$START_TS"),
  "end_timestamp": $(jstr "$END_TS"),
  "data_dir": $(jstr "$DATA_DIR"),
  "backup_dir": $(jstr "$BACKUP_DIR"),
  "manager_downtime": {
    "state": "assumed_stopped",
    "note": "script does not stop manager; ensure Bun is stopped before running"
  },
  "journal_mode": $(jstr "$journal_mode"),
  "method": $(jstr "$method"),
  "files": [$FILES_JSON],
  "manifest": {
    "path": $(jstr "$manifest_name"),
    "verified": $( [ "$manifest_verified" = true ] && echo true || echo false ),
    "file_count": $file_count
  },
  "metadata": {
    "instances_copied": $instances_copied,
    "versions_copied": $versions_copied
  },
  "errors": [$ERRORS],
  "exit_code": $exit_code
}
EOF

# ---------------------------------------------------------------------------
# Human summary to stderr
# ---------------------------------------------------------------------------
log ""
log "=== GOWA Manager Backup ==="
log "Data dir:     $DATA_DIR"
log "Backup dir:   $BACKUP_DIR"
log "Journal mode: $journal_mode"
log "Method:       $method"
log "Files:        $file_count backed up"
log "Manifest:     $( [ "$manifest_verified" = true ] && echo 'verified' || echo 'VERIFICATION FAILED')"
log "Instances:    $instances_copied"
log "Versions:     $versions_copied"
log "Start:        $START_TS"
log "End:          $END_TS"
if [ -n "$ERRORS" ]; then
  log ""
  log "Errors:"
  echo "$ERRORS" | tr ',' '\n' | sed 's/"//g' | while read -r e; do
    [ -n "$e" ] && log "  - $e"
  done
fi
log ""
if [ "$exit_code" -eq 0 ]; then
  log "Result: BACKUP OK — manifest verified"
else
  log "Result: BACKUP FAILED — see errors above"
fi

exit $exit_code
