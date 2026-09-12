[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string]$RepositoryRoot
)

# Hermetic Windows install → reinstall → uninstall smoke. Every path is below
# one temporary root; -NoPath and -BinaryPath prevent registry writes and all
# release-network access.
$ErrorActionPreference = "Stop"
$sandbox = Join-Path ([IO.Path]::GetTempPath()) ("dwyt-install-e2e-" + [Guid]::NewGuid().ToString("N"))

try {
  $sandboxHome = Join-Path $sandbox "home"
  $appData = Join-Path $sandboxHome "AppData\Roaming"
  $dwytHome = Join-Path $appData "dwyt"
  $binDir = Join-Path $dwytHome "bin"
  $sourceBinary = Join-Path $sandbox "source\dwyt.exe"
  $launcher = Join-Path $binDir "dwyt.exe"
  $vault = Join-Path $dwytHome "projects\example\vault.md"
  $managed = Join-Path $dwytHome "cache\managed.txt"
  $externalConfig = Join-Path $sandboxHome "config\unmanaged.txt"

  $null = New-Item -ItemType Directory -Force -Path (Split-Path $sourceBinary -Parent)

  # Build with the runner's real profile. On Windows, overriding HOME and
  # USERPROFILE before Go starts can redirect its cache/config discovery into
  # the uninitialized sandbox profile. The installer and launched binary below
  # remain hermetic after these environment variables are scoped to the test.
  Push-Location (Join-Path $RepositoryRoot "core")
  try {
    & go build -o $sourceBinary .
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
  }
  finally {
    Pop-Location
  }

  $env:HOME = $sandboxHome
  $env:USERPROFILE = $sandboxHome
  $env:APPDATA = $appData
  $env:DWYT_HOME = $dwytHome

  $installer = Join-Path $RepositoryRoot "install.ps1"
  & $installer -SkipDeps -DwytHome $dwytHome -BinaryPath $sourceBinary -NoPath
  if ($LASTEXITCODE -ne 0) { throw "first installer invocation failed with exit code $LASTEXITCODE" }
  if (-not (Test-Path -LiteralPath $launcher)) { throw "installer did not create $launcher" }
  & $launcher version | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "installed launcher did not run" }

  foreach ($entry in @(
    @{ Path = $vault; Content = "user vault must survive" },
    @{ Path = $managed; Content = "managed cache can be removed" },
    @{ Path = $externalConfig; Content = "external config must survive" }
  )) {
    $null = New-Item -ItemType Directory -Force -Path (Split-Path $entry.Path -Parent)
    Set-Content -LiteralPath $entry.Path -Value $entry.Content -NoNewline
  }

  # Reinstall from the same local fixture, proving existing vaults are not a
  # prerequisite for touching release/network/global user state.
  & $installer -SkipDeps -DwytHome $dwytHome -BinaryPath $sourceBinary -NoPath
  if ($LASTEXITCODE -ne 0) { throw "second installer invocation failed with exit code $LASTEXITCODE" }
  if ((Get-Content -LiteralPath $vault -Raw) -ne "user vault must survive") { throw "installer overwrote vault data" }

  & $launcher reinstall | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "reinstall command failed with exit code $LASTEXITCODE" }
  if (-not (Test-Path -LiteralPath $vault)) { throw "reinstall removed vault data" }
  if (Test-Path -LiteralPath $managed) { throw "reinstall kept managed cache" }
  if (-not (Test-Path -LiteralPath $externalConfig)) { throw "reinstall touched external config" }

  # --sandbox avoids pkill/taskkill fallbacks, pip, registry, profile and
  # global binary cleanup. Windows cannot delete a running dwyt.exe, so the
  # assertion is intentionally data-safety focused rather than self-deletion.
  & $launcher uninstall --sandbox --sandbox-root $sandbox --install-dir $binDir | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "sandbox uninstall failed with exit code $LASTEXITCODE" }
  if ((Get-Content -LiteralPath $vault -Raw) -ne "user vault must survive") { throw "uninstall removed vault data" }
  if (-not (Test-Path -LiteralPath $externalConfig)) { throw "uninstall touched external config" }
  if (Test-Path -LiteralPath $managed) { throw "uninstall kept managed cache" }

  Write-Host "installer lifecycle sandbox: PASS"
}
finally {
  if (Test-Path -LiteralPath $sandbox) {
    Remove-Item -LiteralPath $sandbox -Recurse -Force -ErrorAction SilentlyContinue
  }
}
