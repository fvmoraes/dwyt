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
  Copy-Item -Path $exe.FullName -Destination $dest -Force
  Write-Ok "Installed: $dest"

  # Add bin dir to the user PATH (persistent) if missing.
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (($userPath -split ';') -notcontains $binDir) {
    Write-Step "Adding $binDir to your user PATH ..."
    $newPath = if ([string]::IsNullOrEmpty($userPath)) { $binDir } else { "$userPath;$binDir" }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    $env:Path = "$env:Path;$binDir"   # current session
    Write-Ok "PATH updated (restart terminals to pick it up)."
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
