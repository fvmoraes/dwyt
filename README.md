# DWYT — Don't Waste Your Tokens

> The invisible orchestrator that reduces token consumption across your AI clients.

DWYT orchestrates four tools that drastically reduce token usage in clients like Claude Code, Codex, Copilot, Kiro, Cursor, and OpenCode — all managed through a single web UI, with no CLI configuration needed.

---

## One-command install

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
```

The script detects your platform, downloads the latest binary from GitHub Releases, overwrites any previous `dwyt` binary in `~/.local/bin`, configures PATH, and guides you through the next steps.

### Windows (PowerShell)

Windows has **full native support** — the installer is **native PowerShell, with no Git Bash or WSL required**. Open **Windows Terminal** (PowerShell) and run:

```powershell
irm https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.ps1 | iex
```

The installer downloads `dwyt_windows_<arch>.zip` from the latest release, verifies its **SHA-256** checksum, installs `dwyt.exe` under `%APPDATA%\dwyt\bin`, adds that folder to your user PATH, and runs `dwyt install` to set up the tools. From a local clone you can run `.\install.ps1` (add `-SkipDeps` to install only the binary).

See the dedicated [Windows documentation](docs/windows/README.md) for installation, updating, troubleshooting, and Windows Terminal / PowerShell integration.

---

## Usage

Linux / macOS:

```bash
cd ~/my-project
dwyt .
```

Windows (PowerShell):

```powershell
cd C:\path\to\your\project
dwyt .
```

The UI opens at `http://localhost:2737` with your project pre-loaded. **Everything is configured through the UI — no CLI commands needed.** The commands below are identical across platforms.

### Commands

| Command | Description |
|---------|-------------|
| `dwyt .` | Open in current directory |
| `dwyt /path` | Open in a specific directory |
| `dwyt` | Open in CWD |
| `dwyt stop` | Stop all services |
| `dwyt status` | Quick terminal status |
| `dwyt version` | Current version |
| `dwyt reinstall` | Clean tool cache and reinstall while preserving project vaults |
| `dwyt uninstall` | Remove DWYT tools/config while preserving project vaults |

---

## Architecture

DWYT is a single self-contained binary (~37MB) with the React UI embedded inside. No runtime dependencies — the UI, API, and services all run from one process.

```
dwyt .
  ├── Detects project directory
  ├── Loads Obsidian vault (~/.dwyt/projects/<id>/obsidian/)
  ├── ProcessManager starts Codebase + Headroom in background
  ├── RTK active as CLI tool
  └── UI opens at http://localhost:2737
```

---

## The Tools

DWYT coordinates tools in this order when the task calls for them:

1. **RTK** for shell commands and terminal output.
2. **Codebase MCP** for current code structure.
3. **Obsidian MCP** for memory, decisions, tasks, and handoff context.
4. **Headroom** for compatible API proxy/cache optimization.

### RTK — terminal compression

CLI tool that compresses shell command output by 60–98%. Just prefix commands with `rtk`:

```bash
rtk git status
rtk git log --oneline
rtk cargo test
```

Metrics are filtered per project — the card shows commands executed and tokens saved in the current directory.

### Codebase — structural code map

A code graph that enables structural navigation without file-by-file grep. It is the source of truth for symbols, dependencies, calls, routes, and impact analysis. Indexing is on-demand: click "Index" when you want to analyze the project.

Managed by the internal **ProcessManager**:
- Start/Stop with an immediate health probe, then 500 ms polling within a total startup budget
- Stdout/stderr captured to `~/.dwyt/logs/codebase-*.log`
- Dynamic port (9749, falls back to alternatives if occupied)
- **View Logs** button for real diagnostics on failure

The Codebase card shows a local `Tokens Saved` estimate when an index exists, and the global dashboard total includes that estimate. See [Codebase Law](docs/CODEBASE-LAW.md) and [Tokens Saved](docs/TOKENS-SAVED.md).

### Obsidian — mandatory memory

Each project gets an **Obsidian vault** at `~/.dwyt/projects/<id>_<project-name>/` (e.g. `1597b5fc9bfb_dwyt`) with structured markdown files:

```txt
<id>_<project-name>/
├── .dwyt/vault.json      # DWYT vault metadata (hash, name)
├── index.md
├── context.md
├── instructions/
│   ├── obsidian-law.md
│   └── codebase-law.md
├── maps/
│   └── project-map.md
├── templates/
│   ├── decision-template.md
│   ├── task-template.md
│   └── session-context-template.md
├── decisions/
│   └── index.md
├── tasks/
│   └── index.md
├── debug/
│   └── index.md
├── context/
├── knowledge/
└── logs/
    ├── sessions/
    ├── errors/
    └── commands/
```

**Obsidian Law**: agents must query and summarize the vault before relevant work, save decisions/task/debug state during work, and save complete context at the end. Vaults are persistent project memory and must not be deleted by install, repair, reinstall, clean, reset, or uninstall flows.

| API | Purpose |
|-----|---------|
| `GET /api/obsidian/search?q=` | Search vault before starting a task |
| `POST /api/obsidian/save` | Save a decision, debug note, task, or note |
| `POST /api/obsidian/summarize` | Rebuild the vault summary |
| `POST /api/obsidian/context` | Save complete task/session context |

The Obsidian card shows a local `Tokens Saved` estimate based on markdown vault size. See [Obsidian Law](docs/OBSIDIAN-LAW.md) and [Tokens Saved](docs/TOKENS-SAVED.md).

### Headroom — compatible API compression

A proxy/cache optimization for compatible AI clients. DWYT owns the proxy through its ProcessManager and configures supported clients with Headroom's non-interactive, durable `init` command rather than interactive `wrap` commands. Codex authenticated through ChatGPT/OAuth is skipped. Headroom is an optimization only; it is not memory and not a source of code truth. If installed but inactive, DWYT reports it as `installed (launch on demand)` instead of a critical error.

---

## Dashboard

```
┌───────────────────────────────────────────────────────────────────┐
│  🤓 DWYT          [Auto Off 5s 10s] [↺ Refresh] [Logs] [← Setup] │
├───────────────────────────────────────────────────────────────────┤
│  🛡️ my-project  DWYT is protecting this project  🧠 12 obsidian files │
│                                                                   │
│  ┌───────────────────────────────────────────────────────────┐    │
│  │  Without DWYT     │  With DWYT        │  Total Savings    │    │
│  │  2.4M tokens      │  480K tokens      │  1.9M  ↓ 80%     │    │
│  │  would be spent   │  spent            │                   │    │
│  │                   │                   │  Obsidian | RTK   │    │
│  │                   │                   │  Headroom|Codebase│    │
│  └───────────────────────────────────────────────────────────┘    │
│                                                                   │
│  ┌────────────────────────┐  ┌────────────────────────┐          │
│  │  CODEBASE         🟢   │  │  RTK               🟢 │          │
│  │  Code graph — …        │  │  Terminal output —  … │          │
│  │  ─────────────────────  │  │  ─────────────────────  │          │
│  │  UPTIME       2m 3s    │  │  COMMANDS         847 │          │
│  │  STATUS     Indexed    │  │  TOKENS SAVED     31M │          │
│  │  MCP            🟢 Online│  │  % SAVED          61% │          │
│  │  [/path] [Index]       │  │  🏷 CLI: prefix with rtk │          │
│  │  Open Graph →          │  │  ████████████░░░░░░░░░  │          │
│  │  Configure MCP         │  └────────────────────────┘          │
│  └────────────────────────┘                                       │
│  ┌────────────────────────┐  ┌────────────────────────┐          │
│  │  HEADROOM         🟢   │  │  OBSIDIAN          🟢 │          │
│  │  API call compression  │  │  Obsidian vault — …   │          │
│  │  ─────────────────────  │  │  ─────────────────────  │          │
│  │  REQUESTS         234  │  │  FILES             12 │          │
│  │  TOKENS SAVED     8M  │  │  ACTIVE         1h 2m │          │
│  │  COMPRESSION      34%  │  │  MCP            🟢 Online│          │
│  │  ▶ Start  ■ Stop       │  │  [type ▼] [note...] [Save]│          │
│  │  Open Stats →          │  │  [Search obsidian...] 🔍 │          │
│  └────────────────────────┘  │  Configure MCP          │          │
│                               │  Rebuild | Open Vault  │          │
│                               └────────────────────────┘          │
└───────────────────────────────────────────────────────────────────┘
```

**Each card** shows the tool name, a one-line description, and real status (🟢 online / 🟡 stopped / 🔴 not installed).

---

## Setup

On first run, the UI opens the Setup Wizard. **Obsidian is mandatory** and pre-selected. Other tools are optional.

```
┌─────────────────────────────────────────────────────────┐
│  🤓 DWYT                    [Install →] [Dashboard →]   │
├─────────────────────────────────────────────────────────┤
│  ▾ Tools                     4 of 4 selected            │
│  ┌─────────────────────────────────────────────────┐    │
│  │ ● Obsidian (ON)  Obsidian vault — project       │    │
│  │ ● Codebase       Code graph — structural        │    │
│  │ ● Headroom       API call compression           │    │
│  │ ● RTK            Terminal output compression    │    │
│  └─────────────────────────────────────────────────┘    │
│                                                         │
│  ▾ AI Clients                8 of 8 selected            │
│  ┌─────────────────────────────────────────────────┐    │
│  │ ● Claude Code   ● Codex   ● GitHub Copilot      │    │
│  │ ● Kiro          ● Cursor  ● OpenCode            │    │
│  │ ● Windsurf      ● Continue                       │    │
│  └─────────────────────────────────────────────────┘    │
│                                                         │
│  ▾ Project                   /home/user/my-project      │
│  ┌─────────────────────────────────────────────────┐    │
│  │ /home/user/my-project           [Select]        │    │
│  └─────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

Click **Install →** and DWYT downloads and configures Codebase, Headroom, and RTK. It generates instruction files for each selected AI client. Once the proxy is healthy, DWYT runs the supported non-interactive `headroom init` setup; Codex Headroom setup only runs for API-key login. It then starts services and opens the Dashboard.

---

## Where data lives

### Linux / macOS

```
~/.dwyt/
├── bin/                         # tool binaries
├── codebase/                    # code graph data (CBM_CACHE_DIR)
├── headroom-venv/               # Python virtualenv
├── logs/                        # service stdout/stderr
│   ├── codebase-stdout.log
│   ├── codebase-stderr.log
│   ├── headroom-stdout.log
│   └── headroom-stderr.log
├── projects/                    # per-project vaults
│   └── <sha12>/
│       ├── obsidian/            # Obsidian vault (markdowns)
│       └── project.json         # project metadata
├── powers/
│   └── dwyt-power/              # local Kiro Power (regenerable)
├── env.sh                       # environment variables
├── dwyt.db                      # SQLite (projects + config)
└── state.json                   # runtime state (PIDs, ports, errors)
```

`~/.dwyt/projects/` contains persistent project vaults and is protected from automatic cleanup.

### Windows

```
%APPDATA%\dwyt\
├── bin\
├── codebase\
├── headroom-venv\
├── logs\
├── projects\
├── env.ps1
├── dwyt.db
└── state.json
```

---

## Generated project files

Setup creates or updates these files in the project directory. Local configs with absolute paths are ignored; shared instruction files stay versionable by default.

```
<project>/
├── .mcp.json                      # MCP config (codebase + obsidian servers)
├── AGENTS.md                      # instructions for Codex, Kiro, Cursor, OpenCode
├── CLAUDE.md                      # instructions for Claude Code
├── opencode.json                  # OpenCode config
├── .github/
│   └── copilot-instructions.md
├── .cursor/
│   └── rules/dwyt.mdc
├── .claude/
│   └── mcp.json                   # Claude MCP config
├── .vscode/
│   └── mcp.json                   # VSCode MCP config
└── .kiro/
    ├── settings/mcp.json          # Kiro MCP config (primary)
    ├── mcp.json                   # Kiro MCP config (legacy compatibility)
    └── steering/dwyt.md
```

**All instruct IAs** in this priority order:
1. **RTK** — prefix shell commands with `rtk`
2. **Codebase MCP** — use the graph before structural code work
3. **Obsidian MCP** — search/summarize memory and save context
4. **Headroom** — use only as compatible proxy/cache optimization

The generated instructions enforce the [Codebase Law](docs/CODEBASE-LAW.md) and [Obsidian Law](docs/OBSIDIAN-LAW.md). DWYT updates only its managed blocks and preserves user content outside those blocks.

---

## Supported clients

| Client | Generated files |
|---|---|
| **Claude Code** | `CLAUDE.md`, `.claude/` |
| **Codex** | `AGENTS.md`, `.codex/`, `.mcp.json` |
| **GitHub Copilot** | `.github/copilot-instructions.md`, `AGENTS.md` |
| **Kiro** | `.kiro/steering/dwyt.md`, `.kiro/settings/mcp.json`, `.kiro/mcp.json`, `AGENTS.md` |
| **Cursor** | `.cursor/rules/dwyt.mdc`, `AGENTS.md` |
| **OpenCode** | `opencode.json`, `AGENTS.md`, `.mcp.json` |
| **Windsurf** | `.windsurf/rules/dwyt.md`, `.windsurf/mcp.json` |
| **Continue** | `.continue/mcp.json` |

---

## Kiro Power

When Kiro is enabled, DWYT creates a local Power at:

```txt
~/.dwyt/powers/dwyt-power
```

It is linked into:

```txt
~/.kiro/powers/dwyt-power
```

Only real MCPs are placed in `mcp.json`: `codebase` and `obsidian`. RTK and Headroom are provided as steering instructions because RTK is a CLI tool and Headroom is an API proxy.

DWYT writes Kiro workspace MCP config to `.kiro/settings/mcp.json` and also updates `.kiro/mcp.json` for legacy compatibility. Existing user MCP servers are merged and preserved.

If the symlink cannot be created automatically, the dashboard shows an activation hint with the local path to add through Kiro's "Add power from Local Path" flow.

Status endpoints:

```txt
GET  /api/kiro/power/status
POST /api/kiro/power/refresh
```

See [Kiro Power](docs/KIRO-POWER.md).

---

## UI URLs

| URL | Description |
|---|---|
| `/#/` | Setup Wizard |
| `/#/dashboard` | Dashboard (all repositories) |
| `/#/dashboard?project=/path` | Dashboard with specific project |
| `/#/dashboard?reload=5` | Auto-reload every 5s |
| `/#/dashboard?logs=1` | Logs panel open |

---

## Headroom — technical details

Headroom requests port 8787 by default when `dwyt .` starts it. If that port is occupied, DWYT selects the first free port from 8788 through 8791 and publishes the effective port to the dashboard, runtime state, and managed environment file. New terminals should source the updated `env.sh` (Linux/macOS) or `env.ps1` (Windows) before launching a client.

The `env.sh` injected into your shell RC (Linux / macOS) exports the effective port, for example:

```bash
export HEADROOM_PORT=8787
export OPENAI_BASE_URL="http://127.0.0.1:8787/v1"
export ANTHROPIC_BASE_URL="http://127.0.0.1:8787"
```

On Windows, DWYT writes the equivalent `env.ps1` under `%APPDATA%\dwyt`:

```powershell
$env:HEADROOM_PORT = "8787"
$env:OPENAI_BASE_URL = "http://127.0.0.1:8787/v1"
$env:ANTHROPIC_BASE_URL = "http://127.0.0.1:8787"
```

After the proxy is healthy, DWYT runs Headroom's non-interactive durable setup for the selected eligible clients. It does **not** use `headroom wrap` or `headroom unwrap`, because `wrap` starts its own interactive proxy and CLI process. Codex with ChatGPT/OAuth login is skipped; Codex setup only runs for API-key login.

### Headroom client setup

| DWYT client | Headroom setup |
|-------------|----------------|
| Claude Code | `headroom init --port <effective-port> claude` |
| Codex (API-key login) | `headroom init --port <effective-port> codex` |
| GitHub Copilot | `headroom init --global --port <effective-port> copilot` |
| Cursor | Set its base URL manually to `http://127.0.0.1:<effective-port>/v1`; Headroom has no durable Cursor `init` command. |
| Kiro / OpenCode / Windsurf / Continue | Managed environment variables only; no native Headroom `init` command. |

### Startup healthcheck and timeout

The dashboard daemon and managed HTTP services make a probe immediately, then retry every 500 ms until their total startup budget expires. Each HTTP attempt is limited to 2 seconds and cannot extend that total budget. An HTTP 200 is enough to mark the configured endpoint ready; for Headroom, an optional degraded component such as `kompress` does not block readiness.

`DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS` sets the total budget for both daemon and managed-service startup. The default is 60 seconds on Linux/macOS and 120 seconds on Windows. Set a positive integer in seconds to override it for the process, for example:

```bash
DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS=180 dwyt .
```

On a daemon startup timeout, DWYT logs the tested URL, child PID, last HTTP/connection error, and elapsed wait. It then terminates the daemon tree: a dedicated process group on Linux/macOS and `taskkill /F /T` on Windows. This prevents Headroom launcher and Python descendants from being orphaned.

---

## Codebase — technical details

Managed by the internal **ProcessManager**:
- **Start**: immediate healthcheck followed by 500 ms polling, using the shared 60 s (Linux/macOS) or 120 s (Windows) startup budget
- **Stop**: terminates the managed process tree; Windows uses `taskkill /F /T`
- **Logs**: `~/.dwyt/logs/codebase-stdout.log` + `codebase-stderr.log`
- **Dynamic port**: if 9749 is occupied, tries 9750 through 9753
- **stdin**: kept open via pipe (Codebase is an MCP server, exits on EOF)

**Indexing**: on-demand only. Click "Index" in the UI. Progress is polled every 2 seconds.

---

## Requirements

| Tool | Required for |
|------|-------------|
| Obsidian | **Mandatory** — primary knowledge engine (app optional, vault always works) |
| Python 3 | Headroom installation |
| curl or wget | Installer download (Linux / macOS) |
| PowerShell 5.1+ | Installer download (Windows — built in; no Git Bash / WSL needed) |
| Git | Dependency installation |

The `dwyt` binary itself has no dependencies — it's a static Go executable with the React UI embedded.

### Platform notes

- **Linux / macOS / Windows** all run the dashboard, API, SQLite, MCP servers, Headroom proxy, and the cross-platform process manager natively.
- **RTK** terminal compression has **no upstream Windows binary**. On Windows, DWYT uses a pre-installed `rtk.exe` if found and otherwise skips it with a clear message — every other feature works normally. See the [Windows troubleshooting guide](docs/windows/troubleshooting.md#rtk).

---

## Repositories

- [DWYT](https://github.com/fvmoraes/dwyt)
- [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)
- [RTK](https://github.com/rtk-ai/rtk)
- [Headroom](https://github.com/chopratejas/headroom)
- [Obsidian](https://obsidian.md) — Project vault
