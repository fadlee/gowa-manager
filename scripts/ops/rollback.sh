#!/bin/sh
# shellcheck shell=sh
#
# GOWA Manager — Go-to-Bun rollback orchestrator (POSIX sh).
#
# Reverts the system from the Go manager to the pinned Bun manager.
# This script orchestrates: stopping Go traffic, waiting for lifecycle
# operations to quiesce, recording child process state, capturing logs
# and the current DB, running SQLite integrity/schema checks, choosing
# a DB strategy (use current or restore named backup), starting the
# pinned Bun command, and running Bun smoke tests.
#
# Default mode is DRY-RUN: prints what would happen and exits 0.
# --execute is required to actually perform the rollback.
#
# Usage:
#   rollback.sh [--execute] [--backup-dir <path>] [--go-pid <pid>]
#               [--go-version <version>] [--bun-binary <path>]
#               [--bun-checksum <sha256>] [--data-dir <path>]
#               [--sqlite-bin <path>] [--bun-url <url>]
#               [--override-ambiguous-state]
#
# Never prints passwords, tokens, or webhook URLs.

set -u

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
EXECUTE=false
BACKUP_DIR=""
GO_PID=""
GO_VERSION=""
BUN_BINARY=""
BUN_CHECKSUM=""
DATA_DIR="./data"
SQLITE_BIN="${SQLITE_BIN:-sqlite3}"
BUN_URL="http://localhost:3000"
OVERRIDE_AMBIGUOUS=false

# ---------------------------------------------------------------------------
# Helpers (same conventions as preflight.sh / backup.sh)
# ---------------------------------------------------------------------------

# Escape a string for safe inclusion inside a JSON double-quoted string.
jstr() {
  _v=$1
  _v=$(printf '%s' "$_v" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')
  printf '"%s"' "$_v"
}

# Boolean JSON literal.
jbool() {
  if [ "$1" = "1" ] || [ "$1" = "true" ]; then
    printf 'true'
  else
    printf 'false'
  fi
}

# Current UTC timestamp in ISO-8601.
now_iso() {
  date -u '+%Y-%m-%dT%H:%M:%SZ'
}

# Print a human-readable line to stderr.
log() {
  printf '%s\n' "$1" >&2
}

# Compute SHA-256 of a file and print just the hex digest.
SHA_CMD=""
detect_sha_cmd() {
  if [ -z "$SHA_CMD" ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      SHA_CMD=sha256sum
    elif command -v shasum >/dev/null 2>&1; then
      SHA_CMD="shasum -a 256"
    fi
  fi
}
compute_sha() {
  detect_sha_cmd
  if [ -z "$SHA_CMD" ]; then
    printf 'error'
    return
  fi
  if [ "$SHA_CMD" = "sha256sum" ]; then
    _h=$(sha256sum "$1" 2>/dev/null | awk '{print $1}')
  else
    _h=$(shasum -a 256 "$1" 2>/dev/null | awk '{print $1}')
  fi
  _h=${_h#\\}
  [ -n "$_h" ] || _h="error"
  printf '%s' "$_h"
}

# ---------------------------------------------------------------------------
# Accumulators
# ---------------------------------------------------------------------------
STEPS=""
ERRORS=""
WARNINGS=""

add_step() {
  # $1=name  $2=status(pass/fail/skip)  $3=detail
  _entry="{\"name\":$(jstr "$1"),\"status\":$(jstr "$2"),\"detail\":$(jstr "$3")}"
  if [ -z "$STEPS" ]; then
    STEPS="$_entry"
  else
    STEPS="$STEPS,$_entry"
  fi
}

add_error() {
  if [ -z "$ERRORS" ]; then
    ERRORS=$(jstr "$1")
  else
    ERRORS="$ERRORS,$(jstr "$1")"
  fi
}

add_warning() {
  if [ -z "$WARNINGS" ]; then
    WARNINGS=$(jstr "$1")
  else
    WARNINGS="$WARNINGS,$(jstr "$1")"
  fi
}

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --execute)                 EXECUTE=true; shift ;;
    --backup-dir)              BACKUP_DIR=$2; shift 2 ;;
    --go-pid)                  GO_PID=$2; shift 2 ;;
    --go-version)              GO_VERSION=$2; shift 2 ;;
    --bun-binary)              BUN_BINARY=$2; shift 2 ;;
    --bun-checksum)            BUN_CHECKSUM=$2; shift 2 ;;
    --data-dir)                DATA_DIR=$2; shift 2 ;;
    --sqlite-bin)              SQLITE_BIN=$2; shift 2 ;;
    --bun-url)                 BUN_URL=$2; shift 2 ;;
    --override-ambiguous-state) OVERRIDE_AMBIGUOUS=true; shift ;;
    -h|--help)
      cat >&2 <<'EOF'
GOWA Manager rollback — revert from Go to Bun.

Usage: rollback.sh [--execute] [--backup-dir <path>] [--go-pid <pid>]
                   [--go-version <version>] [--bun-binary <path>]
                   [--bun-checksum <sha256>] [--data-dir <path>]
                   [--sqlite-bin <path>] [--bun-url <url>]
                   [--override-ambiguous-state]

Options:
  --execute                 Actually perform the rollback (default: dry-run).
  --backup-dir <path>       Backup directory (required when --execute).
  --go-pid <pid>            PID of the running Go manager (required when --execute).
  --go-version <version>    Go manager version string (required when --execute).
  --bun-binary <path>       Path to the pinned Bun binary (required when --execute).
  --bun-checksum <sha256>   Expected SHA-256 of the Bun binary (required when --execute).
  --data-dir <path>         Data directory (default: ./data).
  --sqlite-bin <path>       Path to sqlite3 CLI (default: sqlite3 from PATH).
  --bun-url <url>           Bun manager URL for smoke tests (default: http://localhost:3000).
  --override-ambiguous-state  Proceed even if child/process state is ambiguous.

Default mode is DRY-RUN: prints what would happen and exits 0.
EOF
      exit 0 ;;
    *) log "Unknown option: $1"; exit 2 ;;
  esac
done

START_TS=$(now_iso)

# ---------------------------------------------------------------------------
# Validate required arguments when --execute is set
# ---------------------------------------------------------------------------
if [ "$EXECUTE" = true ]; then
  missing=""
  [ -z "$BACKUP_DIR" ] && missing="$missing --backup-dir"
  [ -z "$GO_PID" ] && missing="$missing --go-pid"
  [ -z "$GO_VERSION" ] && missing="$missing --go-version"
  [ -z "$BUN_BINARY" ] && missing="$missing --bun-binary"
  [ -z "$BUN_CHECKSUM" ] && missing="$missing --bun-checksum"
  if [ -n "$missing" ]; then
    add_error "missing required arguments when --execute: $missing"
    END_TS=$(now_iso)
    cat <<EOF
{
  "tool": "rollback",
  "schema_version": 1,
  "mode": "execute",
  "start_timestamp": $(jstr "$START_TS"),
  "end_timestamp": $(jstr "$END_TS"),
  "steps": [$STEPS],
  "errors": [$ERRORS],
  "warnings": [$WARNINGS],
  "exit_code": 1
}
EOF
    log "Rollback failed: missing required arguments: $missing"
    exit 1
  fi
fi

# ---------------------------------------------------------------------------
# DRY-RUN mode: print the plan and exit 0
# ---------------------------------------------------------------------------
if [ "$EXECUTE" = false ]; then
  add_step "dry_run" "pass" "dry-run mode - no changes made"
  add_step "plan_verify_go_process" "skip" "would verify Go PID $(jstr "$GO_PID") is running and record version $(jstr "$GO_VERSION")"
  add_step "plan_stop_go" "skip" "would stop Go PID $(jstr "$GO_PID")"
  add_step "plan_quiesce" "skip" "would wait for lifecycle operations to quiesce"
  add_step "plan_record_children" "skip" "would record child process state from $(jstr "$DATA_DIR")"
  add_step "plan_capture_logs_db" "skip" "would capture logs and current DB to $(jstr "$BACKUP_DIR")"
  add_step "plan_integrity_check" "skip" "would run SQLite integrity and schema checks"
  add_step "plan_db_strategy" "skip" "would use current DB if compatible or restore named backup"
  add_step "plan_start_bun" "skip" "would start Bun binary $(jstr "$BUN_BINARY")"
  add_step "plan_bun_smoke" "skip" "would run smoke tests against $(jstr "$BUN_URL")"

  END_TS=$(now_iso)
  cat <<EOF
{
  "tool": "rollback",
  "schema_version": 1,
  "mode": "dry_run",
  "start_timestamp": $(jstr "$START_TS"),
  "end_timestamp": $(jstr "$END_TS"),
  "execute": false,
  "steps": [$STEPS],
  "errors": [$ERRORS],
  "warnings": [$WARNINGS],
  "exit_code": 0
}
EOF

  log ""
  log "=== GOWA Manager Rollback (DRY-RUN) ==="
  log "Mode:         dry-run (no changes made)"
  log "Go PID:       ${GO_PID:-<not specified>}"
  log "Go version:   ${GO_VERSION:-<not specified>}"
  log "Bun binary:   ${BUN_BINARY:-<not specified>}"
  log "Bun checksum: ${BUN_CHECKSUM:-<not specified>}"
  log "Data dir:     $DATA_DIR"
  log "Backup dir:   ${BACKUP_DIR:-<not specified>}"
  log "Bun URL:      $BUN_URL"
  log ""
  log "Plan:"
  log "  0. Verify Go process (PID ${GO_PID:-<not specified>}, version ${GO_VERSION:-<not specified>})"
  log "  1. Stop Go traffic (PID ${GO_PID:-<not specified>})"
  log "  2. Wait for lifecycle operations to quiesce"
  log "  3. Record child process state from $DATA_DIR"
  log "  4. Capture logs and current DB"
  log "  5. Run SQLite integrity and schema checks"
  log "  6. Choose DB strategy (use current or restore backup)"
  log "  7. Verify Bun binary checksum"
  log "  8. Start pinned Bun binary"
  log "  9. Run Bun smoke tests against $BUN_URL"
  log ""
  log "Result: DRY-RUN — no changes made. Use --execute to perform rollback."
  exit 0
fi

# ---------------------------------------------------------------------------
# EXECUTE mode
# ---------------------------------------------------------------------------
exit_code=0
DB_STRATEGY="none"
GO_PID_VERIFIED=false

# Step 0: Verify Go process is running and record version info.
# The --go-version is recorded as the expected version.  Runtime version
# verification is not possible without querying the process directly.
if kill -0 "$GO_PID" 2>/dev/null; then
  add_step "verify_go_process" "pass" "Go PID $GO_PID is running; expected version $GO_VERSION (runtime version verification not possible without querying process directly)"
  GO_PID_VERIFIED=true
else
  if [ "$OVERRIDE_AMBIGUOUS" = true ]; then
    add_step "verify_go_process" "pass" "Go PID $GO_PID not running (override); expected version $GO_VERSION"
    add_warning "Go PID $GO_PID not running; proceeded due to --override-ambiguous-state"
    GO_PID_VERIFIED=true
  else
    add_step "verify_go_process" "fail" "Go PID $GO_PID is not running; expected version $GO_VERSION"
    add_error "Go PID $GO_PID is not running; cannot verify Go process"
    exit_code=1
  fi
fi

# Step 1: Stop Go traffic (stop the Go process by PID).
if [ "$exit_code" -eq 0 ]; then
  if kill -0 "$GO_PID" 2>/dev/null; then
    if kill -TERM "$GO_PID" 2>/dev/null; then
      add_step "stop_go" "pass" "sent SIGTERM to PID $GO_PID"
    else
      add_step "stop_go" "fail" "failed to send SIGTERM to PID $GO_PID"
      add_error "cannot stop Go process: kill -TERM $GO_PID failed"
      exit_code=1
    fi
  else
    # Process not running — ambiguous state.
    if [ "$OVERRIDE_AMBIGUOUS" = true ]; then
      add_step "stop_go" "pass" "Go PID $GO_PID not running (override-ambiguous-state)"
      add_warning "Go PID $GO_PID was not running; proceeded due to --override-ambiguous-state"
    else
      add_step "stop_go" "fail" "Go PID $GO_PID not running and --override-ambiguous-state not set"
      add_error "Go PID $GO_PID is not running; use --override-ambiguous-state to proceed"
      exit_code=1
    fi
  fi
fi

# Step 2: Wait for lifecycle operations to quiesce (brief sleep).
if [ "$exit_code" -eq 0 ]; then
  # Wait up to 10 seconds for the process to exit.
  _waited=0
  while [ "$_waited" -lt 10 ]; do
    if ! kill -0 "$GO_PID" 2>/dev/null; then
      break
    fi
    sleep 1
    _waited=$((_waited + 1))
  done
  if kill -0 "$GO_PID" 2>/dev/null; then
    if [ "$OVERRIDE_AMBIGUOUS" = true ]; then
      add_step "quiesce" "pass" "Go PID $GO_PID still running after 10s (override)"
      add_warning "Go PID $GO_PID did not exit within 10s; proceeded due to override"
    else
      add_step "quiesce" "fail" "Go PID $GO_PID did not exit within 10s"
      add_error "Go process did not quiesce within 10s; use --override-ambiguous-state to proceed"
      exit_code=1
    fi
  else
    add_step "quiesce" "pass" "Go PID $GO_PID exited after ${_waited}s"
  fi
fi

# Step 3: Record child process state (running instances from DB).
if [ "$exit_code" -eq 0 ]; then
  DB_PATH="$DATA_DIR/gowa.db"
  child_count=0
  if [ -f "$DB_PATH" ] && command -v "$SQLITE_BIN" >/dev/null 2>&1; then
    child_count=$("$SQLITE_BIN" "$DB_PATH" \
      "SELECT COUNT(*) FROM instances WHERE status='running';" 2>/dev/null | tr -d ' ')
    [ -n "$child_count" ] || child_count=0
    add_step "record_children" "pass" "$child_count running instances recorded"
  else
    if [ "$OVERRIDE_AMBIGUOUS" = true ]; then
      add_step "record_children" "pass" "DB or sqlite3 unavailable (override)"
      add_warning "could not read child process state; proceeded due to override"
    else
      add_step "record_children" "fail" "DB or sqlite3 unavailable"
      add_error "cannot record child process state: DB or sqlite3 unavailable"
      exit_code=1
    fi
  fi
fi

# Step 4: Capture logs and current DB (call backup.sh).
# The capture goes to a subdirectory so the original pre-cutover backup
# in $BACKUP_DIR is preserved for the db_strategy restore step.
if [ "$exit_code" -eq 0 ]; then
  _script_dir=$(dirname "$0" 2>/dev/null)
  _backup_script="$_script_dir/backup.sh"
  _capture_dir="$BACKUP_DIR/rollback-capture"
  if [ -f "$_backup_script" ]; then
    # Run backup.sh to capture the current DB state to a subdirectory.
    _backup_out=$(sh "$_backup_script" --data-dir "$DATA_DIR" --backup-dir "$_capture_dir" 2>/dev/null)
    _backup_exit=$?
    if [ "$_backup_exit" -eq 0 ]; then
      add_step "capture_logs_db" "pass" "backup.sh completed to $_capture_dir"
    else
      if [ "$OVERRIDE_AMBIGUOUS" = true ]; then
        add_step "capture_logs_db" "pass" "backup.sh failed (override)"
        add_warning "backup.sh exited $_backup_exit; proceeded due to override"
      else
        add_step "capture_logs_db" "fail" "backup.sh exited $_backup_exit"
        add_error "backup.sh failed with exit code $_backup_exit"
        exit_code=1
      fi
    fi
  else
    add_step "capture_logs_db" "skip" "backup.sh not found at $_backup_script"
    add_warning "backup.sh not found; skipped DB capture"
  fi
fi

# Step 5: Run SQLite integrity check and schema check.
# Sets DB_COMPATIBLE for the db_strategy step.  Does NOT abort on failure
# — the db_strategy step handles recovery by restoring from backup.
DB_COMPATIBLE=false
if [ "$exit_code" -eq 0 ]; then
  DB_PATH="$DATA_DIR/gowa.db"
  if [ -f "$DB_PATH" ] && command -v "$SQLITE_BIN" >/dev/null 2>&1; then
    _integrity=$("$SQLITE_BIN" "$DB_PATH" "PRAGMA integrity_check;" 2>/dev/null | head -1)
    if [ "$_integrity" = "ok" ]; then
      add_step "integrity_check" "pass" "SQLite integrity check: ok"
    else
      add_step "integrity_check" "fail" "integrity check: $_integrity"
    fi
    # Schema check: verify instances table exists.
    _schema=$("$SQLITE_BIN" "$DB_PATH" \
      "SELECT name FROM sqlite_master WHERE type='table' AND name='instances';" 2>/dev/null | head -1)
    if [ "$_schema" = "instances" ]; then
      # Column check: verify required columns (same as preflight).
      required_cols="id key name port status config gowa_version created_at updated_at error_message"
      _cols_missing=0
      for col in $required_cols; do
        if ! "$SQLITE_BIN" "$DB_PATH" "PRAGMA table_info(instances);" 2>/dev/null | grep -q "|$col|"; then
          _cols_missing=$((_cols_missing + 1))
        fi
      done
      if [ "$_cols_missing" -eq 0 ]; then
        add_step "schema_check" "pass" "instances table present with all required columns"
      else
        add_step "schema_check" "fail" "instances table missing $_cols_missing required column(s)"
      fi
    else
      add_step "schema_check" "fail" "instances table missing"
    fi
    # Determine overall compatibility.
    if [ "$_integrity" = "ok" ] && [ "$_schema" = "instances" ] && [ "$_cols_missing" -eq 0 ]; then
      DB_COMPATIBLE=true
    fi
  else
    add_step "integrity_check" "skip" "DB or sqlite3 unavailable"
    add_step "schema_check" "skip" "DB or sqlite3 unavailable"
  fi
fi

# Step 6: DB strategy — use current DB if compatible, or restore from backup.
if [ "$exit_code" -eq 0 ]; then
  if [ "$DB_COMPATIBLE" = true ]; then
    add_step "db_strategy" "pass" "using current DB (compatible)"
    DB_STRATEGY="use_current"
  else
    DB_STRATEGY="restore_backup"
    _script_dir=$(dirname "$0" 2>/dev/null)
    _backup_script="$_script_dir/backup.sh"
    _restore_ok=false
    if [ -f "$_backup_script" ]; then
      # Verify the backup manifest.
      _verify_out=$(sh "$_backup_script" --verify --data-dir "$DATA_DIR" --backup-dir "$BACKUP_DIR" 2>/dev/null)
      _verify_exit=$?
      if [ "$_verify_exit" -eq 0 ]; then
        # Copy the backup DB file to the data dir.
        _backup_db="$BACKUP_DIR/gowa.db"
        if [ -f "$_backup_db" ]; then
          if cp "$_backup_db" "$DATA_DIR/gowa.db" 2>/dev/null; then
            # Verify the restored DB passes integrity check.
            if [ -f "$DATA_DIR/gowa.db" ] && command -v "$SQLITE_BIN" >/dev/null 2>&1; then
              _restored_integrity=$("$SQLITE_BIN" "$DATA_DIR/gowa.db" "PRAGMA integrity_check;" 2>/dev/null | head -1)
              if [ "$_restored_integrity" = "ok" ]; then
                add_step "db_strategy" "pass" "restored DB from backup (verified)"
                _restore_ok=true
              else
                add_step "db_strategy" "fail" "restored DB but integrity check failed: $_restored_integrity"
                add_error "DB restore failed: restored DB integrity check: $_restored_integrity"
                exit_code=1
              fi
            else
              add_step "db_strategy" "pass" "restored DB from backup (sqlite3 unavailable for verification)"
              _restore_ok=true
            fi
          else
            add_step "db_strategy" "fail" "failed to copy backup DB to data dir"
            add_error "DB restore failed: cannot copy $BACKUP_DIR/gowa.db to $DATA_DIR/gowa.db"
            exit_code=1
          fi
        else
          add_step "db_strategy" "fail" "backup DB file not found: $BACKUP_DIR/gowa.db"
          add_error "DB restore failed: backup DB file not found at $BACKUP_DIR/gowa.db"
          exit_code=1
        fi
      else
        add_step "db_strategy" "fail" "backup verify failed (exit $_verify_exit)"
        add_error "DB restore failed: backup verification failed with exit code $_verify_exit"
        exit_code=1
      fi
    else
      add_step "db_strategy" "fail" "backup.sh not found at $_backup_script"
      add_error "DB restore failed: backup.sh not found"
      exit_code=1
    fi
  fi
fi

# Step 7: Verify Bun binary checksum before starting.
if [ "$exit_code" -eq 0 ]; then
  if [ -f "$BUN_BINARY" ]; then
    _actual_sha=$(compute_sha "$BUN_BINARY")
    if [ "$_actual_sha" = "$BUN_CHECKSUM" ]; then
      add_step "verify_bun_checksum" "pass" "Bun binary checksum matches"
    else
      add_step "verify_bun_checksum" "fail" "checksum mismatch: expected $BUN_CHECKSUM got $_actual_sha"
      add_error "Bun binary checksum mismatch: expected $BUN_CHECKSUM got $_actual_sha"
      exit_code=1
    fi
  else
    add_step "verify_bun_checksum" "fail" "Bun binary not found: $BUN_BINARY"
    add_error "Bun binary not found: $BUN_BINARY"
    exit_code=1
  fi
fi

# Step 8: Start the pinned Bun command.
if [ "$exit_code" -eq 0 ]; then
  if [ -x "$BUN_BINARY" ]; then
    # Start Bun in the background.  In a real deployment the operator would
    # use a process manager; here we just launch it and record the step.
    # We do NOT wait for it to exit.  Detach stdio from the caller's pipes
    # so a caller capturing our stdout (e.g. the ops test harness) is not
    # blocked waiting for EOF while this long-lived process holds it open.
    "$BUN_BINARY" >/dev/null 2>&1 </dev/null &
    _bun_pid=$!
    add_step "start_bun" "pass" "started Bun binary (PID $_bun_pid)"
  else
    add_step "start_bun" "fail" "Bun binary not executable: $BUN_BINARY"
    add_error "Bun binary not executable: $BUN_BINARY"
    exit_code=1
  fi
fi

# Step 9: Run Bun smoke tests (call smoke.sh against the Bun URL).
if [ "$exit_code" -eq 0 ]; then
  _script_dir=$(dirname "$0" 2>/dev/null)
  _smoke_script="$_script_dir/smoke.sh"
  if [ -f "$_smoke_script" ]; then
    # Give Bun a moment to bind.
    sleep 1
    _smoke_out=$(sh "$_smoke_script" --url "$BUN_URL" 2>/dev/null)
    _smoke_exit=$?
    if [ "$_smoke_exit" -eq 0 ]; then
      add_step "bun_smoke" "pass" "smoke.sh against $BUN_URL passed"
    else
      add_step "bun_smoke" "fail" "smoke.sh exited $_smoke_exit"
      add_error "Bun smoke tests failed with exit code $_smoke_exit"
      exit_code=1
    fi
  else
    add_step "bun_smoke" "skip" "smoke.sh not found at $_smoke_script"
    add_warning "smoke.sh not found; skipped Bun smoke tests"
  fi
fi

# ---------------------------------------------------------------------------
# Emit JSON
# ---------------------------------------------------------------------------
END_TS=$(now_iso)

cat <<EOF
{
  "tool": "rollback",
  "schema_version": 1,
  "mode": "execute",
  "start_timestamp": $(jstr "$START_TS"),
  "end_timestamp": $(jstr "$END_TS"),
  "execute": true,
  "go_pid": $(jstr "$GO_PID"),
  "go_version": $(jstr "$GO_VERSION"),
  "bun_binary": $(jstr "$BUN_BINARY"),
  "bun_checksum_verified": $( [ "$exit_code" -eq 0 ] && echo true || echo false ),
  "data_dir": $(jstr "$DATA_DIR"),
  "backup_dir": $(jstr "$BACKUP_DIR"),
  "bun_url": $(jstr "$BUN_URL"),
  "override_ambiguous_state": $(jbool "$OVERRIDE_AMBIGUOUS"),
  "go_pid_verified": $(jbool "$GO_PID_VERIFIED"),
  "db_strategy": $(jstr "$DB_STRATEGY"),
  "steps": [$STEPS],
  "errors": [$ERRORS],
  "warnings": [$WARNINGS],
  "exit_code": $exit_code
}
EOF

# ---------------------------------------------------------------------------
# Human summary to stderr
# ---------------------------------------------------------------------------
log ""
log "=== GOWA Manager Rollback ==="
log "Mode:         execute"
log "Go PID:       $GO_PID"
log "Go version:   $GO_VERSION"
log "Bun binary:   $BUN_BINARY"
log "Data dir:     $DATA_DIR"
log "Backup dir:   $BACKUP_DIR"
log "Bun URL:      $BUN_URL"
log "Start:        $START_TS"
log "End:          $END_TS"
if [ -n "$WARNINGS" ]; then
  log ""
  log "Warnings:"
  echo "$WARNINGS" | tr ',' '\n' | sed 's/"//g' | while read -r w; do
    [ -n "$w" ] && log "  - $w"
  done
fi
if [ -n "$ERRORS" ]; then
  log ""
  log "Errors:"
  echo "$ERRORS" | tr ',' '\n' | sed 's/"//g' | while read -r e; do
    [ -n "$e" ] && log "  - $e"
  done
fi
log ""
if [ "$exit_code" -eq 0 ]; then
  log "Result: ROLLBACK OK — Go stopped, Bun started"
else
  log "Result: ROLLBACK FAILED — see errors above"
fi

exit $exit_code
