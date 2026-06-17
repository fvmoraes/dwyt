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

## Stop everything

```powershell
dwyt stop
```

This terminates the daemon and managed services by PID
(`%APPDATA%\dwyt\run\*.pid`) using `taskkill`.
