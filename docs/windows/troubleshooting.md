# DWYT on Windows — Troubleshooting

## `dwyt` is not recognized

The installer adds `%APPDATA%\dwyt\bin` to your **user PATH**, but open terminals
don't pick it up until restarted. Close and reopen Windows Terminal, or run:

```powershell
$env:Path += ";$env:APPDATA\dwyt\bin"
```

## Script execution is blocked

If running `.\install.ps1` fails with an execution-policy error, allow scripts
for the current process only (does not change machine policy):

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

The `irm | iex` one-liner is not affected by execution policy.

## The dashboard didn't open

Check the daemon and logs:

```powershell
dwyt status
Get-Content "$env:APPDATA\dwyt\dwyt.log" -Tail 50
```

Restart it:

```powershell
dwyt stop
dwyt .
```

## `daemon healthcheck timeout`

DWYT probes the dashboard immediately and then every 500 ms. On Windows the
total startup budget is **120 seconds** (60 seconds on Linux/macOS); an HTTP
request is limited to 2 seconds and cannot extend that overall deadline. The
same budget is used while managed services such as Headroom start.

If this machine needs a longer one-time budget, set a positive value in seconds
before running DWYT:

```powershell
$env:DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS = '180'
dwyt .
```

To make it the default for new terminals, set a user environment variable and
restart Windows Terminal:

```powershell
[Environment]::SetEnvironmentVariable('DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS', '180', 'User')
```

On expiry, `%APPDATA%\dwyt\dwyt.log` records the tested URL, child PID, last
HTTP/connection error, and elapsed wait. DWYT then uses `taskkill /F /T` to
terminate the daemon/service tree, including Headroom's `.bat` launcher and
Python descendants, so a failed startup should not leave orphan processes.

For Headroom, a `GET /health` response with HTTP 200 is ready. An optional
degraded field such as `kompress` does not make the proxy unready.

## Headroom selected another port

Headroom requests port 8787 first. If it is in use, DWYT tries 8788 through
8791 and publishes the first free port to the dashboard, runtime state, and
`%APPDATA%\dwyt\env.ps1`. A terminal that was already open keeps its old
environment values; refresh it after `dwyt .` selects a fallback:

```powershell
. "$env:APPDATA\dwyt\env.ps1"
$env:HEADROOM_PORT
Invoke-RestMethod "http://127.0.0.1:$env:HEADROOM_PORT/health"
```

Open a new Windows Terminal instead if preferred; the DWYT-managed PowerShell
profile sources `env.ps1` automatically. The health request should return HTTP
200 before a compatible client uses the proxy.

## Port 2737 already in use

Another process holds the dashboard port. Find and stop it:

```powershell
Get-NetTCPConnection -LocalPort 2737 | Select-Object OwningProcess
Stop-Process -Id <pid>
```

## Headroom didn't install

Headroom needs Python 3.10–3.12 on PATH:

```powershell
winget install Python.Python.3.12
dwyt install --tools=headroom
```

## RTK

RTK does not ship a Windows binary upstream. Options:

- Use DWYT without RTK — every other feature works.
- Run DWYT inside **WSL** if you want RTK's terminal compression.
- Place a `rtk.exe` at `%APPDATA%\rtk\rtk.exe` (or `%LOCALAPPDATA%\rtk\rtk.exe`)
  and run `dwyt install --tools=rtk`; DWYT will pick it up.

## Obsidian MCP missing in the dashboard

Open `dwyt status` and look for `obsidian` in the tool list. The canonical
Obsidian MCP command is `dwyt.exe obsidian-mcp` — only the main binary
needs to be present in `%APPDATA%\dwyt\bin`. A leftover
`%APPDATA%\dwyt\bin\dwyt-obsidian-mcp.exe` from older versions is removed
automatically the next time you click **Reconfigure MCP** on the dashboard.
If the dashboard still reports the MCP as offline:

```powershell
dwyt stop
Remove-Item "$env:APPDATA\dwyt\bin\dwyt-obsidian-mcp.exe" -ErrorAction SilentlyContinue
dwyt .
```

Then click **Reconfigure MCP** on the Obsidian card. The button now shows
the underlying error if the operation still fails (e.g. no AI client
selected in setup).

## Stop everything

```powershell
dwyt stop
```

This terminates the daemon and managed services by PID
(`%APPDATA%\dwyt\run\*.pid`) using `taskkill /F /T`, including each managed
child process tree.
