# DWYT on Windows

DWYT runs natively on Windows — native PowerShell installer, `taskkill`-based
process management, `%APPDATA%\dwyt` layout, and native binary installs with
SHA-256 verification. **Git Bash / WSL are not required.**

## Pages

- [Installation](./installation.md)
- [Updating](./update.md)
- [Troubleshooting](./troubleshooting.md)
- [Windows Terminal integration](./windows-terminal.md)
- [PowerShell integration](./powershell.md)

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
| RTK terminal compression        | ⚠️ no upstream Windows binary ([details](./troubleshooting.md#rtk)) |
| Obsidian desktop app            | manual download (vault works without it) |
