<#
.SYNOPSIS
  GOWA Manager - Go cutover preflight checks (PowerShell).

.DESCRIPTION
  Verifies that the environment is safe for switching from the Bun backend
  to the Go backend.  Produces machine-readable JSON on stdout and a concise
  human summary on stderr.  Exits non-zero when any blocker is found.

  Secrets (passwords, instance config, webhook URLs) are NEVER printed.
  Only structural metadata (paths, sizes, counts, status strings) is emitted.

.PARAMETER Binary
  Path to the Go manager binary to verify.

.PARAMETER DataDir
  Data directory (default: ./data).

.PARAMETER Port
  Manager HTTP port (default: 3000).

.PARAMETER BackupDir
  Backup destination directory (default: ./backup).

.PARAMETER SqliteBin
  Path to sqlite3 CLI (default: sqlite3 from PATH).
#>
[CmdletBinding()]
param(
  [Alias('b')]
  [string]$Binary = '',

  [Alias('d')]
  [string]$DataDir = './data',

  [Alias('p')]
  [int]$Port = 3000,

  [string]$BackupDir = './backup',

  [string]$SqliteBin = 'sqlite3',

  [long]$MinSpace = 10485760
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

function Test-PortAvailable([int]$PortNum) {
  try {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $PortNum)
    $listener.Start()
    $listener.Stop()
    return $true
  } catch {
    return $false
  }
}

function Get-FreeBytes([string]$Path) {
  try {
    $di = [System.IO.DriveInfo]::new((Get-Item $Path).Root.FullName)
    return [long]$di.AvailableFreeSpace
  } catch {
    try {
      $root = (Get-Item $Path -ErrorAction Stop).Root.FullName
      $di = [System.IO.DriveInfo]::new($root)
      return [long]$di.AvailableFreeSpace
    } catch {
      return [long]0
    }
  }
}

# ---------------------------------------------------------------------------
# Accumulators
# ---------------------------------------------------------------------------
$checks = [System.Collections.ArrayList]::new()
$blockers = [System.Collections.ArrayList]::new()
$warnings = [System.Collections.ArrayList]::new()

function Add-Check([string]$Name, [string]$Status, [string]$Detail) {
  $null = $checks.Add(@{ name = $Name; status = $Status; detail = $Detail })
}
function Add-Blocker([string]$Msg) { $null = $blockers.Add($Msg) }
function Add-Warning([string]$Msg) { $null = $warnings.Add($Msg) }

# ---------------------------------------------------------------------------
# Check 1: OS / architecture
# ---------------------------------------------------------------------------
$osName = & { if ($PSVersionTable.Platform -eq 'Unix') { 'Linux' } elseif ($IsWindows) { 'windows' } else { 'unknown' } }
# On Windows PowerShell 5.1, $IsWindows may not be set.
if (-not $IsWindows -and -not $PSVersionTable.ContainsKey('Platform')) { $osName = 'windows' }
if ($PSVersionTable.ContainsKey('Platform') -and $PSVersionTable.Platform -eq 'Win') { $osName = 'windows' }
if ($PSVersionTable.ContainsKey('Platform') -and $PSVersionTable.Platform -eq 'Unix') {
  # Distinguish Linux from macOS
  $unameS = (uname -s 2>$null) 
  if ($unameS -eq 'Darwin') { $osName = 'Darwin' } else { $osName = 'Linux' }
}

$archName = & {
  if ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture -band 0) {}
  $a = [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString()
  switch ($a) {
    'X64'   { 'amd64'; break }
    'Arm64' { 'arm64'; break }
    'X86'   { '386'; break }
    default { $a.ToLower() }
  }
}
# Fallback for older PowerShell
if (-not $archName) {
  $envArch = $env:PROCESSOR_ARCHITECTURE
  switch ($envArch) {
    'AMD64' { $archName = 'amd64' }
    'ARM64' { $archName = 'arm64' }
    default { $archName = $envArch }
  }
}

$supportedOs = @('Linux', 'Darwin', 'windows')
$supportedArch = @('amd64', 'arm64')
if ($supportedOs -contains $osName -and $supportedArch -contains $archName) {
  Add-Check 'os_arch' 'pass' "$osName/$archName"
} else {
  Add-Check 'os_arch' 'fail' "$osName/$archName not supported"
  Add-Blocker "unsupported OS/arch: $osName/$archName"
}

# ---------------------------------------------------------------------------
# Check 2: Binary exists / is executable / reports version
# ---------------------------------------------------------------------------
$binExists = $false
$binExec = $false
$binVersion = ''
$binChecksum = ''
if ($Binary -and (Test-Path $Binary -PathType Leaf)) {
  $binExists = $true
  $binExec = $true  # On Windows, executability is implied for .exe
  try {
    $out = & $Binary --version 2>$null | Select-Object -First 1
    $binVersion = $out
  } catch { $binVersion = '' }
}
if ($binExists -and $binExec -and $binVersion) {
  Add-Check 'binary' 'pass' $binVersion
} else {
  $detail = if ($Binary) { 'binary missing or not executable' } else { 'no binary path provided' }
  Add-Check 'binary' 'fail' $detail
  Add-Blocker 'manager binary not usable'
}

# Binary checksum (informational): record the SHA-256 of the manager binary.
# The check passes as long as a checksum can be computed; it fails (blocker)
# if the file cannot be read for hashing.  There is no reference checksum to
# compare against at preflight time.
if ($binExists) {
  $binChecksum = Get-Sha256 $Binary
  if ($binChecksum -and $binChecksum -ne 'error') {
    Add-Check 'binary_checksum' 'pass' $binChecksum
  } else {
    $binChecksum = ''
    Add-Check 'binary_checksum' 'fail' 'could not compute checksum of binary'
    Add-Blocker 'manager binary checksum could not be computed'
  }
} else {
  Add-Check 'binary_checksum' 'fail' 'binary not present - no checksum'
}

# ---------------------------------------------------------------------------
# Check 3: Data directory exists and free space
# ---------------------------------------------------------------------------
$ddExists = (Test-Path $DataDir -PathType Container)
$ddFreeBytes = [long]0
if ($ddExists) {
  $ddFreeBytes = Get-FreeBytes $DataDir
}
if ($ddExists) {
  if ($ddFreeBytes -lt $MinSpace) {
    Add-Check 'data_dir_space' 'fail' "only $ddFreeBytes bytes free (< $MinSpace required)"
    Add-Blocker 'insufficient free space in data dir'
  } else {
    Add-Check 'data_dir_space' 'pass' "$ddFreeBytes bytes free"
  }
} else {
  Add-Check 'data_dir_space' 'fail' 'data dir does not exist'
  Add-Blocker 'data directory not found'
}

# ---------------------------------------------------------------------------
# Check 4: Read / write / execute permissions on data dir
# ---------------------------------------------------------------------------
$permRead = $false; $permWrite = $false; $permExecute = $false
if ($ddExists) {
  $item = Get-Item $DataDir
  # On Windows, check via .NET; on Unix, use test flags.
  if ($PSVersionTable.ContainsKey('Platform') -and $PSVersionTable.Platform -eq 'Unix') {
    $permRead = [bool](test -r $DataDir 2>$null)
    $permWrite = [bool](test -w $DataDir 2>$null)
    $permExecute = [bool](test -x $DataDir 2>$null)
  } else {
    # Windows: attempt actual r/w operations.
    $permRead = $true
    $permExecute = $true
    $testFile = Join-Path $DataDir '.preflight_write_test'
    try {
      Set-Content -Path $testFile -Value 'x' -ErrorAction Stop
      Remove-Item $testFile -Force -ErrorAction SilentlyContinue
      $permWrite = $true
    } catch {
      $permWrite = $false
    }
  }
}
if ($permRead -and $permWrite -and $permExecute) {
  Add-Check 'permissions' 'pass' 'rwx ok'
} else {
  Add-Check 'permissions' 'fail' "r=$permRead w=$permWrite x=$permExecute"
  Add-Blocker 'data dir lacks required rwx permissions'
}

# ---------------------------------------------------------------------------
# Check 5: Absence of active Bun / Go manager process
# ---------------------------------------------------------------------------
$mgrBun = $false
$mgrGo = $false
$procs = Get-Process -ErrorAction SilentlyContinue
foreach ($p in $procs) {
  $name = $p.ProcessName
  $cmd = ''
  try { $cmd = $p.Path } catch {}
  if ($name -match 'gowa-manager' -or ($cmd -and $cmd -match 'gowa-manager')) { $mgrGo = $true }
  if ($name -match '^bun$' -or ($cmd -and $cmd -match 'bun')) { $mgrBun = $true }
}
if ($mgrBun -or $mgrGo) {
  $d = @()
  if ($mgrBun) { $d += 'bun active' }
  if ($mgrGo)  { $d += 'go active' }
  Add-Check 'manager_active' 'fail' ($d -join ' ')
  Add-Blocker 'a manager process is still running'
} else {
  Add-Check 'manager_active' 'pass' 'no manager process detected'
}

# ---------------------------------------------------------------------------
# Check 6: Manager lock not held
# ---------------------------------------------------------------------------
$lockPath = Join-Path $DataDir '.gowa-manager.lock'
$lockHeld = $false
if (Test-Path $lockPath -PathType Leaf) {
  # Try to open the file exclusively — if it fails, it's likely locked.
  try {
    $fs = [System.IO.File]::Open($lockPath, 'Open', 'ReadWrite', 'None')
    $fs.Close()
    $lockHeld = $false
  } catch {
    $lockHeld = $true
  }
}
if ($lockHeld) {
  Add-Check 'lock' 'fail' "lock file held: $lockPath"
  Add-Blocker 'manager lock is held by another process'
} else {
  Add-Check 'lock' 'pass' 'lock not held'
}

# ---------------------------------------------------------------------------
# Check 7: HTTP port available
# ---------------------------------------------------------------------------
$portAvailable = Test-PortAvailable $Port
if ($portAvailable) {
  Add-Check 'port' 'pass' "port $Port available"
} else {
  Add-Check 'port' 'fail' "port $Port occupied"
  Add-Blocker "HTTP port $Port is occupied"
}

# ---------------------------------------------------------------------------
# Check 8 & 9: SQLite integrity and required columns
# ---------------------------------------------------------------------------
$dbPath = Join-Path $DataDir 'gowa.db'
$sqliteExists = (Test-Path $dbPath -PathType Leaf)
$sqliteIntegrity = 'skipped'
$sqliteJournal = 'unknown'
$columnsPresent = @()
$columnsMissing = @()

$sqlite3Available = $false
try { & $SqliteBin --version 2>$null | Out-Null; $sqlite3Available = $true } catch {}

if ($sqliteExists -and $sqlite3Available) {
  $sqliteIntegrity = (& $SqliteBin $dbPath 'PRAGMA integrity_check;' 2>$null | Select-Object -First 1)
  if (-not $sqliteIntegrity) { $sqliteIntegrity = 'error' }
  $sqliteJournal = (& $SqliteBin $dbPath 'PRAGMA journal_mode;' 2>$null | Select-Object -First 1)
  if (-not $sqliteJournal) { $sqliteJournal = 'unknown' }

  if ($sqliteIntegrity -eq 'ok') {
    Add-Check 'sqlite_integrity' 'pass' 'ok'
  } else {
    Add-Check 'sqlite_integrity' 'fail' $sqliteIntegrity
    Add-Blocker 'SQLite integrity check failed'
  }

  $requiredCols = @('id','key','name','port','status','config','gowa_version','created_at','updated_at','error_message')
  $tableInfo = & $SqliteBin $dbPath 'PRAGMA table_info(instances);' 2>$null
  $existingCols = @()
  foreach ($line in $tableInfo) {
    $parts = $line -split '\|'
    if ($parts.Length -ge 2) { $existingCols += $parts[1] }
  }
  foreach ($col in $requiredCols) {
    if ($existingCols -contains $col) {
      $columnsPresent += $col
    } else {
      $columnsMissing += $col
    }
  }
  if ($columnsMissing.Count -gt 0) {
    Add-Check 'columns' 'fail' 'missing columns'
    Add-Blocker 'instances table missing required columns'
  } else {
    Add-Check 'columns' 'pass' 'all required columns present'
  }
} elseif ($sqliteExists -and -not $sqlite3Available) {
  Add-Check 'sqlite_integrity' 'warn' 'sqlite3 CLI not found - cannot verify'
  Add-Warning 'sqlite3 CLI not available; integrity and column checks skipped'
} else {
  Add-Check 'sqlite_integrity' 'warn' 'database file does not exist yet'
  Add-Warning 'no existing database - fresh install'
}

# ---------------------------------------------------------------------------
# Check 10: Installed GOWA binaries and execute permission
# ---------------------------------------------------------------------------
$gowaBinaries = @()
$versionsDir = Join-Path $DataDir 'bin\versions'
if (Test-Path $versionsDir -PathType Container) {
  Get-ChildItem $versionsDir -Directory | ForEach-Object {
    $vname = $_.Name
    if ($vname -like '.install-*') { return }
    $gowaPath = Join-Path $_.FullName 'gowa.exe'
    if (-not (Test-Path $gowaPath)) { $gowaPath = Join-Path $_.FullName 'gowa' }
    $gowaExists = (Test-Path $gowaPath -PathType Leaf)
    $gowaExec = $false
    if ($gowaExists) {
      # On Windows, .exe is executable; on Unix, check the executable bit.
      if ($PSVersionTable.ContainsKey('Platform') -and $PSVersionTable.Platform -eq 'Unix') {
        $gowaExec = [bool](test -x $gowaPath 2>$null)
      } else {
        $gowaExec = $true
      }
    }
    $gowaBinaries += @{ version = $vname; path = $gowaPath; exists = $gowaExists; executable = $gowaExec }
    if (-not $gowaExists) {
      Add-Check "gowa_binary_$vname" 'fail' "binary missing for $vname"
      Add-Blocker "GOWA binary missing for version $vname"
    } elseif (-not $gowaExec) {
      Add-Check "gowa_binary_$vname" 'fail' "not executable: $vname"
      Add-Blocker "GOWA binary not executable for version $vname"
    }
  }
}
if ($gowaBinaries.Count -eq 0) {
  Add-Check 'gowa_binaries' 'warn' "no installed GOWA versions found"
  Add-Warning "no GOWA binaries installed under $versionsDir"
} else {
  Add-Check 'gowa_binaries' 'pass' 'installed versions verified'
}

# ---------------------------------------------------------------------------
# Check 11: Backup destination writable
# ---------------------------------------------------------------------------
$bkExists = (Test-Path $BackupDir -PathType Container)
$bkWritable = $false
if (-not $bkExists) {
  try { New-Item -ItemType Directory -Path $BackupDir -Force -ErrorAction Stop | Out-Null; $bkExists = $true } catch {}
}
if ($bkExists) {
  $testFile = Join-Path $BackupDir '.preflight_write_test'
  try {
    Set-Content -Path $testFile -Value 'x' -ErrorAction Stop
    Remove-Item $testFile -Force -ErrorAction SilentlyContinue
    $bkWritable = $true
  } catch { $bkWritable = $false }
}
if ($bkWritable) {
  Add-Check 'backup_destination' 'pass' 'writable'
} else {
  Add-Check 'backup_destination' 'fail' "not writable: $BackupDir"
  Add-Blocker 'backup destination not writable'
}

# ---------------------------------------------------------------------------
# Check 12: Child-process / port inventory (running instances from DB)
# ---------------------------------------------------------------------------
$childProcesses = @()
if ($sqliteExists -and $sqlite3Available) {
  # Never select config (may contain tokens).
  $query = "SELECT key,name,port,status FROM instances WHERE status='running';"
  $rows = & $SqliteBin $dbPath $query 2>$null
  foreach ($line in $rows) {
    $parts = $line -split '\|'
    if ($parts.Length -ge 4) {
      $childProcesses += @{
        key = $parts[0]
        name = $parts[1]
        port = [int]$parts[2]
        status = $parts[3]
      }
    }
  }
}
if ($childProcesses.Count -gt 0) {
  Add-Check 'child_processes' 'warn' "$($childProcesses.Count) running instance(s) - stop before cutover"
  Add-Warning "$($childProcesses.Count) running instance(s) detected - must be stopped before cutover"
} else {
  Add-Check 'child_processes' 'pass' 'no running instances'
}

# ---------------------------------------------------------------------------
# Determine exit code
# ---------------------------------------------------------------------------
$exitCode = if ($blockers.Count -gt 0) { 1 } else { 0 }

# ---------------------------------------------------------------------------
# Emit JSON to stdout
# ---------------------------------------------------------------------------
$result = [ordered]@{
  tool             = 'preflight'
  schema_version   = 1
  timestamp        = Now-Iso
  os               = $osName
  arch             = $archName
  binary           = [ordered]@{
    path            = $Binary
    exists          = $binExists
    executable      = $binExec
    version         = $binVersion
    binary_checksum = $binChecksum
  }
  data_dir         = [ordered]@{
    path       = $DataDir
    exists     = $ddExists
    free_bytes = $ddFreeBytes
  }
  permissions      = [ordered]@{
    read    = $permRead
    write   = $permWrite
    execute = $permExecute
  }
  manager_active   = [ordered]@{
    bun = $mgrBun
    go  = $mgrGo
  }
  lock             = [ordered]@{
    path = $lockPath
    held = $lockHeld
  }
  port             = [ordered]@{
    number    = $Port
    available = $portAvailable
  }
  sqlite           = [ordered]@{
    path         = $dbPath
    exists       = $sqliteExists
    integrity    = $sqliteIntegrity
    journal_mode = $sqliteJournal
  }
  columns          = [ordered]@{
    present = $columnsPresent
    missing = $columnsMissing
  }
  gowa_binaries    = $gowaBinaries
  backup_destination = [ordered]@{
    path     = $BackupDir
    exists   = $bkExists
    writable = $bkWritable
  }
  child_processes  = $childProcesses
  checks           = $checks
  blockers         = $blockers
  warnings         = $warnings
  exit_code        = $exitCode
}

$json = $result | ConvertTo-Json -Depth 10 -Compress
Write-Output $json

# ---------------------------------------------------------------------------
# Human summary to stderr
# ---------------------------------------------------------------------------
[Console]::Error.WriteLine('')
[Console]::Error.WriteLine('=== GOWA Manager Preflight ===')
[Console]::Error.WriteLine("OS/Arch:      $osName/$archName")
[Console]::Error.WriteLine("Binary:       $Binary ($binVersion)")
if ($binChecksum) {
  [Console]::Error.WriteLine("Checksum:     $binChecksum")
}
[Console]::Error.WriteLine("Data dir:     $DataDir ($ddFreeBytes bytes free)")
$portStatus = if ($portAvailable) { 'available' } else { 'occupied' }
[Console]::Error.WriteLine("Port:         $Port ($portStatus)")
[Console]::Error.WriteLine("SQLite:       $sqliteIntegrity (journal: $sqliteJournal)")
$lockStatus = if ($lockHeld) { 'HELD' } else { 'free' }
[Console]::Error.WriteLine("Lock:         $lockStatus")
$bkStatus = if ($bkWritable) { 'writable' } else { 'NOT writable' }
[Console]::Error.WriteLine("Backup dest:  $BackupDir ($bkStatus)")

if ($blockers.Count -gt 0) {
  [Console]::Error.WriteLine('')
  [Console]::Error.WriteLine('BLOCKERS:')
  foreach ($b in $blockers) { [Console]::Error.WriteLine("  - $b") }
}
if ($warnings.Count -gt 0) {
  [Console]::Error.WriteLine('')
  [Console]::Error.WriteLine('Warnings:')
  foreach ($w in $warnings) { [Console]::Error.WriteLine("  - $w") }
}
[Console]::Error.WriteLine('')
if ($exitCode -eq 0) {
  [Console]::Error.WriteLine('Result: PASS - environment ready for cutover')
} else {
  [Console]::Error.WriteLine('Result: FAIL - resolve blockers before cutover')
}

exit $exitCode
