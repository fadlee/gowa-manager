<#
.SYNOPSIS
  GOWA Manager - pre-cutover backup (PowerShell).

.DESCRIPTION
  Run AFTER stopping Bun but BEFORE starting Go.  Creates a consistent
  backup of the SQLite database (online backup for WAL, safe file copy
  otherwise), instance/version metadata, and a SHA-256 manifest that is
  verified immediately after writing.

  This script does NOT stop the manager - the operator must ensure Bun is
  stopped and child processes are stabilised before invoking it.

  Never prints passwords, instance config, or webhook URLs.
  Never claims atomicity across multiple files.

.PARAMETER DataDir
  Data directory to back up (default: ./data).

.PARAMETER BackupDir
  Destination directory for the backup (default: ./backup/<timestamp>).

.PARAMETER SqliteBin
  Path to sqlite3 CLI (default: sqlite3 from PATH).
#>
[CmdletBinding()]
param(
  [Alias('d')]
  [string]$DataDir = './data',

  [Alias('o')]
  [string]$BackupDir = '',

  [string]$SqliteBin = 'sqlite3',

  [switch]$Verify
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

$startTs = Now-Iso

# Default backup dir with timestamp.
if (-not $BackupDir) {
  $BackupDir = "./backup/$((Get-Date).ToUniversalTime().ToString('yyyyMMdd-HHmmss'))"
}

# ---------------------------------------------------------------------------
# Accumulators (defined early so verify mode can use Add-Error)
# ---------------------------------------------------------------------------
$errors = [System.Collections.ArrayList]::new()
$files = [System.Collections.ArrayList]::new()

function Add-Error([string]$Msg) { $null = $errors.Add($Msg) }
function Add-File([string]$RelPath, [string]$Sha256, [long]$Size) {
  $null = $files.Add(@{ path = $RelPath; sha256 = $Sha256; size = $Size })
}

# ---------------------------------------------------------------------------
# Verify mode: re-read the manifest and re-hash all files.  No backup is
# performed.  Exits non-zero on any mismatch or missing file.
# ---------------------------------------------------------------------------
if ($Verify) {
  $errors = [System.Collections.ArrayList]::new()
  $manifestName = 'manifest.sha256'
  $manifestPath = Join-Path $BackupDir $manifestName
  $manifestVerified = $true
  $fileCount = 0

  if (-not (Test-Path $manifestPath -PathType Leaf)) {
    Add-Error "manifest not found: $manifestPath"
    $manifestVerified = $false
  } else {
    foreach ($line in (Get-Content $manifestPath)) {
      if ([string]::IsNullOrWhiteSpace($line)) { continue }
      $fileCount++
      $parts = $line -split '  ', 2
      if ($parts.Length -lt 2) {
        $manifestVerified = $false
        Add-Error 'manifest verify: malformed line'
        break
      }
      $expectedSha = $parts[0]
      $relPath = $parts[1].Trim()
      $fullPath = Join-Path $BackupDir $relPath
      if (-not (Test-Path $fullPath -PathType Leaf)) {
        $manifestVerified = $false
        Add-Error "manifest verify: file missing: $relPath"
        break
      }
      $actualSha = Get-Sha256 $fullPath
      if ($expectedSha -ne $actualSha) {
        $manifestVerified = $false
        Add-Error "manifest verify: checksum mismatch for $relPath"
        break
      }
    }
  }

  $verifyExit = if ($manifestVerified) { 0 } else { 1 }
  $endTs = Now-Iso
  $result = [ordered]@{
    tool              = 'backup'
    schema_version    = 1
    mode              = 'verify'
    start_timestamp   = $startTs
    end_timestamp     = $endTs
    data_dir          = $DataDir
    backup_dir        = $BackupDir
    manager_downtime  = [ordered]@{
      state = 'assumed_stopped'
      note  = 'verify mode - no backup performed'
    }
    journal_mode      = 'unknown'
    method            = 'verify'
    files             = @()
    manifest          = [ordered]@{
      path       = $manifestName
      verified   = $manifestVerified
      file_count = $fileCount
    }
    metadata          = [ordered]@{ instances_copied = 0; versions_copied = 0 }
    errors            = $errors
    exit_code         = $verifyExit
  }
  $result | ConvertTo-Json -Depth 10 -Compress | Write-Output
  [Console]::Error.WriteLine('')
  [Console]::Error.WriteLine('=== GOWA Manager Backup Verify ===')
  [Console]::Error.WriteLine("Backup dir:   $BackupDir")
  $manifestStatus = if ($manifestVerified) { 'verified' } else { 'VERIFICATION FAILED' }
  [Console]::Error.WriteLine("Manifest:     $manifestStatus")
  [Console]::Error.WriteLine("Files:        $fileCount checked")
  if ($errors.Count -gt 0) {
    [Console]::Error.WriteLine('')
    [Console]::Error.WriteLine('Errors:')
    foreach ($e in $errors) { [Console]::Error.WriteLine("  - $e") }
  }
  [Console]::Error.WriteLine('')
  if ($verifyExit -eq 0) {
    [Console]::Error.WriteLine('Result: VERIFY OK - manifest verified')
  } else {
    [Console]::Error.WriteLine('Result: VERIFY FAILED - see errors above')
  }
  exit $verifyExit
}

# ---------------------------------------------------------------------------
# Validate prerequisites
# ---------------------------------------------------------------------------
$exitCode = 0

if (-not (Test-Path $DataDir -PathType Container)) {
  Add-Error "data directory does not exist: $DataDir"
  $exitCode = 1
}

$sqlite3Available = $false
try { & $SqliteBin --version 2>$null | Out-Null; $sqlite3Available = $true } catch {}
if (-not $sqlite3Available) {
  Add-Error "sqlite3 CLI not found: $SqliteBin"
  $exitCode = 1
}

# If prerequisites failed, emit JSON and exit early.
if ($exitCode -ne 0) {
  $endTs = Now-Iso
  $result = [ordered]@{
    tool              = 'backup'
    schema_version    = 1
    start_timestamp   = $startTs
    end_timestamp     = $endTs
    data_dir          = $DataDir
    backup_dir        = $BackupDir
    manager_downtime  = [ordered]@{
      state = 'assumed_stopped'
      note  = 'script does not stop manager; ensure Bun is stopped before running'
    }
    journal_mode      = 'unknown'
    method            = 'none'
    files             = @()
    manifest          = [ordered]@{ path = ''; verified = $false; file_count = 0 }
    metadata          = [ordered]@{ instances_copied = 0; versions_copied = 0 }
    errors            = $errors
    exit_code         = $exitCode
  }
  $result | ConvertTo-Json -Depth 10 -Compress | Write-Output
  [Console]::Error.WriteLine('Backup failed: prerequisites not met')
  exit $exitCode
}

# ---------------------------------------------------------------------------
# Create backup directory
# ---------------------------------------------------------------------------
try {
  New-Item -ItemType Directory -Path $BackupDir -Force -ErrorAction Stop | Out-Null
} catch {
  Add-Error "cannot create backup directory: $BackupDir"
  $exitCode = 1
  $endTs = Now-Iso
  $result = [ordered]@{
    tool              = 'backup'
    schema_version    = 1
    start_timestamp   = $startTs
    end_timestamp     = $endTs
    data_dir          = $DataDir
    backup_dir        = $BackupDir
    manager_downtime  = [ordered]@{
      state = 'assumed_stopped'
      note  = 'script does not stop manager; ensure Bun is stopped before running'
    }
    journal_mode      = 'unknown'
    method            = 'none'
    files             = @()
    manifest          = [ordered]@{ path = ''; verified = $false; file_count = 0 }
    metadata          = [ordered]@{ instances_copied = 0; versions_copied = 0 }
    errors            = $errors
    exit_code         = $exitCode
  }
  $result | ConvertTo-Json -Depth 10 -Compress | Write-Output
  exit $exitCode
}

# ---------------------------------------------------------------------------
# Determine journal mode and choose backup method
# ---------------------------------------------------------------------------
$dbPath = Join-Path $DataDir 'gowa.db'
$journalMode = 'unknown'
$method = 'none'

if (Test-Path $dbPath -PathType Leaf) {
  $journalMode = (& $SqliteBin $dbPath 'PRAGMA journal_mode;' 2>$null | Select-Object -First 1)
  if (-not $journalMode) { $journalMode = 'unknown' }
}

# ---------------------------------------------------------------------------
# Back up the SQLite database
# ---------------------------------------------------------------------------
$dbBackedUp = $false
$dbBackupName = 'gowa.db'

if (Test-Path $dbPath -PathType Leaf) {
  $dbDest = Join-Path $BackupDir $dbBackupName
  if ($journalMode -eq 'wal') {
    # Use SQLite online backup API via the .backup command.
    # Escape single quotes in the path to prevent SQLite command injection.
    $escapedDest = $dbDest -replace "'", "''"
    $backupCmd = ".backup '$escapedDest'"
    $null = & $SqliteBin $dbPath $backupCmd 2>$null
    if ($LASTEXITCODE -eq 0 -and (Test-Path $dbDest)) {
      $method = 'online_backup'
      $dbBackedUp = $true
    } else {
      Add-Error 'SQLite online backup failed'
      $exitCode = 1
    }
  } else {
    # Safe file copy for non-WAL modes.
    try {
      Copy-Item $dbPath $dbDest -Force -ErrorAction Stop
      $method = 'file_copy'
      $dbBackedUp = $true
    } catch {
      Add-Error 'file copy of database failed'
      $exitCode = 1
    }
  }

  if ($dbBackedUp) {
    $dbInfo = Get-Item $dbDest
    $dbSha = Get-Sha256 $dbDest
    Add-File $dbBackupName $dbSha ([long]$dbInfo.Length)
  }
} else {
  Add-Error "database file not found: $dbPath"
  $exitCode = 1
}

# ---------------------------------------------------------------------------
# Copy instance metadata (JSON export - never includes config column)
# ---------------------------------------------------------------------------
$instancesCopied = 0
$instancesMetaName = 'instances.json'
$instancesMetaPath = Join-Path $BackupDir $instancesMetaName

if ($dbBackedUp -or (Test-Path $dbPath -PathType Leaf)) {
  # Export key, name, port, status, gowa_version, created_at, updated_at.
  # Deliberately omit config (may contain tokens) and error_message.
  $query = "SELECT key,name,port,status,gowa_version,created_at,updated_at FROM instances;"
  $rawData = & $SqliteBin -json $dbPath $query 2>$null
  # sqlite3 -json outputs a single JSON document; PowerShell may split it
  # into an array of lines.  Join back into a single string.
  $data = if ($rawData -is [array]) { $rawData -join "`n" } else { [string]$rawData }
  if (-not $data) { $data = '[]' }
  try {
    Set-Content -Path $instancesMetaPath -Value $data -NoNewline -ErrorAction Stop
    $imInfo = Get-Item $instancesMetaPath
    $imSha = Get-Sha256 $instancesMetaPath
    Add-File $instancesMetaName $imSha ([long]$imInfo.Length)
    # Count instances by parsing JSON.
    try {
      $parsed = $data | ConvertFrom-Json -ErrorAction Stop
      if ($parsed -is [array]) { $instancesCopied = $parsed.Count }
      elseif ($parsed) { $instancesCopied = 1 }
    } catch { $instancesCopied = 0 }
  } catch {
    Add-Error 'failed to write instances metadata'
  }
}

# ---------------------------------------------------------------------------
# Copy version metadata (list of installed versions)
# ---------------------------------------------------------------------------
$versionsCopied = 0
$versionsMetaName = 'versions.json'
$versionsMetaPath = Join-Path $BackupDir $versionsMetaName

$versionsDir = Join-Path $DataDir 'bin\versions'
$versionEntries = @()
if (Test-Path $versionsDir -PathType Container) {
  Get-ChildItem $versionsDir -Directory | ForEach-Object {
    $vname = $_.Name
    if ($vname -like '.install-*') { return }
    $gowaPath = Join-Path $_.FullName 'gowa.exe'
    if (-not (Test-Path $gowaPath)) { $gowaPath = Join-Path $_.FullName 'gowa' }
    $vsize = [long]0
    if (Test-Path $gowaPath -PathType Leaf) {
      $vsize = [long](Get-Item $gowaPath).Length
    }
    $versionEntries += @{ version = $vname; path = $gowaPath; size = $vsize }
    $versionsCopied++
  }
}
$versionJson = $versionEntries | ConvertTo-Json -Depth 5 -Compress
if (-not $versionJson) { $versionJson = '[]' }
try {
  Set-Content -Path $versionsMetaPath -Value $versionJson -NoNewline -ErrorAction Stop
  $vmInfo = Get-Item $versionsMetaPath
  $vmSha = Get-Sha256 $versionsMetaPath
  Add-File $versionsMetaName $vmSha ([long]$vmInfo.Length)
} catch {
  Add-Error 'failed to write versions metadata'
}

# ---------------------------------------------------------------------------
# Generate SHA-256 manifest and verify immediately
# ---------------------------------------------------------------------------
$manifestName = 'manifest.sha256'
$manifestPath = Join-Path $BackupDir $manifestName
$manifestVerified = $false
$fileCount = 0

# Build manifest content: "<sha256>  <relative_path>" per line.
$manifestLines = [System.Collections.ArrayList]::new()
foreach ($f in $files) {
  $fullPath = Join-Path $BackupDir $f.path
  $sha = Get-Sha256 $fullPath
  $null = $manifestLines.Add("$sha  $($f.path)")
}

# Write manifest.
try {
  Set-Content -Path $manifestPath -Value $manifestLines -ErrorAction Stop
} catch {
  Add-Error 'failed to write manifest'
}

# Verify manifest: re-hash each file and compare.
if (Test-Path $manifestPath -PathType Leaf) {
  $manifestVerified = $true
  $fileCount = 0
  foreach ($line in (Get-Content $manifestPath)) {
    if ([string]::IsNullOrWhiteSpace($line)) { continue }
    $fileCount++
    $parts = $line -split '  ', 2
    if ($parts.Length -lt 2) {
      $manifestVerified = $false
      Add-Error 'manifest verify: malformed line'
      break
    }
    $expectedSha = $parts[0]
    $relPath = $parts[1].Trim()
    $fullPath = Join-Path $BackupDir $relPath
    if (-not (Test-Path $fullPath -PathType Leaf)) {
      $manifestVerified = $false
      Add-Error "manifest verify: file missing: $relPath"
      break
    }
    $actualSha = Get-Sha256 $fullPath
    if ($expectedSha -ne $actualSha) {
      $manifestVerified = $false
      Add-Error "manifest verify: checksum mismatch for $relPath"
      break
    }
  }
}

if (-not $manifestVerified) { $exitCode = 1 }

# Add manifest itself to the files list.
if (Test-Path $manifestPath -PathType Leaf) {
  $mInfo = Get-Item $manifestPath
  $mSha = Get-Sha256 $manifestPath
  Add-File $manifestName $mSha ([long]$mInfo.Length)
}

$endTs = Now-Iso

# ---------------------------------------------------------------------------
# Emit JSON to stdout
# ---------------------------------------------------------------------------
$result = [ordered]@{
  tool              = 'backup'
  schema_version    = 1
  start_timestamp   = $startTs
  end_timestamp     = $endTs
  data_dir          = $DataDir
  backup_dir        = $BackupDir
  manager_downtime  = [ordered]@{
    state = 'assumed_stopped'
    note  = 'script does not stop manager; ensure Bun is stopped before running'
  }
  journal_mode      = $journalMode
  method            = $method
  files             = $files
  manifest          = [ordered]@{
    path       = $manifestName
    verified   = $manifestVerified
    file_count = $fileCount
  }
  metadata          = [ordered]@{
    instances_copied = $instancesCopied
    versions_copied  = $versionsCopied
  }
  errors            = $errors
  exit_code         = $exitCode
}

$result | ConvertTo-Json -Depth 10 -Compress | Write-Output

# ---------------------------------------------------------------------------
# Human summary to stderr
# ---------------------------------------------------------------------------
[Console]::Error.WriteLine('')
[Console]::Error.WriteLine('=== GOWA Manager Backup ===')
[Console]::Error.WriteLine("Data dir:     $DataDir")
[Console]::Error.WriteLine("Backup dir:   $BackupDir")
[Console]::Error.WriteLine("Journal mode: $journalMode")
[Console]::Error.WriteLine("Method:       $method")
[Console]::Error.WriteLine("Files:        $fileCount backed up")
$manifestStatus = if ($manifestVerified) { 'verified' } else { 'VERIFICATION FAILED' }
[Console]::Error.WriteLine("Manifest:     $manifestStatus")
[Console]::Error.WriteLine("Instances:    $instancesCopied")
[Console]::Error.WriteLine("Versions:     $versionsCopied")
[Console]::Error.WriteLine("Start:        $startTs")
[Console]::Error.WriteLine("End:          $endTs")
if ($errors.Count -gt 0) {
  [Console]::Error.WriteLine('')
  [Console]::Error.WriteLine('Errors:')
  foreach ($e in $errors) { [Console]::Error.WriteLine("  - $e") }
}
[Console]::Error.WriteLine('')
if ($exitCode -eq 0) {
  [Console]::Error.WriteLine('Result: BACKUP OK - manifest verified')
} else {
  [Console]::Error.WriteLine('Result: BACKUP FAILED - see errors above')
}

exit $exitCode
