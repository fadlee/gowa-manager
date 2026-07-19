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

# Invoke-GetCheckMulti accepts multiple acceptable status codes (used for
# proxy endpoints that may return 200, 502, or 503 depending on whether
# the upstream instance is running).
function Invoke-GetCheckMulti([string]$Name, [string]$Path, [bool]$UseAuth, [int[]]$Expects) {
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
  $matched = $false
  foreach ($expect in $Expects) {
    if ($status -eq $expect) { $matched = $true; break }
  }
  $expectsStr = $Expects -join ' '
  if ($matched) {
    Add-Check $Name 'pass' $status "GET $Path -> $status"
    $script:passCount++
  } else {
    Add-Check $Name 'fail' $status "GET $Path expected one of $expectsStr got $status"
    Add-Error "$Name`: GET $Path expected one of $expectsStr got $status"
    $script:failCount++
  }
}

# Fetch the response body (for JSON parsing).  Does not record a check.
function Fetch-Body([string]$Path, [bool]$UseAuth) {
  $fullUrl = "$Url$Path"
  try {
    $headers = @{}
    if ($UseAuth) { $headers['Authorization'] = $authHeader }
    $resp = Invoke-WebRequest -Uri $fullUrl -Method Get -Headers $headers -UseBasicParsing -TimeoutSec 3
    return $resp.Content
  } catch {
    return ''
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

# Parse the instances list to get the first instance's id and key for
# the detail/status/proxy checks below.
$firstId = ''
$firstKey = ''
$instancesBody = Fetch-Body '/api/instances' $true
if ($instancesBody) {
  try {
    $instancesJson = $instancesBody | ConvertFrom-Json
    if ($instancesJson -and $instancesJson.Count -gt 0) {
      $firstId = [string]$instancesJson[0].id
      $firstKey = [string]$instancesJson[0].key
    }
  } catch {
    # JSON parse failed — leave firstId/firstKey empty.
  }
}

if ($firstId -and $firstKey) {
  # GET /api/instances/{id} - instance detail, with Basic Auth, expect 200.
  Invoke-GetCheck 'instance_detail' "/api/instances/$firstId" $true 200
  # GET /api/instances/{id}/status - instance status, with Basic Auth, expect 200.
  Invoke-GetCheck 'instance_status' "/api/instances/$firstId/status" $true 200
  # GET /app/{key}/status - proxy status, no auth.  Accept 200, 502, 503.
  Invoke-GetCheckMulti 'proxy_status' "/app/$firstKey/status" $false @(200, 502, 503)
  # GET /app/{key}/health - proxy health, no auth.  Same multi-accept.
  Invoke-GetCheckMulti 'proxy_health' "/app/$firstKey/health" $false @(200, 502, 503)
} else {
  # No instances available — skip these checks (not failures).
  Add-Check 'instance_detail' 'skip' 0 'no instances available - skipped'
  Add-Check 'instance_status' 'skip' 0 'no instances available - skipped'
  Add-Check 'proxy_status' 'skip' 0 'no instances available - skipped'
  Add-Check 'proxy_health' 'skip' 0 'no instances available - skipped'
}

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
  # Full lifecycle: create -> start -> status -> stop -> delete.
  # Each step is recorded.  If any step fails, we continue to the next
  # (best-effort cleanup).  The delete step is always attempted.

  # 1. Create: POST /api/instances with the test key.
  $createBody = @{ name = "smoke-test-$TestKey"; gowa_version = 'v1.0.0' } | ConvertTo-Json -Compress
  $destructiveId = ''
  try {
    $headers = @{ Authorization = $authHeader; 'Content-Type' = 'application/json' }
    $createResp = Invoke-WebRequest -Uri "$Url/api/instances" -Method Post -Headers $headers -Body $createBody -UseBasicParsing -TimeoutSec 5
    $createStatus = [int]$createResp.StatusCode
    $createBodyResp = $createResp.Content
  } catch {
    $createStatus = 0
    $createBodyResp = ''
    if ($_.Exception.Response) { $createStatus = [int]$_.Exception.Response.StatusCode }
  }
  if ($createStatus -eq 200 -or $createStatus -eq 201) {
    Add-Check 'destructive_create' 'pass' $createStatus "POST /api/instances -> $createStatus"
    $script:passCount++
  } else {
    Add-Check 'destructive_create' 'fail' $createStatus "POST /api/instances expected 200/201 got $createStatus"
    Add-Error "destructive_create: POST /api/instances expected 200/201 got $createStatus"
    $script:failCount++
  }

  # Parse the instance ID from the create response.
  if ($createBodyResp) {
    try {
      $createdObj = $createBodyResp | ConvertFrom-Json
      if ($createdObj.id) { $destructiveId = [string]$createdObj.id }
    } catch {}
  }

  # If we don't have an ID from the create response, try listing instances.
  if (-not $destructiveId) {
    $listBody = Fetch-Body '/api/instances' $true
    if ($listBody) {
      try {
        $listJson = $listBody | ConvertFrom-Json
        $found = $listJson | Where-Object { $_.name -eq "smoke-test-$TestKey" } | Select-Object -First 1
        if ($found -and $found.id) { $destructiveId = [string]$found.id }
      } catch {}
    }
  }

  # 2. Start: POST /api/instances/{id}/start
  if ($destructiveId) {
    try {
      $headers = @{ Authorization = $authHeader }
      $startResp = Invoke-WebRequest -Uri "$Url/api/instances/$destructiveId/start" -Method Post -Headers $headers -UseBasicParsing -TimeoutSec 5
      $startStatus = [int]$startResp.StatusCode
    } catch {
      $startStatus = 0
      if ($_.Exception.Response) { $startStatus = [int]$_.Exception.Response.StatusCode }
    }
    if ($startStatus -eq 200 -or $startStatus -eq 201) {
      Add-Check 'destructive_start' 'pass' $startStatus "POST /api/instances/$destructiveId/start -> $startStatus"
      $script:passCount++
    } else {
      Add-Check 'destructive_start' 'fail' $startStatus "POST /api/instances/$destructiveId/start expected 200 got $startStatus"
      Add-Error "destructive_start: failed with status $startStatus"
      $script:failCount++
    }

    # 3. Wait briefly, then check status: GET /api/instances/{id}/status
    Start-Sleep -Seconds 1
    try {
      $headers = @{ Authorization = $authHeader }
      $dstStatusResp = Invoke-WebRequest -Uri "$Url/api/instances/$destructiveId/status" -Method Get -Headers $headers -UseBasicParsing -TimeoutSec 3
      $dstStatus = [int]$dstStatusResp.StatusCode
    } catch {
      $dstStatus = 0
      if ($_.Exception.Response) { $dstStatus = [int]$_.Exception.Response.StatusCode }
    }
    if ($dstStatus -eq 200) {
      Add-Check 'destructive_status' 'pass' $dstStatus "GET /api/instances/$destructiveId/status -> $dstStatus"
      $script:passCount++
    } else {
      Add-Check 'destructive_status' 'fail' $dstStatus "GET /api/instances/$destructiveId/status expected 200 got $dstStatus"
      Add-Error "destructive_status: failed with status $dstStatus"
      $script:failCount++
    }

    # 4. Stop: POST /api/instances/{id}/stop
    try {
      $headers = @{ Authorization = $authHeader }
      $stopResp = Invoke-WebRequest -Uri "$Url/api/instances/$destructiveId/stop" -Method Post -Headers $headers -UseBasicParsing -TimeoutSec 5
      $stopStatus = [int]$stopResp.StatusCode
    } catch {
      $stopStatus = 0
      if ($_.Exception.Response) { $stopStatus = [int]$_.Exception.Response.StatusCode }
    }
    if ($stopStatus -eq 200 -or $stopStatus -eq 201) {
      Add-Check 'destructive_stop' 'pass' $stopStatus "POST /api/instances/$destructiveId/stop -> $stopStatus"
      $script:passCount++
    } else {
      Add-Check 'destructive_stop' 'fail' $stopStatus "POST /api/instances/$destructiveId/stop expected 200 got $stopStatus"
      Add-Error "destructive_stop: failed with status $stopStatus"
      $script:failCount++
    }
  } else {
    Add-Check 'destructive_start' 'skip' 0 'no instance ID - skipped'
    Add-Check 'destructive_status' 'skip' 0 'no instance ID - skipped'
    Add-Check 'destructive_stop' 'skip' 0 'no instance ID - skipped'
  }

  # 5. Delete: DELETE /api/instances/{id} — always attempted.
  if ($destructiveId) {
    try {
      $headers = @{ Authorization = $authHeader }
      $deleteResp = Invoke-WebRequest -Uri "$Url/api/instances/$destructiveId" -Method Delete -Headers $headers -UseBasicParsing -TimeoutSec 5
      $deleteStatus = [int]$deleteResp.StatusCode
    } catch {
      $deleteStatus = 0
      if ($_.Exception.Response) { $deleteStatus = [int]$_.Exception.Response.StatusCode }
    }
    if ($deleteStatus -eq 200 -or $deleteStatus -eq 204) {
      Add-Check 'destructive_delete' 'pass' $deleteStatus "DELETE /api/instances/$destructiveId -> $deleteStatus"
      $script:passCount++
    } else {
      Add-Check 'destructive_delete' 'fail' $deleteStatus "DELETE /api/instances/$destructiveId expected 200/204 got $deleteStatus"
      Add-Error "destructive_delete: failed with status $deleteStatus"
      $script:failCount++
    }
  } else {
    Add-Check 'destructive_delete' 'skip' 0 'no instance ID - skipped'
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

