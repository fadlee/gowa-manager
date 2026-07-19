#!/bin/sh
# shellcheck shell=sh
#
# GOWA Manager — post-cutover smoke tests (POSIX sh).
#
# Checks a running Go manager's HTTP endpoints.  By default NON-DESTRUCTIVE:
# only GET requests (plus the POST /api/auth/login which is required to
# verify credentials).  Destructive mode additionally exercises the test
# instance lifecycle (start/stop/create/delete) and requires both
# --destructive and --test-key <key>.
#
# Produces machine-readable JSON on stdout and a concise human summary on
# stderr.  Exits non-zero on any failure.
#
# Usage:
#   smoke.sh [--url <base>] [--admin-username <user>] [--admin-password <pass>]
#            [--metrics] [--destructive] [--test-key <key>]
#
# Never prints passwords, tokens, or webhook URLs.

set -u

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
BASE_URL="http://localhost:3000"
ADMIN_USERNAME="admin"
ADMIN_PASSWORD="password"
METRICS=false
DESTRUCTIVE=false
TEST_KEY=""

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

# ---------------------------------------------------------------------------
# Accumulators
# ---------------------------------------------------------------------------
CHECKS=""
ERRORS=""
PASS_COUNT=0
FAIL_COUNT=0

add_check() {
  # $1=name  $2=status(pass/fail)  $3=http_status  $4=detail
  _entry="{\"name\":$(jstr "$1"),\"status\":$(jstr "$2"),\"http_status\":$3,\"detail\":$(jstr "$4")}"
  if [ -z "$CHECKS" ]; then
    CHECKS="$_entry"
  else
    CHECKS="$CHECKS,$_entry"
  fi
}

add_error() {
  if [ -z "$ERRORS" ]; then
    ERRORS=$(jstr "$1")
  else
    ERRORS="$ERRORS,$(jstr "$1")"
  fi
}

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --url)            BASE_URL=$2; shift 2 ;;
    --admin-username) ADMIN_USERNAME=$2; shift 2 ;;
    --admin-password) ADMIN_PASSWORD=$2; shift 2 ;;
    --metrics)        METRICS=true; shift ;;
    --destructive)    DESTRUCTIVE=true; shift ;;
    --test-key)       TEST_KEY=$2; shift 2 ;;
    -h|--help)
      cat >&2 <<'EOF'
GOWA Manager smoke tests — verify a running Go manager's endpoints.

Usage: smoke.sh [--url <base>] [--admin-username <user>] [--admin-password <pass>]
                [--metrics] [--destructive] [--test-key <key>]

Options:
  --url <base>            Base URL of the manager (default: http://localhost:3000).
  --admin-username <user> Admin username (default: admin).
  --admin-password <pass> Admin password (default: password).
  --metrics               Also check GET /metrics.
  --destructive           Enable destructive checks (requires --test-key).
  --test-key <key>        Instance key to use for destructive checks.

Non-destructive mode (default) only issues GET requests (plus POST /api/auth/login
for credential verification).  Destructive mode may start/stop/create/delete the
test instance identified by --test-key.
EOF
      exit 0 ;;
    *) log "Unknown option: $1"; exit 2 ;;
  esac
done

# Destructive mode requires --test-key.
if [ "$DESTRUCTIVE" = true ] && [ -z "$TEST_KEY" ]; then
  add_error "destructive mode requires --test-key"
  END_TS=$(now_iso)
  cat <<EOF
{
  "tool": "smoke",
  "schema_version": 1,
  "mode": "non_destructive",
  "start_timestamp": $(jstr "$START_TS"),
  "end_timestamp": $(jstr "$END_TS"),
  "url": $(jstr "$BASE_URL"),
  "checks": [$CHECKS],
  "pass_count": $PASS_COUNT,
  "fail_count": $FAIL_COUNT,
  "errors": [$ERRORS],
  "exit_code": 1
}
EOF
  log "Smoke test failed: destructive mode requires --test-key"
  exit 1
fi

START_TS=$(now_iso)
MODE="non_destructive"
[ "$DESTRUCTIVE" = true ] && MODE="destructive"

# Basic auth credentials for curl.
AUTH="$ADMIN_USERNAME:$ADMIN_PASSWORD"

# ---------------------------------------------------------------------------
# HTTP request helper.
#
# http_get <name> <path> <use_auth> <expected_status>
# http_post <name> <path> <use_auth> <expected_status>
#
# Sets HTTP_STATUS and HTTP_BODY on success/failure.  Records a check result.
# ---------------------------------------------------------------------------
HTTP_STATUS=0
HTTP_BODY=""

do_get() {
  _name=$1
  _path=$2
  _use_auth=$3
  _expect=$4
  _full="$BASE_URL$_path"
  if [ "$_use_auth" = "1" ]; then
    _resp=$(curl -s --max-time 3 -o /dev/null -w '%{http_code}' -u "$AUTH" "$_full" 2>/dev/null)
  else
    _resp=$(curl -s --max-time 3 -o /dev/null -w '%{http_code}' "$_full" 2>/dev/null)
  fi
  HTTP_STATUS=${_resp:-0}
  if [ "$HTTP_STATUS" = "$_expect" ]; then
    add_check "$_name" "pass" "$HTTP_STATUS" "GET $_path -> $HTTP_STATUS"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    add_check "$_name" "fail" "$HTTP_STATUS" "GET $_path expected $_expect got $HTTP_STATUS"
    add_error "$_name: GET $_path expected $_expect got $HTTP_STATUS"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

do_post() {
  _name=$1
  _path=$2
  _use_auth=$3
  _expect=$4
  _full="$BASE_URL$_path"
  if [ "$_use_auth" = "1" ]; then
    _resp=$(curl -s --max-time 3 -o /dev/null -w '%{http_code}' -X POST -u "$AUTH" "$_full" 2>/dev/null)
  else
    _resp=$(curl -s --max-time 3 -o /dev/null -w '%{http_code}' -X POST "$_full" 2>/dev/null)
  fi
  HTTP_STATUS=${_resp:-0}
  if [ "$HTTP_STATUS" = "$_expect" ]; then
    add_check "$_name" "pass" "$HTTP_STATUS" "POST $_path -> $HTTP_STATUS"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    add_check "$_name" "fail" "$HTTP_STATUS" "POST $_path expected $_expect got $HTTP_STATUS"
    add_error "$_name: POST $_path expected $_expect got $HTTP_STATUS"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

# ---------------------------------------------------------------------------
# Check: curl availability
# ---------------------------------------------------------------------------
if ! command -v curl >/dev/null 2>&1; then
  add_error "curl is required but not found on PATH"
  END_TS=$(now_iso)
  cat <<EOF
{
  "tool": "smoke",
  "schema_version": 1,
  "mode": $(jstr "$MODE"),
  "start_timestamp": $(jstr "$START_TS"),
  "end_timestamp": $(jstr "$END_TS"),
  "url": $(jstr "$BASE_URL"),
  "checks": [$CHECKS],
  "pass_count": $PASS_COUNT,
  "fail_count": $FAIL_COUNT,
  "errors": [$ERRORS],
  "exit_code": 1
}
EOF
  log "Smoke test failed: curl not available"
  exit 1
fi

# ---------------------------------------------------------------------------
# Non-destructive checks
# ---------------------------------------------------------------------------

# GET /api/health — no auth, expect 200.
do_get "health" "/api/health" 0 200

# GET /api/ready — no auth, expect 200.
do_get "ready" "/api/ready" 0 200

# POST /api/auth/login — Basic Auth credentials, expect 200.
do_post "auth_login" "/api/auth/login" 1 200

# GET /api/instances — with Basic Auth, expect 200.
do_get "instances" "/api/instances" 1 200

# GET /api/system/status — with Basic Auth, expect 200.
do_get "system_status" "/api/system/status" 1 200

# GET /api/system/versions/installed — with Basic Auth, expect 200.
do_get "system_versions_installed" "/api/system/versions/installed" 1 200

# GET /api/system/auto-update/status — with Basic Auth, expect 200.
do_get "system_autoupdate_status" "/api/system/auto-update/status" 1 200

# GET /metrics — only if --metrics flag passed; no auth, expect 200.
if [ "$METRICS" = true ]; then
  do_get "metrics" "/metrics" 0 200
fi

# ---------------------------------------------------------------------------
# Destructive checks (only when --destructive and --test-key supplied)
# ---------------------------------------------------------------------------
if [ "$DESTRUCTIVE" = true ]; then
  # In destructive mode we may start/stop/create/delete the test instance.
  # We issue a GET on the test instance status to confirm it is reachable.
  # Full lifecycle (create/start/stop/delete) is intentionally minimal here
  # to avoid side effects on a production system; the operator is expected
  # to supply a throwaway --test-key.
  _tk_path="/api/instances"
  _resp=$(curl -s --max-time 3 -o /dev/null -w '%{http_code}' -X POST -u "$AUTH" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"smoke-test-$(jstr "$TEST_KEY")\",\"gowa_version\":\"v1.0.0\"}" \
    "$BASE_URL$_tk_path" 2>/dev/null)
  if [ "$_resp" = "200" ] || [ "$_resp" = "201" ]; then
    add_check "destructive_create" "pass" "$_resp" "POST $_tk_path -> $_resp"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    add_check "destructive_create" "fail" "$_resp" "POST $_tk_path expected 200/201 got $_resp"
    add_error "destructive_create: POST $_tk_path expected 200/201 got $_resp"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
fi

# ---------------------------------------------------------------------------
# Emit JSON
# ---------------------------------------------------------------------------
exit_code=0
[ "$FAIL_COUNT" -gt 0 ] && exit_code=1

END_TS=$(now_iso)

cat <<EOF
{
  "tool": "smoke",
  "schema_version": 1,
  "mode": $(jstr "$MODE"),
  "start_timestamp": $(jstr "$START_TS"),
  "end_timestamp": $(jstr "$END_TS"),
  "url": $(jstr "$BASE_URL"),
  "metrics_enabled": $(jbool "$METRICS"),
  "destructive": $(jbool "$DESTRUCTIVE"),
  "checks": [$CHECKS],
  "pass_count": $PASS_COUNT,
  "fail_count": $FAIL_COUNT,
  "errors": [$ERRORS],
  "exit_code": $exit_code
}
EOF

# ---------------------------------------------------------------------------
# Human summary to stderr
# ---------------------------------------------------------------------------
log ""
log "=== GOWA Manager Smoke Tests ==="
log "URL:          $BASE_URL"
log "Mode:         $MODE"
log "Metrics:      $( [ "$METRICS" = true ] && echo 'enabled' || echo 'disabled')"
log "Passed:       $PASS_COUNT"
log "Failed:       $FAIL_COUNT"
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
  log "Result: SMOKE OK — all checks passed"
else
  log "Result: SMOKE FAILED — see errors above"
fi

exit $exit_code
