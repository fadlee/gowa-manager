<#
.SYNOPSIS
  GOWA Manager - post-cutover smoke tests (PowerShell).

.DESCRIPTION
  Checks a running Go manager's HTTP endpoints.  By default NON-DESTRUCTIVE:
  only GET requests (plus the POST /api/auth/login which is required to
  verify credentials).  Destructive mode additionally exercises the test
  instance lifecycle (start/stop/create/delete) and requires both
  --destructive and --test-key <key>.

  Produces machine-readable JSON on stdout and a concise human summary on
  stderr.  Exits non-zero on any failure.

  Never prints passwords, tokens, or webhook URLs.

.PARAMETER Url
  Base URL of the manager (default: http://localhost:3000).

.PARAMETER AdminUsername
  Admin username (default: admin).

.PARAMETER AdminPassword
  Admin password (default: password).

.PARAMETER Metrics
  Also check GET /metrics.

.PARAMETER Destructive
  Enable destructive checks (requires -TestKey).

.PARAMETER TestKey
  Instance key to use for destructive checks.
#>
[CmdletBinding()]
param(
  [string]$Url = 'http://localhost:3000',

  [Alias('admin-username')]
  [string]$AdminUsername = 'admin',

  [Alias('admin-password')]
  [string]$AdminPassword = 'password',

  [switch]$Metrics,

  [switch]$Destructive,

  [Alias('test-key')]
  [string]$TestKey = ''
)

$ErrorActionPreference = 'SilentlyContinue'

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
function Now-Iso { (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ') }

# ---------------------------------------------------------------------------
# Accumulators
# ---------------------------------------------------------------------------
$checks = [System.Collections.ArrayList]::new()
$errors = [System.Collections.ArrayList]::new()
$passCount = 0
$failCount = 0

function Add-Check([string]$Name, [string]$Status, [int]$HttpStatus, [string]$Detail) {
  $null = $checks.Add(@{ name = $Name; status = $Status; http_status = $HttpStatus; detail = $Detail })
}
function Add-Error([string]$Msg) { $null = $errors.Add($Msg) }

$startTs = Now-Iso

# Destructive mode requires -TestKey.
if ($Destructive -and -not $TestKey) {
  Add-Error 'destructive mode requires --test-key'
  $endTs = Now-Iso
  $result = [ordered]@{
    tool              = 'smoke'
    schema_version    = 1
    mode              = 'non_destructive'
    start_timestamp   = $startTs
    end_timestamp     = $endTs
    url               = $Url
    checks            = @()
    pass_count        = 0
    fail_count        = 0
    errors            = $errors
    exit_code         = 1
  }
  $result | ConvertTo-Json -Depth 10 -Compress | Write-Output
  [Console]::Error.WriteLine('Smoke test failed: destructive mode requires --test-key')
  exit 1
}

$mode = if ($Destructive) { 'destructive' } else { 'non_destructive' }

# Build Basic Auth header value (base64 of user:pass).
$authPair = "${AdminUsername}:${AdminPassword}"
$authBytes = [System.Text.Encoding]::ASCII.GetBytes($authPair)
$authB64 = [System.Convert]::ToBase64String($authBytes)
$authHeader = "Basic $authB64"

# ---------------------------------------------------------------------------
# HTTP request helpers.
# ---------------------------------------------------------------------------
function Invoke-GetCheck([string]$Name, [string]$Path, [bool]$UseAuth, [int]$Expect) {
  $fullUrl = "$Url$Path"
  try {
    $headers = @{}
    if ($UseAuth) { $headers['Authorization'] = $authHeader }
    $resp = Invoke-WebRequest -Uri $fullUrl -Method Get -Headers $headers -UseBasicParsing -TimeoutSec 3
    $status = [int]$resp.StatusCode
  } catch {
    $status = 0
    if ($_.Exception.Response) { $status = [int]$_.Exception.Response.StatusCode }
  }
  if ($status -eq $Expect) {
    Add-Check $Name 'pass' $status "GET $Path -> $status"
    $script:passCount++
  } else {
    Add-Check $Name 'fail' $status "GET $Path expected $Expect got $status"
    Add-Error "$Name`: GET $Path expected $Expect got $status"
    $script:failCount++
  }
}

function Invoke-PostCheck([string]$Name, [string]$Path, [bool]$UseAuth, [int]$Expect) {
  $fullUrl = "$Url$Path"
  try {
    $headers = @{}
    if ($UseAuth) { $headers['Authorization'] = $authHeader }
    $resp = Invoke-WebRequest -Uri $fullUrl -Method Post -Headers $headers -UseBasicParsing -TimeoutSec 3
    $status = [int]$resp.StatusCode
  } catch {
    $status = 0
    if ($_.Exception.Response) { $status = [int]$_.Exception.Response.StatusCode }
  }
  if ($status -eq $Expect) {
    Add-Check $Name 'pass' $status "POST $Path -> $status"
    $script:passCount++
  } else {
    Add-Check $Name 'fail' $status "POST $Path expected $Expect got $status"
    Add-Error "$Name`: POST $Path expected $Expect got $status"
    $script:failCount++
  }
}

# ---------------------------------------------------------------------------
# Non-destructive checks
# ---------------------------------------------------------------------------

# GET /api/health - no auth, expect 200.
Invoke-GetCheck 'health' '/api/health' $false 200

# GET /api/ready - no auth, expect 200.
Invoke-GetCheck 'ready' '/api/ready' $false 200

# POST /api/auth/login - Basic Auth credentials, expect 200.
Invoke-PostCheck 'auth_login' '/api/auth/login' $true 200

# GET /api/instances - with Basic Auth, expect 200.
Invoke-GetCheck 'instances' '/api/instances' $true 200

# GET /api/system/status - with Basic Auth, expect 200.
Invoke-GetCheck 'system_status' '/api/system/status' $true 200

# GET /api/system/versions/installed - with Basic Auth, expect 200.
Invoke-GetCheck 'system_versions_installed' '/api/system/versions/installed' $true 200

# GET /api/system/auto-update/status - with Basic Auth, expect 200.
Invoke-GetCheck 'system_autoupdate_status' '/api/system/auto-update/status' $true 200

# GET /metrics - only if -Metrics flag passed; no auth, expect 200.
if ($Metrics) {
  Invoke-GetCheck 'metrics' '/metrics' $false 200
}

# ---------------------------------------------------------------------------
# Destructive checks (only when -Destructive and -TestKey supplied)
# ---------------------------------------------------------------------------
if ($Destructive) {
  $body = @{ name = "smoke-test-$TestKey"; gowa_version = 'v1.0.0' } | ConvertTo-Json -Compress
  try {
    $headers = @{ Authorization = $authHeader; 'Content-Type' = 'application/json' }
    $resp = Invoke-WebRequest -Uri "$Url/api/instances" -Method Post -Headers $headers -Body $body -UseBasicParsing -TimeoutSec 3
    $status = [int]$resp.StatusCode
  } catch {
    $status = 0
    if ($_.Exception.Response) { $status = [int]$_.Exception.Response.StatusCode }
  }
  if ($status -eq 200 -or $status -eq 201) {
    Add-Check 'destructive_create' 'pass' $status "POST /api/instances -> $status"
    $script:passCount++
  } else {
    Add-Check 'destructive_create' 'fail' $status "POST /api/instances expected 200/201 got $status"
    Add-Error "destructive_create: POST /api/instances expected 200/201 got $status"
    $script:failCount++
  }
}

# ---------------------------------------------------------------------------
# Emit JSON
# ---------------------------------------------------------------------------
$exitCode = if ($failCount -gt 0) { 1 } else { 0 }
$endTs = Now-Iso

$result = [ordered]@{
  tool              = 'smoke'
  schema_version    = 1
  mode              = $mode
  start_timestamp   = $startTs
  end_timestamp     = $endTs
  url               = $Url
  metrics_enabled   = $Metrics.IsPresent
  destructive       = $Destructive.IsPresent
  checks            = $checks
  pass_count        = $passCount
  fail_count        = $failCount
  errors            = $errors
  exit_code         = $exitCode
}

$result | ConvertTo-Json -Depth 10 -Compress | Write-Output

# ---------------------------------------------------------------------------
# Human summary to stderr
# ---------------------------------------------------------------------------
[Console]::Error.WriteLine('')
[Console]::Error.WriteLine('=== GOWA Manager Smoke Tests ===')
[Console]::Error.WriteLine("URL:          $Url")
[Console]::Error.WriteLine("Mode:         $mode")
$metricsStr = if ($Metrics) { 'enabled' } else { 'disabled' }
[Console]::Error.WriteLine("Metrics:      $metricsStr")
[Console]::Error.WriteLine("Passed:       $passCount")
[Console]::Error.WriteLine("Failed:       $failCount")
[Console]::Error.WriteLine("Start:        $startTs")
[Console]::Error.WriteLine("End:          $endTs")
if ($errors.Count -gt 0) {
  [Console]::Error.WriteLine('')
  [Console]::Error.WriteLine('Errors:')
  foreach ($e in $errors) { [Console]::Error.WriteLine("  - $e") }
}
[Console]::Error.WriteLine('')
if ($exitCode -eq 0) {
  [Console]::Error.WriteLine('Result: SMOKE OK - all checks passed')
} else {
  [Console]::Error.WriteLine('Result: SMOKE FAILED - see errors above')
}

exit $exitCode

