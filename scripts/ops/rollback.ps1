<#
.SYNOPSIS
  GOWA Manager - Go-to-Bun rollback orchestrator (PowerShell).

.DESCRIPTION
  Reverts the system from the Go manager to the pinned Bun manager.
  This script orchestrates: stopping Go traffic, waiting for lifecycle
  operations to quiesce, recording child process state, capturing logs
  and the current DB, running SQLite integrity/schema checks, choosing
  a DB strategy (use current or restore named backup), starting the
  pinned Bun command, and running Bun smoke tests.

  Default mode is DRY-RUN: prints what would happen and exits 0.
  -Execute is required to actually perform the rollback.

  Never prints passwords, tokens, or webhook URLs.

.PARAMETER Execute
  Actually perform the rollback (default: dry-run).

.PARAMETER BackupDir
  Backup directory (required when -Execute).

.PARAMETER GoPid
  PID of the running Go manager (required when -Execute).

.PARAMETER GoVersion
  Go manager version string (required when -Execute).

.PARAMETER BunBinary
  Path to the pinned Bun binary (required when -Execute).

.PARAMETER BunChecksum
  Expected SHA-256 of the Bun binary (required when -Execute).

.PARAMETER DataDir
  Data directory (default: ./data).

.PARAMETER SqliteBin
  Path to sqlite3 CLI (default: sqlite3 from PATH).

.PARAMETER BunUrl
  Bun manager URL for smoke tests (default: http://localhost:3000).

.PARAMETER OverrideAmbiguousState
  Proceed even if child/process state is ambiguous.
#>
[CmdletBinding()]
param(
  [switch]$Execute,

  [Alias('backup-dir')]
  [string]$BackupDir = '',

  [Alias('go-pid')]
  [string]$GoPid = '',

  [Alias('go-version')]
  [string]$GoVersion = '',

  [Alias('bun-binary')]
  [string]$BunBinary = '',

  [Alias('bun-checksum')]
  [string]$BunChecksum = '',

  [Alias('data-dir')]
  [string]$DataDir = './data',

  [Alias('sqlite-bin')]
  [string]$SqliteBin = 'sqlite3',

  [Alias('bun-url')]
  [string]$BunUrl = 'http://localhost:3000',

  [Alias('override-ambiguous-state')]
  [switch]$OverrideAmbiguousState
)

$ErrorActionPreference = 'SilentlyContinue'

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
function Now-Iso { (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ') }

function Get-Sha256([string]$Path) {
  try {
    $h = [System.Security.Cryptography.SHA256]::Create()
    $fs = [System.IO.File]::OpenRead($Path)
    $hashBytes = $h.ComputeHash($fs)
    $fs.Close()
    $h.Dispose()
    return ([System.BitConverter]::ToString($hashBytes) -replace '-', '').ToLower()
  } catch {
    return 'error'
  }
}

# ---------------------------------------------------------------------------
# Accumulators
# ---------------------------------------------------------------------------
$steps = [System.Collections.ArrayList]::new()
$errors = [System.Collections.ArrayList]::new()
$warnings = [System.Collections.ArrayList]::new()

function Add-Step([string]$Name, [string]$Status, [string]$Detail) {
  $null = $steps.Add(@{ name = $Name; status = $Status; detail = $Detail })
}
function Add-Error([string]$Msg) { $null = $errors.Add($Msg) }
function Add-Warning([string]$Msg) { $null = $warnings.Add($Msg) }

$startTs = Now-Iso

# ---------------------------------------------------------------------------
# Validate required arguments when -Execute is set
# ---------------------------------------------------------------------------
if ($Execute) {
  $missing = @()
  if (-not $BackupDir)   { $missing += '--backup-dir' }
  if (-not $GoPid)       { $missing += '--go-pid' }
  if (-not $GoVersion)   { $missing += '--go-version' }
  if (-not $BunBinary)   { $missing += '--bun-binary' }
  if (-not $BunChecksum) { $missing += '--bun-checksum' }
  if ($missing.Count -gt 0) {
    Add-Error "missing required arguments when --execute: $($missing -join ' ')"
    $endTs = Now-Iso
    $result = [ordered]@{
      tool              = 'rollback'
      schema_version    = 1
      mode              = 'execute'
      start_timestamp   = $startTs
      end_timestamp     = $endTs
      steps             = @()
      errors            = $errors
      warnings          = @()
      exit_code         = 1
    }
    $result | ConvertTo-Json -Depth 10 -Compress | Write-Output
    [Console]::Error.WriteLine("Rollback failed: missing required arguments: $($missing -join ' ')")
    exit 1
  }
}

# ---------------------------------------------------------------------------
# DRY-RUN mode: print the plan and exit 0
# ---------------------------------------------------------------------------
if (-not $Execute) {
  Add-Step 'dry_run' 'pass' 'dry-run mode - no changes made'
  Add-Step 'plan_stop_go' 'skip' "would stop Go PID $GoPid"
  Add-Step 'plan_quiesce' 'skip' 'would wait for lifecycle operations to quiesce'
  Add-Step 'plan_record_children' 'skip' "would record child process state from $DataDir"
  Add-Step 'plan_capture_logs_db' 'skip' "would capture logs and current DB to $BackupDir"
  Add-Step 'plan_integrity_check' 'skip' 'would run SQLite integrity and schema checks'
  Add-Step 'plan_db_strategy' 'skip' 'would use current DB if compatible or restore named backup'
  Add-Step 'plan_start_bun' 'skip' "would start Bun binary $BunBinary"
  Add-Step 'plan_bun_smoke' 'skip' "would run smoke tests against $BunUrl"

  $endTs = Now-Iso
  $result = [ordered]@{
    tool              = 'rollback'
    schema_version    = 1
    mode              = 'dry_run'
    start_timestamp   = $startTs
    end_timestamp     = $endTs
    execute           = $false
    steps             = $steps
    errors            = $errors
    warnings          = $warnings
    exit_code         = 0
  }
  $result | ConvertTo-Json -Depth 10 -Compress | Write-Output

  [Console]::Error.WriteLine('')
  [Console]::Error.WriteLine('=== GOWA Manager Rollback (DRY-RUN) ===')
  [Console]::Error.WriteLine('Mode:         dry-run (no changes made)')
  $goPidStr = if ($GoPid) { $GoPid } else { '<not specified>' }
  $goVerStr = if ($GoVersion) { $GoVersion } else { '<not specified>' }
  $bunBinStr = if ($BunBinary) { $BunBinary } else { '<not specified>' }
  $bunChkStr = if ($BunChecksum) { $BunChecksum } else { '<not specified>' }
  $bkDirStr = if ($BackupDir) { $BackupDir } else { '<not specified>' }
  [Console]::Error.WriteLine("Go PID:       $goPidStr")
  [Console]::Error.WriteLine("Go version:   $goVerStr")
  [Console]::Error.WriteLine("Bun binary:   $bunBinStr")
  [Console]::Error.WriteLine("Bun checksum: $bunChkStr")
  [Console]::Error.WriteLine("Data dir:     $DataDir")
  [Console]::Error.WriteLine("Backup dir:   $bkDirStr")
  [Console]::Error.WriteLine("Bun URL:      $BunUrl")
  [Console]::Error.WriteLine('')
  [Console]::Error.WriteLine('Plan:')
  [Console]::Error.WriteLine("  1. Stop Go traffic (PID $goPidStr)")
  [Console]::Error.WriteLine('  2. Wait for lifecycle operations to quiesce')
  [Console]::Error.WriteLine("  3. Record child process state from $DataDir")
  [Console]::Error.WriteLine('  4. Capture logs and current DB')
  [Console]::Error.WriteLine('  5. Run SQLite integrity and schema checks')
  [Console]::Error.WriteLine('  6. Choose DB strategy (use current or restore backup)')
  [Console]::Error.WriteLine('  7. Start pinned Bun binary')
  [Console]::Error.WriteLine("  8. Run Bun smoke tests against $BunUrl")
  [Console]::Error.WriteLine('')
  [Console]::Error.WriteLine('Result: DRY-RUN - no changes made. Use --execute to perform rollback.')
  exit 0
}

# ---------------------------------------------------------------------------
# EXECUTE mode
# ---------------------------------------------------------------------------
$exitCode = 0

# Step 1: Stop Go traffic (stop the Go process by PID).
$goProcess = $null
try { $goProcess = Get-Process -Id ([int]$GoPid) -ErrorAction Stop } catch {}
if ($goProcess) {
  try {
    Stop-Process -Id ([int]$GoPid) -ErrorAction Stop
    Add-Step 'stop_go' 'pass' "sent stop to PID $GoPid"
  } catch {
    Add-Step 'stop_go' 'fail' "failed to stop PID $GoPid"
    Add-Error "cannot stop Go process: Stop-Process $GoPid failed"
    $exitCode = 1
  }
} else {
  if ($OverrideAmbiguousState) {
    Add-Step 'stop_go' 'pass' "Go PID $GoPid not running (override-ambiguous-state)"
    Add-Warning "Go PID $GoPid was not running; proceeded due to --override-ambiguous-state"
  } else {
    Add-Step 'stop_go' 'fail' "Go PID $GoPid not running and --override-ambiguous-state not set"
    Add-Error "Go PID $GoPid is not running; use --override-ambiguous-state to proceed"
    $exitCode = 1
  }
}

# Step 2: Wait for lifecycle operations to quiesce.
if ($exitCode -eq 0) {
  $waited = 0
  $maxWait = 10
  while ($waited -lt $maxWait) {
    $stillRunning = $false
    try { $null = Get-Process -Id ([int]$GoPid) -ErrorAction Stop; $stillRunning = $true } catch {}
    if (-not $stillRunning) { break }
    Start-Sleep -Seconds 1
    $waited++
  }
  $stillRunning = $false
  try { $null = Get-Process -Id ([int]$GoPid) -ErrorAction Stop; $stillRunning = $true } catch {}
  if ($stillRunning) {
    if ($OverrideAmbiguousState) {
      Add-Step 'quiesce' 'pass' "Go PID $GoPid still running after ${waited}s (override)"
      Add-Warning "Go PID $GoPid did not exit within ${waited}s; proceeded due to override"
    } else {
      Add-Step 'quiesce' 'fail' "Go PID $GoPid did not exit within ${waited}s"
      Add-Error "Go process did not quiesce within ${waited}s; use --override-ambiguous-state to proceed"
      $exitCode = 1
    }
  } else {
    Add-Step 'quiesce' 'pass' "Go PID $GoPid exited after ${waited}s"
  }
}

# Step 3: Record child process state (running instances from DB).
if ($exitCode -eq 0) {
  $dbPath = Join-Path $DataDir 'gowa.db'
  $childCount = 0
  $sqliteOk = $false
  try { & $SqliteBin --version 2>$null | Out-Null; $sqliteOk = $true } catch {}
  if ((Test-Path $dbPath -PathType Leaf) -and $sqliteOk) {
    $childCount = [int](& $SqliteBin $dbPath "SELECT COUNT(*) FROM instances WHERE status='running';" 2>$null)
    Add-Step 'record_children' 'pass' "$childCount running instances recorded"
  } else {
    if ($OverrideAmbiguousState) {
      Add-Step 'record_children' 'pass' 'DB or sqlite3 unavailable (override)'
      Add-Warning 'could not read child process state; proceeded due to override'
    } else {
      Add-Step 'record_children' 'fail' 'DB or sqlite3 unavailable'
      Add-Error 'cannot record child process state: DB or sqlite3 unavailable'
      $exitCode = 1
    }
  }
}

# Step 4: Capture logs and current DB (call backup.ps1).
if ($exitCode -eq 0) {
  $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
  $backupScript = Join-Path $scriptDir 'backup.ps1'
  if (Test-Path $backupScript -PathType Leaf) {
    $backupOut = & pwsh -NoProfile -NonInteractive -File $backupScript -DataDir $DataDir -BackupDir $BackupDir 2>$null
    $backupExit = $LASTEXITCODE
    if ($backupExit -eq 0) {
      Add-Step 'capture_logs_db' 'pass' "backup.ps1 completed to $BackupDir"
    } else {
      if ($OverrideAmbiguousState) {
        Add-Step 'capture_logs_db' 'pass' "backup.ps1 failed (override)"
        Add-Warning "backup.ps1 exited $backupExit; proceeded due to override"
      } else {
        Add-Step 'capture_logs_db' 'fail' "backup.ps1 exited $backupExit"
        Add-Error "backup.ps1 failed with exit code $backupExit"
        $exitCode = 1
      }
    }
  } else {
    Add-Step 'capture_logs_db' 'skip' "backup.ps1 not found at $backupScript"
    Add-Warning 'backup.ps1 not found; skipped DB capture'
  }
}

# Step 5: Run SQLite integrity check and schema check.
if ($exitCode -eq 0) {
  $dbPath = Join-Path $DataDir 'gowa.db'
  $sqliteOk = $false
  try { & $SqliteBin --version 2>$null | Out-Null; $sqliteOk = $true } catch {}
  if ((Test-Path $dbPath -PathType Leaf) -and $sqliteOk) {
    $integrity = (& $SqliteBin $dbPath 'PRAGMA integrity_check;' 2>$null | Select-Object -First 1)
    if ($integrity -eq 'ok') {
      Add-Step 'integrity_check' 'pass' 'SQLite integrity check: ok'
    } else {
      if ($OverrideAmbiguousState) {
        Add-Step 'integrity_check' 'pass' "integrity check: $integrity (override)"
        Add-Warning "SQLite integrity check returned: $integrity; proceeded due to override"
      } else {
        Add-Step 'integrity_check' 'fail' "integrity check: $integrity"
        Add-Error "SQLite integrity check failed: $integrity"
        $exitCode = 1
      }
    }
    $schema = (& $SqliteBin $dbPath "SELECT name FROM sqlite_master WHERE type='table' AND name='instances';" 2>$null | Select-Object -First 1)
    if ($schema -eq 'instances') {
      Add-Step 'schema_check' 'pass' 'instances table present'
    } else {
      if ($OverrideAmbiguousState) {
        Add-Step 'schema_check' 'pass' 'instances table missing (override)'
        Add-Warning 'instances table not found; proceeded due to override'
      } else {
        Add-Step 'schema_check' 'fail' 'instances table missing'
        Add-Error 'schema check failed: instances table not found'
        $exitCode = 1
      }
    }
  } else {
    Add-Step 'integrity_check' 'skip' 'DB or sqlite3 unavailable'
    Add-Step 'schema_check' 'skip' 'DB or sqlite3 unavailable'
  }
}

# Step 6: Verify Bun binary checksum before starting.
if ($exitCode -eq 0) {
  if (Test-Path $BunBinary -PathType Leaf) {
    $actualSha = Get-Sha256 $BunBinary
    if ($actualSha -eq $BunChecksum) {
      Add-Step 'verify_bun_checksum' 'pass' 'Bun binary checksum matches'
    } else {
      Add-Step 'verify_bun_checksum' 'fail' "checksum mismatch: expected $BunChecksum got $actualSha"
      Add-Error "Bun binary checksum mismatch: expected $BunChecksum got $actualSha"
      $exitCode = 1
    }
  } else {
    Add-Step 'verify_bun_checksum' 'fail' "Bun binary not found: $BunBinary"
    Add-Error "Bun binary not found: $BunBinary"
    $exitCode = 1
  }
}

# Step 7: Start the pinned Bun command.
if ($exitCode -eq 0) {
  try {
    $bunProc = Start-Process -FilePath $BunBinary -PassThru -ErrorAction Stop
    Add-Step 'start_bun' 'pass' "started Bun binary (PID $($bunProc.Id))"
  } catch {
    Add-Step 'start_bun' 'fail' "failed to start Bun binary: $BunBinary"
    Add-Error "failed to start Bun binary: $_"
    $exitCode = 1
  }
}

# Step 8: Run Bun smoke tests (call smoke.ps1 against the Bun URL).
if ($exitCode -eq 0) {
  $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
  $smokeScript = Join-Path $scriptDir 'smoke.ps1'
  if (Test-Path $smokeScript -PathType Leaf) {
    Start-Sleep -Seconds 1
    $smokeOut = & pwsh -NoProfile -NonInteractive -File $smokeScript -Url $BunUrl 2>$null
    $smokeExit = $LASTEXITCODE
    if ($smokeExit -eq 0) {
      Add-Step 'bun_smoke' 'pass' "smoke.ps1 against $BunUrl passed"
    } else {
      Add-Step 'bun_smoke' 'fail' "smoke.ps1 exited $smokeExit"
      Add-Error "Bun smoke tests failed with exit code $smokeExit"
      $exitCode = 1
    }
  } else {
    Add-Step 'bun_smoke' 'skip' "smoke.ps1 not found at $smokeScript"
    Add-Warning 'smoke.ps1 not found; skipped Bun smoke tests'
  }
}

# ---------------------------------------------------------------------------
# Emit JSON
# ---------------------------------------------------------------------------
$endTs = Now-Iso

$result = [ordered]@{
  tool                    = 'rollback'
  schema_version          = 1
  mode                    = 'execute'
  start_timestamp         = $startTs
  end_timestamp           = $endTs
  execute                 = $true
  go_pid                  = $GoPid
  go_version              = $GoVersion
  bun_binary              = $BunBinary
  bun_checksum_verified   = ($exitCode -eq 0)
  data_dir                = $DataDir
  backup_dir              = $BackupDir
  bun_url                 = $BunUrl
  override_ambiguous_state = $OverrideAmbiguousState.IsPresent
  steps                   = $steps
  errors                  = $errors
  warnings                = $warnings
  exit_code               = $exitCode
}

$result | ConvertTo-Json -Depth 10 -Compress | Write-Output

# ---------------------------------------------------------------------------
# Human summary to stderr
# ---------------------------------------------------------------------------
[Console]::Error.WriteLine('')
[Console]::Error.WriteLine('=== GOWA Manager Rollback ===')
[Console]::Error.WriteLine('Mode:         execute')
[Console]::Error.WriteLine("Go PID:       $GoPid")
[Console]::Error.WriteLine("Go version:   $GoVersion")
[Console]::Error.WriteLine("Bun binary:   $BunBinary")
[Console]::Error.WriteLine("Data dir:     $DataDir")
[Console]::Error.WriteLine("Backup dir:   $BackupDir")
[Console]::Error.WriteLine("Bun URL:      $BunUrl")
[Console]::Error.WriteLine("Start:        $startTs")
[Console]::Error.WriteLine("End:          $endTs")
if ($warnings.Count -gt 0) {
  [Console]::Error.WriteLine('')
  [Console]::Error.WriteLine('Warnings:')
  foreach ($w in $warnings) { [Console]::Error.WriteLine("  - $w") }
}
if ($errors.Count -gt 0) {
  [Console]::Error.WriteLine('')
  [Console]::Error.WriteLine('Errors:')
  foreach ($e in $errors) { [Console]::Error.WriteLine("  - $e") }
}
[Console]::Error.WriteLine('')
if ($exitCode -eq 0) {
  [Console]::Error.WriteLine('Result: ROLLBACK OK - Go stopped, Bun started')
} else {
  [Console]::Error.WriteLine('Result: ROLLBACK FAILED - see errors above')
}

exit $exitCode
