# DWYT on Windows — Installation

DWYT has full native support on Windows. The installer is native PowerShell —
**no Git Bash or WSL required**.

## Requirements

- Windows 10/11 (x64 or ARM64)
- PowerShell 5.1+ (built in) or PowerShell 7+
- Optional: [Python 3.10–3.12](https://python.org) for the Headroom proxy
  (`winget install Python.Python.3.12`)

## One-line install (recommended)

Open **Windows Terminal** (PowerShell) and run:

```powershell
irm https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.ps1 | iex
```

## From a local clone

```powershell
git clone https://github.com/fvmoraes/dwyt
cd dwyt
.\install.ps1
```

Install only the binary (skip the tools):

```powershell
.\install.ps1 -SkipDeps
```

## What the installer does

1. Downloads `dwyt_windows_<arch>.zip` from the latest GitHub release.
2. Verifies the **SHA-256** checksum against `checksums.txt`.
3. Installs `dwyt.exe` to `C:\Users\<user>\AppData\Roaming\dwyt\bin`.
4. Adds that folder to your **user PATH**.
5. Runs `dwyt install`, which installs the tools natively:
   - **Codebase Memory MCP** — prebuilt binary (zip), checksum-verified.
   - **Obsidian MCP** — bundled with DWYT.
   - **Headroom** — Python venv (requires Python on PATH).
   - **RTK** — see the note below.
6. Leaves you ready to run `dwyt .` in any project.

## Start DWYT

Open a project folder in your terminal and run:

```powershell
cd C:\path\to\your\project
dwyt .
```

The dashboard opens at <http://localhost:2737> and DWYT configures the MCP
servers and instruction files for the AI clients you enable.

## File locations on Windows

| Purpose            | Path                                                       |
|--------------------|------------------------------------------------------------|
| DWYT home          | `C:\Users\<user>\AppData\Roaming\dwyt`                     |
| Binaries           | `C:\Users\<user>\AppData\Roaming\dwyt\bin`                 |
| Project memory     | `C:\Users\<user>\AppData\Roaming\dwyt\projects\<id>`       |
| Logs               | `C:\Users\<user>\AppData\Roaming\dwyt\logs`                |
| Running PIDs       | `C:\Users\<user>\AppData\Roaming\dwyt\run`                 |

## Note on RTK

The RTK terminal-compression tool does not publish a Windows binary upstream.
DWYT will use a pre-installed `rtk.exe` if present (e.g. via WSL or a manual
build) and otherwise skips it with a clear message — **all other DWYT features
work normally**. See [troubleshooting](./troubleshooting.md#rtk).
