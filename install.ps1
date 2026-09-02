#Requires -Version 5.1
<#
.SYNOPSIS
  DWYT - Don't Waste Your Tokens - native Windows installer (PowerShell).

.DESCRIPTION
  Downloads the dwyt binary for your architecture, verifies its SHA-256
  checksum, installs it under %APPDATA%\dwyt\bin, adds that folder to your
  user PATH, installs the DWYT tools (Codebase MCP, Obsidian MCP, Headroom),
  and starts the dashboard daemon. No Git Bash / WSL required.

.EXAMPLE
  irm https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.ps1 | iex

.EXAMPLE
  .\install.ps1 -SkipDeps      # install only the dwyt binary
#>
[CmdletBinding()]
param(
  [switch]$SkipDeps,
  [string]$Repo = "fvmoraes/dwyt"
)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Write-Step($msg) { Write-Host "  -> $msg" -ForegroundColor Yellow }
function Write-Ok($msg)   { Write-Host "  [ok] $msg" -ForegroundColor Green }
function Write-Err($msg)  { Write-Host "  [x] $msg" -ForegroundColor Red }

function Get-Arch {
  switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { return "amd64" }
    "ARM64" { return "arm64" }
    default { return "amd64" }
  }
}

# Test-DwytDaemonProcess deliberately validates both the executable path and
# the `daemon` argument before taskkill is used. PID files can become stale and
# Windows can reuse a PID, so a numeric match alone is not safe enough.
function Test-DwytDaemonProcess {
  param(
    [Parameter(Mandatory = $true)]$Process,
    [Parameter(Mandatory = $true)][string]$DaemonPath
  )

  if ([string]::IsNullOrWhiteSpace($Process.ExecutablePath) -or [string]::IsNullOrWhiteSpace($Process.CommandLine)) {
    return $false
  }
  try {
    $actualPath = [IO.Path]::GetFullPath([string]$Process.ExecutablePath)
    $expectedPath = [IO.Path]::GetFullPath($DaemonPath)
  }
  catch {
    return $false
  }
  if (-not [string]::Equals($actualPath, $expectedPath, [StringComparison]::OrdinalIgnoreCase)) {
    return $false
  }
  return [string]$Process.CommandLine -match '(?i)(^|\s)daemon(\s|$)'
}

# Stop-DwytDaemon stops only the daemon launched from the binary that this
# installer is about to replace. taskkill /T is essential: Headroom can spawn
# children of the daemon and Windows otherwise leaves them orphaned. The
# operation is idempotent: missing/stale PID files and already-exited daemons
# are both successful no-ops.
function Stop-DwytDaemon {
  param(
    [Parameter(Mandatory = $true)][string]$DaemonPath,
    [Parameter(Mandatory = $true)][string]$DwytHome
  )

  if (-not (Test-Path -LiteralPath $DaemonPath)) { return }

  $pidFile = Join-Path $DwytHome "run\daemon.pid"
  $candidatePids = @()
  if (Test-Path -LiteralPath $pidFile) {
    $pidText = [string](Get-Content -LiteralPath $pidFile -Raw -ErrorAction SilentlyContinue)
    $pidText = $pidText.Trim()
    if ($pidText -match '^\d+$') {
      $candidatePids += [int]$pidText
    }
  }

  # The PID file is the primary source. Enumerating the exact executable path
  # also repairs the rare case where a previous release crashed before writing
  # it, without ever touching a different application's process.
  try {
    $running = Get-CimInstance -ClassName Win32_Process -Filter "Name = 'dwyt.exe'" -ErrorAction Stop
    foreach ($process in $running) {
      if (Test-DwytDaemonProcess -Process $process -DaemonPath $DaemonPath) {
        $candidatePids += [int]$process.ProcessId
      }
    }
  }
  catch {
    # A constrained PowerShell session may deny CIM inspection. The PID-file
    # path below still works when it can be validated, otherwise Copy-Item
    # reports the locked target instead of risking an unrelated process.
  }

  foreach ($daemonPid in ($candidatePids | Select-Object -Unique)) {
    try {
      $process = Get-CimInstance -ClassName Win32_Process -Filter "ProcessId = $daemonPid" -ErrorAction Stop
    }
    catch {
      continue
    }
    if ($null -eq $process) {
      continue
    }
    if (-not (Test-DwytDaemonProcess -Process $process -DaemonPath $DaemonPath)) {
      continue
    }

    Write-Step "Stopping running DWYT daemon (PID $daemonPid) ..."
    & taskkill.exe /F /T /PID $daemonPid | Out-Null
    if ($LASTEXITCODE -ne 0) {
      throw "could not stop the running DWYT daemon (PID $daemonPid) before updating"
    }

    $deadline = [DateTime]::UtcNow.AddSeconds(10)
    do {
      Start-Sleep -Milliseconds 200
      try {
        $stillRunning = Get-CimInstance -ClassName Win32_Process -Filter "ProcessId = $daemonPid" -ErrorAction Stop
      }
      catch {
        $stillRunning = $null
      }
    } while ($stillRunning -and [DateTime]::UtcNow -lt $deadline)
    if ($stillRunning) {
      throw "DWYT daemon (PID $daemonPid) did not exit before updating"
    }
  }

  # It is safe to clear a stale DWYT daemon record after the process check.
  Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "  DWYT - Don't Waste Your Tokens (Windows installer)" -ForegroundColor Cyan
Write-Host ""

$arch        = Get-Arch
$archiveName = "dwyt_windows_$arch.zip"
$baseUrl     = "https://github.com/$Repo/releases/latest/download"
$archiveUrl  = "$baseUrl/$archiveName"
$checksumUrl = "$baseUrl/checksums.txt"

# Layout mirrors detect.go / platform.go: %APPDATA%\dwyt
$dwytHome = Join-Path $env:APPDATA "dwyt"
$binDir   = Join-Path $dwytHome "bin"
$null = New-Item -ItemType Directory -Force -Path $binDir
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("dwyt-" + [Guid]::NewGuid().ToString("N"))
$null = New-Item -ItemType Directory -Force -Path $tmp

try {
  $archivePath  = Join-Path $tmp $archiveName
  $checksumPath = Join-Path $tmp "checksums.txt"

  Write-Step "Downloading $archiveName ..."
  Invoke-WebRequest -Uri $archiveUrl -OutFile $archivePath -UseBasicParsing

  Write-Step "Verifying SHA-256 checksum ..."
  Invoke-WebRequest -Uri $checksumUrl -OutFile $checksumPath -UseBasicParsing
  $expected = (Select-String -Path $checksumPath -Pattern ([regex]::Escape($archiveName)) |
               Select-Object -First 1).Line -split '\s+' | Select-Object -First 1
  if (-not $expected) { throw "checksum for $archiveName not found in checksums.txt" }
  $actual = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLower()
  if ($actual -ne $expected.ToLower()) {
    throw "checksum mismatch (expected $expected, got $actual)"
  }
  Write-Ok "Checksum verified."

  Write-Step "Extracting ..."
  Expand-Archive -Path $archivePath -DestinationPath $tmp -Force
  $exe = Get-ChildItem -Path $tmp -Recurse -Filter "dwyt.exe" | Select-Object -First 1
  if (-not $exe) { throw "dwyt.exe not found in archive" }

  $dest = Join-Path $binDir "dwyt.exe"
  Stop-DwytDaemon -DaemonPath $dest -DwytHome $dwytHome
  Copy-Item -Path $exe.FullName -Destination $dest -Force
  Write-Ok "Installed: $dest"

  # Persist bin dir on the user PATH (registry) so future terminals see it.
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (($userPath -split ';') -notcontains $binDir) {
    Write-Step "Adding $binDir to your user PATH ..."
    $newPath = if ([string]::IsNullOrEmpty($userPath)) { $binDir } else { "$userPath;$binDir" }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Ok "PATH updated (new terminals pick it up automatically)."
  }
  # Always expose it in THIS session too, so `dwyt` works immediately — even on
  # a re-install where the registry PATH already contained it (otherwise the
  # current shell, opened before the first install, never sees the new entry).
  if (($env:Path -split ';') -notcontains $binDir) {
    $env:Path = "$binDir;$env:Path"
  }

  $version = & $dest version 2>&1
  Write-Ok "Binary works: $version"

  if (-not $SkipDeps) {
    Write-Step "Installing DWYT tools (this may take a few minutes) ..."
    & $dest install
  } else {
    Write-Host "  (skipping dependency install; run 'dwyt install' later)" -ForegroundColor DarkGray
  }

  Write-Host ""
  Write-Ok "DWYT installed."
  Write-Host "  Next: open a project folder and run 'dwyt .'" -ForegroundColor Cyan
  Write-Host "  (Already-open terminals from before the install need a restart to find 'dwyt'.)" -ForegroundColor DarkGray
  Write-Host "  The dashboard opens at http://localhost:2737" -ForegroundColor Cyan
  Write-Host ""
}
catch {
  Write-Err $_.Exception.Message
  Write-Host "  See https://github.com/$Repo for manual install steps." -ForegroundColor DarkGray
  exit 1
}
finally {
  Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
