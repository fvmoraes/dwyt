# DWYT on Windows — Updating

## Update the DWYT binary

Re-run the installer; it always fetches the latest release and overwrites the
old binary (your projects and Obsidian vaults are preserved):

```powershell
irm https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.ps1 | iex
```

When a newer version is available, the dashboard also shows an **update banner**
with the exact command.

## Stop a running daemon first (optional)

The installer overwrites `dwyt.exe` in place. If the daemon is running and the
file is locked, stop it first:

```powershell
dwyt stop
```

DWYT auto-detects a version mismatch on the next `dwyt .` and restarts the
daemon with the new binary, so a manual stop is usually unnecessary.

## Repair / reinstall the tools

```powershell
dwyt install                 # install or repair all tools
dwyt install --tools=cbmcp   # just one
```

## Clean reinstall (preserves Obsidian vaults)

```powershell
dwyt reinstall
```

This clears the tool cache under `%APPDATA%\dwyt` **except** the
`projects\` folder (your vaults and history), then you re-run `dwyt`.
