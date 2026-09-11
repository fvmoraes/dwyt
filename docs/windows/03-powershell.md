# DWYT + PowerShell

DWYT's installer and CLI are designed for PowerShell (5.1+ and 7+). No Git Bash
or Unix shell is required.

## Common commands

```powershell
dwyt .                       # start the dashboard for the current project
dwyt status                  # quick status of all tools
dwyt install                 # install / repair tools (native, no shell)
dwyt install --tools=cbmcp   # install a subset
dwyt stop                    # stop the daemon and services
dwyt sync mcp                # re-sync MCP configs for AI clients
dwyt version
```

## Add DWYT helpers to your PowerShell profile

Open your profile:

```powershell
notepad $PROFILE
```

Add shortcuts:

```powershell
function dw  { dwyt . }
function dws { dwyt status }
function dwx { dwyt stop }
```

Reload:

```powershell
. $PROFILE
```

## Environment variables

DWYT respects these (set per-session or in `$PROFILE`):

```powershell
$env:DWYT_HOME = "D:\dwyt"        # override the data dir (default %APPDATA%\dwyt)
$env:DWYT_HEADROOM_PORT = "8788"  # change the Headroom proxy port
```

## Paths use Windows conventions

DWYT stores everything under `%APPDATA%\dwyt`
(`C:\Users\<user>\AppData\Roaming\dwyt`). To open it:

```powershell
ii "$env:APPDATA\dwyt"
```

## Execution policy

The `irm ... | iex` one-liner runs without changing policy. To run a saved
`.ps1` once without altering machine settings:

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1
```
