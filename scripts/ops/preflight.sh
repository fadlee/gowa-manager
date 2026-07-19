#!/bin/sh
# shellcheck shell=sh
#
# GOWA Manager — Go cutover preflight checks (POSIX sh).
#
# Verifies that the environment is safe for switching from the Bun backend
# to the Go backend.  Produces machine-readable JSON on stdout and a concise
# human summary on stderr.  Exits non-zero when any blocker is found.
#
# Usage:
#   preflight.sh [-b|--binary PATH] [-d|--data-dir DIR]
#                [-p|--port N] [--backup-dir DIR]
#                [--sqlite-bin PATH]
#
# Secrets (passwords, instance config, webhook URLs) are NEVER printed.
# Only structural metadata (paths, sizes, counts, status strings) is emitted.

set -u

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
BINARY=""
DATA_DIR="./data"
PORT=3000
BACKUP_DIR="./backup"
SQLITE_BIN="${SQLITE_BIN:-sqlite3}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# Escape a string for safe inclusion inside a JSON double-quoted string.
jstr() {
  _v=$1
  # Escape backslashes first, then double quotes.
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

# Append a check result to the CHECKS variable (JSON array fragment).
CHECKS=""
add_check() {
  # $1=name  $2=status(pass/fail/warn)  $3=detail
  _entry="{\"name\":$(jstr "$1"),\"status\":$(jstr "$2"),\"detail\":$(jstr "$3")}"
  if [ -z "$CHECKS" ]; then
    CHECKS="$_entry"
  else
    CHECKS="$CHECKS,$_entry"
  fi
}

BLOCKERS=""
WARNINGS=""
add_blocker() {
  if [ -z "$BLOCKERS" ]; then
    BLOCKERS=$(jstr "$1")
  else
    BLOCKERS="$BLOCKERS,$(jstr "$1")"
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
    -b|--binary)    BINARY=$2; shift 2 ;;
    -d|--data-dir)  DATA_DIR=$2; shift 2 ;;
    -p|--port)      PORT=$2; shift 2 ;;
    --backup-dir)   BACKUP_DIR=$2; shift 2 ;;
    --sqlite-bin)   SQLITE_BIN=$2; shift 2 ;;
    -h|--help)
      cat >&2 <<'EOF'
GOWA Manager preflight — verify environment before Go cutover.

Usage: preflight.sh [-b|--binary PATH] [-d|--data-dir DIR]
                    [-p|--port N] [--backup-dir DIR] [--sqlite-bin PATH]

Options:
  -b, --binary PATH      Path to the Go manager binary to verify.
  -d, --data-dir DIR     Data directory (default: ./data).
  -p, --port N           Manager HTTP port (default: 3000).
  --backup-dir DIR       Backup destination directory (default: ./backup).
  --sqlite-bin PATH      Path to sqlite3 CLI (default: sqlite3 from PATH).
EOF
      exit 0 ;;
    *) log "Unknown option: $1"; exit 2 ;;
  esac
done

# ---------------------------------------------------------------------------
# Check 1: OS / architecture
# ---------------------------------------------------------------------------
OS_NAME=$(uname -s 2>/dev/null || echo unknown)
ARCH_NAME=$(uname -m 2>/dev/null || echo unknown)
case "$OS_NAME" in
  Linux)  os_ok=1 ;;
  Darwin) os_ok=1 ;;
  *)      os_ok=0 ;;
esac
case "$ARCH_NAME" in
  x86_64|amd64)   arch_ok=1 ;;
  arm64|aarch64)  arch_ok=1 ;;
  *)              arch_ok=0 ;;
esac
if [ "$os_ok" = 1 ] && [ "$arch_ok" = 1 ]; then
  add_check "os_arch" "pass" "$OS_NAME/$ARCH_NAME"
else
  add_check "os_arch" "fail" "$OS_NAME/$ARCH_NAME not supported"
  add_blocker "unsupported OS/arch: $OS_NAME/$ARCH_NAME"
fi

# ---------------------------------------------------------------------------
# Check 2: Binary exists / is executable / reports version
# ---------------------------------------------------------------------------
bin_exists=false
bin_exec=false
bin_version=""
if [ -n "$BINARY" ]; then
  if [ -f "$BINARY" ]; then
    bin_exists=true
    if [ -x "$BINARY" ]; then
      bin_exec=true
      bin_version=$("$BINARY" --version 2>/dev/null | head -1 || echo "")
    fi
  fi
else
  bin_exists=false
fi
if [ "$bin_exists" = true ] && [ "$bin_exec" = true ] && [ -n "$bin_version" ]; then
  add_check "binary" "pass" "$bin_version"
else
  _detail="binary missing or not executable"
  [ -n "$BINARY" ] || _detail="no binary path provided"
  add_check "binary" "fail" "$_detail"
  add_blocker "manager binary not usable"
fi

# ---------------------------------------------------------------------------
# Check 3: Data directory exists and free space
# ---------------------------------------------------------------------------
dd_exists=false
dd_free_bytes=0
if [ -d "$DATA_DIR" ]; then
  dd_exists=true
  # df output: filesystem blocks used available capacity mounted-on
  dd_free_bytes=$(df -P "$DATA_DIR" 2>/dev/null | awk 'NR==2{print $4}')
  [ -n "$dd_free_bytes" ] || dd_free_bytes=0
  # df reports in 512-byte blocks on some systems; convert with -P (1024-byte)
  # -P guarantees POSIX 512-byte blocks on some, 1K on others.  Multiply by 1024
  # when the value looks like 1K-blocks (most Linux df -P uses 1K-blocks).
  dd_free_bytes=$((dd_free_bytes * 1024))
fi
if [ "$dd_exists" = true ]; then
  if [ "$dd_free_bytes" -lt 10485760 ]; then
    add_check "data_dir_space" "fail" "only ${dd_free_bytes} bytes free (< 10 MiB)"
    add_blocker "insufficient free space in data dir"
  else
    add_check "data_dir_space" "pass" "${dd_free_bytes} bytes free"
  fi
else
  add_check "data_dir_space" "fail" "data dir does not exist"
  add_blocker "data directory not found"
fi

# ---------------------------------------------------------------------------
# Check 4: Read / write / execute permissions on data dir
# ---------------------------------------------------------------------------
perm_read=false
perm_write=false
perm_execute=false
if [ "$dd_exists" = true ]; then
  [ -r "$DATA_DIR" ] && perm_read=true
  [ -w "$DATA_DIR" ] && perm_write=true
  [ -x "$DATA_DIR" ] && perm_execute=true
fi
if [ "$perm_read" = true ] && [ "$perm_write" = true ] && [ "$perm_execute" = true ]; then
  add_check "permissions" "pass" "rwx ok"
else
  add_check "permissions" "fail" "r=$(jstr "$perm_read") w=$(jstr "$perm_write") x=$(jstr "$perm_execute")"
  add_blocker "data dir lacks required rwx permissions"
fi

# ---------------------------------------------------------------------------
# Check 5: Absence of active Bun / Go manager process
# ---------------------------------------------------------------------------
mgr_bun=false
mgr_go=false
if command -v pgrep >/dev/null 2>&1; then
  pgrep -f 'gowa-manager' >/dev/null 2>&1 && mgr_go=true
  pgrep -f 'bun.*src/index' >/dev/null 2>&1 && mgr_bun=true
elif command -v ps >/dev/null 2>&1; then
  ps aux 2>/dev/null | grep -v grep | grep -q 'gowa-manager' && mgr_go=true
  ps aux 2>/dev/null | grep -v grep | grep -q 'bun.*src/index' && mgr_bun=true
fi
if [ "$mgr_bun" = true ] || [ "$mgr_go" = true ]; then
  _d=""
  [ "$mgr_bun" = true ] && _d="bun active"
  [ "$mgr_go" = true ] && _d="$_d go active"
  add_check "manager_active" "fail" "$_d"
  add_blocker "a manager process is still running"
else
  add_check "manager_active" "pass" "no manager process detected"
fi

# ---------------------------------------------------------------------------
# Check 6: Manager lock not held
# ---------------------------------------------------------------------------
LOCK_PATH="$DATA_DIR/.gowa-manager.lock"
lock_held=false
if [ -f "$LOCK_PATH" ]; then
  # Try to acquire the lock non-blocking with flock if available.
  if command -v flock >/dev/null 2>&1; then
    if flock -n "$LOCK_PATH" true 2>/dev/null; then
      lock_held=false
    else
      lock_held=true
    fi
  else
    # Without flock we cannot reliably tell; treat existence as a warning.
    lock_held=true
  fi
fi
if [ "$lock_held" = true ]; then
  add_check "lock" "fail" "lock file held: $LOCK_PATH"
  add_blocker "manager lock is held by another process"
else
  add_check "lock" "pass" "lock not held"
fi

# ---------------------------------------------------------------------------
# Check 7: HTTP port available
# ---------------------------------------------------------------------------
port_available=true
# Try connecting — if something answers, the port is occupied.
if (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null; then
  port_available=false
  exec 3>&- 3<&- 2>/dev/null || true
else
  # /dev/tcp is a bash-ism; fall back to nc if /dev/tcp is unavailable.
  if command -v nc >/dev/null 2>&1; then
    if nc -z 127.0.0.1 "$PORT" 2>/dev/null; then
      port_available=false
    fi
  fi
fi
if [ "$port_available" = true ]; then
  add_check "port" "pass" "port $PORT available"
else
  add_check "port" "fail" "port $PORT occupied"
  add_blocker "HTTP port $PORT is occupied"
fi

# ---------------------------------------------------------------------------
# Check 8 & 9: SQLite integrity and required columns
# ---------------------------------------------------------------------------
DB_PATH="$DATA_DIR/gowa.db"
sqlite_exists=false
sqlite_integrity="skipped"
sqlite_journal="unknown"
columns_present=""
columns_missing=""

sqlite3_available=true
command -v "$SQLITE_BIN" >/dev/null 2>&1 || sqlite3_available=false

if [ -f "$DB_PATH" ]; then
  sqlite_exists=true
fi

if [ "$sqlite_exists" = true ] && [ "$sqlite3_available" = true ]; then
  sqlite_integrity=$("$SQLITE_BIN" "$DB_PATH" "PRAGMA integrity_check;" 2>/dev/null | head -1)
  [ -n "$sqlite_integrity" ] || sqlite_integrity="error"
  sqlite_journal=$("$SQLITE_BIN" "$DB_PATH" "PRAGMA journal_mode;" 2>/dev/null | head -1)
  [ -n "$sqlite_journal" ] || sqlite_journal="unknown"

  if [ "$sqlite_integrity" = "ok" ]; then
    add_check "sqlite_integrity" "pass" "ok"
  else
    add_check "sqlite_integrity" "fail" "$sqlite_integrity"
    add_blocker "SQLite integrity check failed"
  fi

  # Required columns per internal/database/schema.go
  required_cols="id key name port status config gowa_version created_at updated_at error_message"
  for col in $required_cols; do
    if "$SQLITE_BIN" "$DB_PATH" "PRAGMA table_info(instances);" 2>/dev/null | grep -q "|$col|"; then
      if [ -z "$columns_present" ]; then
        columns_present=$(jstr "$col")
      else
        columns_present="$columns_present,$(jstr "$col")"
      fi
    else
      if [ -z "$columns_missing" ]; then
        columns_missing=$(jstr "$col")
      else
        columns_missing="$columns_missing,$(jstr "$col")"
      fi
    fi
  done
  if [ -n "$columns_missing" ]; then
    add_check "columns" "fail" "missing columns"
    add_blocker "instances table missing required columns"
  else
    add_check "columns" "pass" "all required columns present"
  fi
elif [ "$sqlite_exists" = true ] && [ "$sqlite3_available" = false ]; then
  add_check "sqlite_integrity" "warn" "sqlite3 CLI not found — cannot verify"
  add_warning "sqlite3 CLI not available; integrity and column checks skipped"
else
  add_check "sqlite_integrity" "warn" "database file does not exist yet"
  add_warning "no existing database — fresh install"
fi

# ---------------------------------------------------------------------------
# Check 10: Installed GOWA binaries and execute permission
# ---------------------------------------------------------------------------
GOWA_BINARIES=""
versions_dir="$DATA_DIR/bin/versions"
if [ -d "$versions_dir" ]; then
  for vdir in "$versions_dir"/*/; do
    [ -d "$vdir" ] || continue
    vname=$(basename "$vdir")
    case "$vname" in
      .install-*) continue ;;
    esac
    gowa_path="$vdir/gowa"
    gowa_exists=false
    gowa_exec=false
    if [ -f "$gowa_path" ]; then
      gowa_exists=true
      [ -x "$gowa_path" ] && gowa_exec=true
    fi
    _entry="{\"version\":$(jstr "$vname"),\"path\":$(jstr "$gowa_path"),\"exists\":$(jbool "$gowa_exists"),\"executable\":$(jbool "$gowa_exec")}"
    if [ -z "$GOWA_BINARIES" ]; then
      GOWA_BINARIES="$_entry"
    else
      GOWA_BINARIES="$GOWA_BINARIES,$_entry"
    fi
    if [ "$gowa_exists" = false ]; then
      add_check "gowa_binary_$vname" "fail" "binary missing for $vname"
      add_blocker "GOWA binary missing for version $vname"
    elif [ "$gowa_exec" = false ]; then
      add_check "gowa_binary_$vname" "fail" "not executable: $vname"
      add_blocker "GOWA binary not executable for version $vname"
    fi
  done
fi
if [ -z "$GOWA_BINARIES" ]; then
  add_check "gowa_binaries" "warn" "no installed GOWA versions found"
  add_warning "no GOWA binaries installed under $versions_dir"
else
  add_check "gowa_binaries" "pass" "installed versions verified"
fi

# ---------------------------------------------------------------------------
# Check 11: Backup destination writable
# ---------------------------------------------------------------------------
bk_exists=false
bk_writable=false
if [ -d "$BACKUP_DIR" ]; then
  bk_exists=true
  if [ -w "$BACKUP_DIR" ]; then
    bk_writable=true
  fi
else
  # Try to create it.
  if mkdir -p "$BACKUP_DIR" 2>/dev/null; then
    bk_exists=true
    bk_writable=true
  fi
fi
if [ "$bk_writable" = true ]; then
  add_check "backup_destination" "pass" "writable"
else
  add_check "backup_destination" "fail" "not writable: $BACKUP_DIR"
  add_blocker "backup destination not writable"
fi

# ---------------------------------------------------------------------------
# Check 12: Child-process / port inventory (running instances from DB)
# ---------------------------------------------------------------------------
CHILD_PROCESSES=""
if [ "$sqlite_exists" = true ] && [ "$sqlite3_available" = true ]; then
  # Query running instances — never select config (may contain tokens).
  _query="SELECT key,name,port,status FROM instances WHERE status='running';"
  _rows=$("$SQLITE_BIN" "$DB_PATH" "$_query" 2>/dev/null || true)
  if [ -n "$_rows" ]; then
    # Parse pipe-separated rows: key|name|port|status
    # The while-loop runs in a subshell; capture via command substitution.
    CHILD_PROCESSES=$(echo "$_rows" | while IFS='|' read -r _k _n _p _s; do
      [ -n "$_k" ] || continue
      printf '{"key":%s,"name":%s,"port":%s,"status":%s}\n' \
        "$(jstr "$_k")" "$(jstr "$_n")" "${_p:-0}" "$(jstr "$_s")"
    done | paste -sd, -)
  fi
fi
if [ -n "$CHILD_PROCESSES" ]; then
  _count=$(echo "$CHILD_PROCESSES" | tr ',' '\n' | wc -l | tr -d ' ')
  add_check "child_processes" "warn" "$_count running instance(s) — stop before cutover"
  add_warning "$_count running instance(s) detected — must be stopped before cutover"
else
  add_check "child_processes" "pass" "no running instances"
fi

# ---------------------------------------------------------------------------
# Emit JSON
# ---------------------------------------------------------------------------
BLOCKERS_JSON="[$BLOCKERS]"
WARNINGS_JSON="[$WARNINGS]"
CHECKS_JSON="[$CHECKS]"
GOWA_BINARIES_JSON="[$GOWA_BINARIES]"
CHILD_PROCESSES_JSON="[$CHILD_PROCESSES]"
COLUMNS_PRESENT_JSON="[$columns_present]"
COLUMNS_MISSING_JSON="[$columns_missing]"

# Determine exit code
exit_code=0
[ -n "$BLOCKERS" ] && exit_code=1

cat <<EOF
{
  "tool": "preflight",
  "schema_version": 1,
  "timestamp": $(jstr "$(now_iso)"),
  "os": $(jstr "$OS_NAME"),
  "arch": $(jstr "$ARCH_NAME"),
  "binary": {
    "path": $(jstr "$BINARY"),
    "exists": $(jbool "$bin_exists"),
    "executable": $(jbool "$bin_exec"),
    "version": $(jstr "$bin_version")
  },
  "data_dir": {
    "path": $(jstr "$DATA_DIR"),
    "exists": $(jbool "$dd_exists"),
    "free_bytes": $dd_free_bytes
  },
  "permissions": {
    "read": $(jbool "$perm_read"),
    "write": $(jbool "$perm_write"),
    "execute": $(jbool "$perm_execute")
  },
  "manager_active": {
    "bun": $(jbool "$mgr_bun"),
    "go": $(jbool "$mgr_go")
  },
  "lock": {
    "path": $(jstr "$LOCK_PATH"),
    "held": $(jbool "$lock_held")
  },
  "port": {
    "number": $PORT,
    "available": $(jbool "$port_available")
  },
  "sqlite": {
    "path": $(jstr "$DB_PATH"),
    "exists": $(jbool "$sqlite_exists"),
    "integrity": $(jstr "$sqlite_integrity"),
    "journal_mode": $(jstr "$sqlite_journal")
  },
  "columns": {
    "present": $COLUMNS_PRESENT_JSON,
    "missing": $COLUMNS_MISSING_JSON
  },
  "gowa_binaries": $GOWA_BINARIES_JSON,
  "backup_destination": {
    "path": $(jstr "$BACKUP_DIR"),
    "exists": $(jbool "$bk_exists"),
    "writable": $(jbool "$bk_writable")
  },
  "child_processes": $CHILD_PROCESSES_JSON,
  "checks": $CHECKS_JSON,
  "blockers": $BLOCKERS_JSON,
  "warnings": $WARNINGS_JSON,
  "exit_code": $exit_code
}
EOF

# ---------------------------------------------------------------------------
# Human summary to stderr
# ---------------------------------------------------------------------------
log ""
log "=== GOWA Manager Preflight ==="
log "OS/Arch:      $OS_NAME/$ARCH_NAME"
log "Binary:       $BINARY ($bin_version)"
log "Data dir:     $DATA_DIR ($dd_free_bytes bytes free)"
log "Port:         $PORT ($( [ "$port_available" = true ] && echo 'available' || echo 'occupied'))"
log "SQLite:       $sqlite_integrity (journal: $sqlite_journal)"
log "Lock:         $( [ "$lock_held" = true ] && echo 'HELD' || echo 'free')"
log "Backup dest:  $BACKUP_DIR ($( [ "$bk_writable" = true ] && echo 'writable' || echo 'NOT writable'))"
if [ -n "$BLOCKERS" ]; then
  log ""
  log "BLOCKERS:"
  echo "$BLOCKERS" | tr ',' '\n' | sed 's/"//g' | while read -r b; do
    [ -n "$b" ] && log "  - $b"
  done
fi
if [ -n "$WARNINGS" ]; then
  log ""
  log "Warnings:"
  echo "$WARNINGS" | tr ',' '\n' | sed 's/"//g' | while read -r w; do
    [ -n "$w" ] && log "  - $w"
  done
fi
log ""
if [ "$exit_code" -eq 0 ]; then
  log "Result: PASS — environment ready for cutover"
else
  log "Result: FAIL — resolve blockers before cutover"
fi

exit $exit_code
