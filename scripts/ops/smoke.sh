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
  # Normalize the HTTP status to a bare JSON number. curl reports "000" on
  # connection failure, and a leading-zero literal like 000 is not valid
  # JSON. HTTP status codes are always three digits with no leading zero,
  # so empty/non-numeric/leading-zero values all collapse to 0. Uses a
  # POSIX case (no bashisms / arithmetic) so it is dash-safe under /bin/sh.
  _hs=$3
  case "$_hs" in
    ''|*[!0-9]*|0*) _hs=0 ;;
  esac
  _entry="{\"name\":$(jstr "$1"),\"status\":$(jstr "$2"),\"http_status\":$_hs,\"detail\":$(jstr "$4")}"
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

START_TS=$(now_iso)

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

# http_get_multi <name> <path> <use_auth> <expect1> [expect2 ...]
# Accepts multiple acceptable status codes (used for proxy endpoints that
# may return 200, 502, or 503 depending on whether the upstream is running).
do_get_multi() {
  _name=$1
  _path=$2
  _use_auth=$3
  shift 3
  _expects="$*"
  _full="$BASE_URL$_path"
  if [ "$_use_auth" = "1" ]; then
    _resp=$(curl -s --max-time 3 -o /dev/null -w '%{http_code}' -u "$AUTH" "$_full" 2>/dev/null)
  else
    _resp=$(curl -s --max-time 3 -o /dev/null -w '%{http_code}' "$_full" 2>/dev/null)
  fi
  HTTP_STATUS=${_resp:-0}
  _matched=0
  for _expect in $_expects; do
    if [ "$HTTP_STATUS" = "$_expect" ]; then
      _matched=1
      break
    fi
  done
  if [ "$_matched" = "1" ]; then
    add_check "$_name" "pass" "$HTTP_STATUS" "GET $_path -> $HTTP_STATUS"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    add_check "$_name" "fail" "$HTTP_STATUS" "GET $_path expected one of $_expects got $HTTP_STATUS"
    add_error "$_name: GET $_path expected one of $_expects got $HTTP_STATUS"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

# Fetch the response body (for JSON parsing).  Does not record a check.
# fetch_body <path> <use_auth>
fetch_body() {
  _fb_path=$1
  _fb_auth=$2
  _fb_full="$BASE_URL$_fb_path"
  if [ "$_fb_auth" = "1" ]; then
    curl -s --max-time 3 -u "$AUTH" "$_fb_full" 2>/dev/null
  else
    curl -s --max-time 3 "$_fb_full" 2>/dev/null
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

# Parse the instances list to get the first instance's id and key for
# the detail/status/proxy checks below.
FIRST_ID=""
FIRST_KEY=""
_instances_body=$(fetch_body "/api/instances" 1)
if command -v jq >/dev/null 2>&1; then
  _inst_count=$(printf '%s' "$_instances_body" | jq 'length' 2>/dev/null)
  if [ -n "$_inst_count" ] && [ "$_inst_count" -gt 0 ] 2>/dev/null; then
    FIRST_ID=$(printf '%s' "$_instances_body" | jq -r '.[0].id // empty' 2>/dev/null)
    FIRST_KEY=$(printf '%s' "$_instances_body" | jq -r '.[0].key // empty' 2>/dev/null)
  fi
else
  # Fallback: grep/sed to extract the first id and key.
  _inst_count=$(printf '%s' "$_instances_body" | grep -o '"id"' 2>/dev/null | wc -l | tr -d ' ')
  if [ -n "$_inst_count" ] && [ "$_inst_count" -gt 0 ] 2>/dev/null; then
    FIRST_ID=$(printf '%s' "$_instances_body" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' | head -1)
    FIRST_KEY=$(printf '%s' "$_instances_body" | sed -n 's/.*"key"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
  fi
fi

if [ -n "$FIRST_ID" ] && [ -n "$FIRST_KEY" ]; then
  # GET /api/instances/{id} — instance detail, with Basic Auth, expect 200.
  do_get "instance_detail" "/api/instances/$FIRST_ID" 1 200
  # GET /api/instances/{id}/status — instance status, with Basic Auth, expect 200.
  do_get "instance_status" "/api/instances/$FIRST_ID/status" 1 200
  # GET /app/{key}/status — proxy status, no auth.  Accept 200, 502, 503
  # (proxy responds even if the upstream instance is down).
  do_get_multi "proxy_status" "/app/$FIRST_KEY/status" 0 200 502 503
  # GET /app/{key}/health — proxy health, no auth.  Same multi-accept.
  do_get_multi "proxy_health" "/app/$FIRST_KEY/health" 0 200 502 503
else
  # No instances available — skip these checks with warnings (not failures).
  add_check "instance_detail" "skip" 0 "no instances available — skipped"
  add_check "instance_status" "skip" 0 "no instances available — skipped"
  add_check "proxy_status" "skip" 0 "no instances available — skipped"
  add_check "proxy_health" "skip" 0 "no instances available — skipped"
fi

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
  # Full lifecycle: create → start → status → stop → delete.
  # Each step is recorded.  If any step fails, we continue to the next
  # (best-effort cleanup).  The delete step is always attempted.

  # 1. Create: POST /api/instances with the test key.
  _create_path="/api/instances"
  _create_body="{\"name\":\"smoke-test-$TEST_KEY\",\"gowa_version\":\"v1.0.0\"}"
  _create_resp=$(curl -s --max-time 5 -w '\n%{http_code}' -X POST -u "$AUTH" \
    -H 'Content-Type: application/json' \
    -d "$_create_body" \
    "$BASE_URL$_create_path" 2>/dev/null)
  _create_status=$(printf '%s' "$_create_resp" | tail -1)
  _create_body_resp=$(printf '%s' "$_create_resp" | sed '$d')
  if [ "$_create_status" = "200" ] || [ "$_create_status" = "201" ]; then
    add_check "destructive_create" "pass" "$_create_status" "POST $_create_path -> $_create_status"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    add_check "destructive_create" "fail" "${_create_status:-0}" "POST $_create_path expected 200/201 got ${_create_status:-0}"
    add_error "destructive_create: POST $_create_path expected 200/201 got ${_create_status:-0}"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi

  # Parse the instance ID from the create response.
  _destructive_id=""
  if command -v jq >/dev/null 2>&1; then
    _destructive_id=$(printf '%s' "$_create_body_resp" | jq -r '.id // empty' 2>/dev/null)
  else
    _destructive_id=$(printf '%s' "$_create_body_resp" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' | head -1)
  fi

  # If we don't have an ID from the create response, try listing instances
  # and finding one with the smoke-test name.
  if [ -z "$_destructive_id" ]; then
    _list_body=$(fetch_body "/api/instances" 1)
    if command -v jq >/dev/null 2>&1; then
      _destructive_id=$(printf '%s' "$_list_body" | jq -r --arg name "smoke-test-$TEST_KEY" '.[] | select(.name == $name) | .id' 2>/dev/null | head -1)
    else
      _destructive_id=$(printf '%s' "$_list_body" | grep -o '"id"[[:space:]]*:[[:space:]]*[0-9]*' | tail -1 | sed 's/.*:[[:space:]]*//')
    fi
  fi

  # 2. Start: POST /api/instances/{id}/start
  if [ -n "$_destructive_id" ]; then
    _start_resp=$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' -X POST -u "$AUTH" \
      "$BASE_URL/api/instances/$_destructive_id/start" 2>/dev/null)
    if [ "$_start_resp" = "200" ] || [ "$_start_resp" = "201" ]; then
      add_check "destructive_start" "pass" "$_start_resp" "POST /api/instances/$_destructive_id/start -> $_start_resp"
      PASS_COUNT=$((PASS_COUNT + 1))
    else
      add_check "destructive_start" "fail" "${_start_resp:-0}" "POST /api/instances/$_destructive_id/start expected 200 got ${_start_resp:-0}"
      add_error "destructive_start: failed with status ${_start_resp:-0}"
      FAIL_COUNT=$((FAIL_COUNT + 1))
    fi

    # 3. Wait briefly, then check status: GET /api/instances/{id}/status
    sleep 1
    _dst_status_resp=$(curl -s --max-time 3 -o /dev/null -w '%{http_code}' -u "$AUTH" \
      "$BASE_URL/api/instances/$_destructive_id/status" 2>/dev/null)
    if [ "$_dst_status_resp" = "200" ]; then
      add_check "destructive_status" "pass" "$_dst_status_resp" "GET /api/instances/$_destructive_id/status -> $_dst_status_resp"
      PASS_COUNT=$((PASS_COUNT + 1))
    else
      add_check "destructive_status" "fail" "${_dst_status_resp:-0}" "GET /api/instances/$_destructive_id/status expected 200 got ${_dst_status_resp:-0}"
      add_error "destructive_status: failed with status ${_dst_status_resp:-0}"
      FAIL_COUNT=$((FAIL_COUNT + 1))
    fi

    # 4. Stop: POST /api/instances/{id}/stop
    _stop_resp=$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' -X POST -u "$AUTH" \
      "$BASE_URL/api/instances/$_destructive_id/stop" 2>/dev/null)
    if [ "$_stop_resp" = "200" ] || [ "$_stop_resp" = "201" ]; then
      add_check "destructive_stop" "pass" "$_stop_resp" "POST /api/instances/$_destructive_id/stop -> $_stop_resp"
      PASS_COUNT=$((PASS_COUNT + 1))
    else
      add_check "destructive_stop" "fail" "${_stop_resp:-0}" "POST /api/instances/$_destructive_id/stop expected 200 got ${_stop_resp:-0}"
      add_error "destructive_stop: failed with status ${_stop_resp:-0}"
      FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
  else
    add_check "destructive_start" "skip" 0 "no instance ID — skipped"
    add_check "destructive_status" "skip" 0 "no instance ID — skipped"
    add_check "destructive_stop" "skip" 0 "no instance ID — skipped"
  fi

  # 5. Delete: DELETE /api/instances/{id} — always attempted even if
  #    earlier steps failed (best-effort cleanup).
  if [ -n "$_destructive_id" ]; then
    _delete_resp=$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' -X DELETE -u "$AUTH" \
      "$BASE_URL/api/instances/$_destructive_id" 2>/dev/null)
    if [ "$_delete_resp" = "200" ] || [ "$_delete_resp" = "204" ]; then
      add_check "destructive_delete" "pass" "$_delete_resp" "DELETE /api/instances/$_destructive_id -> $_delete_resp"
      PASS_COUNT=$((PASS_COUNT + 1))
    else
      add_check "destructive_delete" "fail" "${_delete_resp:-0}" "DELETE /api/instances/$_destructive_id expected 200/204 got ${_delete_resp:-0}"
      add_error "destructive_delete: failed with status ${_delete_resp:-0}"
      FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
  else
    add_check "destructive_delete" "skip" 0 "no instance ID — skipped"
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
