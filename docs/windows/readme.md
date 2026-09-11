# DWYT on Windows

DWYT runs natively on Windows — native PowerShell installer, `taskkill`-based
process management, `%APPDATA%\dwyt` layout, and native binary installs with
SHA-256 verification. **Git Bash / WSL are not required.**

## Pages

- [Installation](./01-installation.md)
- [Updating](./02-update.md)
- [Troubleshooting](./05-troubleshooting.md)
- [Windows Terminal integration](./04-windows-terminal.md)
- [PowerShell integration](./03-powershell.md)

## Quick start

```powershell
irm https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.ps1 | iex
cd C:\path\to\your\project
dwyt .
```

## Feature parity

| Feature                         | Windows |
|---------------------------------|---------|
| Dashboard + API + SQLite        | ✅ native |
| Process manager (start/stop/health) | ✅ `taskkill` + PID files |
| `dwyt install` (headless)       | ✅ native |
| Codebase Memory MCP             | ✅ native binary + checksum |
| Obsidian MCP + vault            | ✅ native |
| Headroom proxy                  | ✅ native (needs Python) |
| RTK terminal compression        | ⚠️ no upstream Windows binary ([details](./05-troubleshooting.md#rtk)) |
| Obsidian desktop app            | manual download (vault works without it) |
