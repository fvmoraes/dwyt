# DWYT — Don't Waste Your Tokens

> The invisible orchestrator that reduces token consumption across your AI clients.

DWYT orchestrates four tools that drastically reduce token usage in clients like Claude Code, Codex, Copilot, Kiro, Cursor, and OpenCode — all managed through a single web UI, with no CLI configuration needed.

---

## One-command install

```bash
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
```

The script detects your platform, downloads the latest binary from GitHub Releases, overwrites any previous `dwyt` binary in `~/.local/bin`, configures PATH, and guides you through the next steps.

---

## Usage

```bash
cd ~/my-project
dwyt .
```

The UI opens at `http://localhost:2737` with your project pre-loaded. **Everything is configured through the UI — no CLI commands needed.**

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
- Start/Stop with healthcheck (5 retries, exponential backoff)
- Stdout/stderr captured to `~/.dwyt/logs/codebase-*.log`
- Dynamic port (9749, falls back to alternatives if occupied)
- **View Logs** button for real diagnostics on failure

The Codebase card shows a local `Tokens Saved` estimate when an index exists, and the global dashboard total includes that estimate. See [Codebase Law](docs/CODEBASE-LAW.md) and [Tokens Saved](docs/TOKENS-SAVED.md).

### Obsidian — mandatory memory

Each project gets an **Obsidian vault** at `~/.dwyt/projects/<id>/obsidian/` with structured markdown files:

```txt
obsidian/
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

A proxy/cache optimization for compatible AI clients. DWYT can configure eligible clients with Headroom's native wrapping:

```bash
headroom wrap claude      # Claude Code
headroom wrap codex       # Codex API-key login only
headroom wrap cursor      # Cursor
headroom wrap copilot     # GitHub Copilot CLI
```

Codex authenticated through ChatGPT/OAuth is skipped. Headroom is an optimization only; it is not memory and not a source of code truth. If installed but inactive, DWYT reports it as `installed (launch on demand)` instead of a critical error.

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
│  ▾ AI Clients                6 of 6 selected            │
│  ┌─────────────────────────────────────────────────┐    │
│  │ ● Claude Code   ● Codex   ● GitHub Copilot      │    │
│  │ ● Kiro          ● Cursor  ● OpenCode            │    │
│  └─────────────────────────────────────────────────┘    │
│                                                         │
│  ▾ Project                   /home/user/my-project      │
│  ┌─────────────────────────────────────────────────┐    │
│  │ /home/user/my-project           [Select]        │    │
│  └─────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

Click **Install →** and DWYT downloads Configures Codebase, Headroom, and RTK. Generates instruction files for each AI client. Runs `headroom wrap` for supported clients; Codex Headroom setup only runs for API-key login. Starts services. Opens the Dashboard.

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

Headroom starts automatically in background on port 8787 with `dwyt .`. The `env.sh` injected into your shell RC exports:

```bash
export HEADROOM_PORT=8787
export OPENAI_BASE_URL="http://127.0.0.1:8787/v1"
export ANTHROPIC_BASE_URL="http://127.0.0.1:8787"
```

On start, DWYT runs `headroom wrap` for each eligible enabled AI client, configuring their proxy settings natively. Codex with ChatGPT/OAuth login is skipped; Codex Headroom setup only runs for API-key login. On stop, `headroom unwrap` cleans up. **Automatic fallback**: if Headroom goes down, clients fall back to direct API endpoints.

### Headroom wrap mapping

| DWYT client | Headroom command |
|-------------|-----------------|
| Claude Code | `headroom wrap claude` |
| Codex (API-key login) | `headroom wrap codex` |
| Cursor | `headroom wrap cursor` |
| GitHub Copilot | `headroom wrap copilot` |
| Kiro / OpenCode | env vars only (no native wrap) |

---

## Codebase — technical details

Managed by the internal **ProcessManager**:
- **Start**: healthcheck with retry (5 attempts, exponential backoff, 30s timeout for Codebase)
- **Stop**: `SIGTERM` → wait 5s → `SIGKILL`
- **Logs**: `~/.dwyt/logs/codebase-stdout.log` + `codebase-stderr.log`
- **Dynamic port**: if 9749 is occupied, tries 9750, 9751, 9752
- **stdin**: kept open via pipe (Codebase is an MCP server, exits on EOF)

**Indexing**: on-demand only. Click "Index" in the UI. Progress is polled every 2 seconds.

---

## Requirements

| Tool | Required for |
|------|-------------|
| Obsidian | **Mandatory** — primary knowledge engine (app optional, vault always works) |
| Python 3 | Headroom installation |
| curl or wget | Installer download |
| Git | Dependency installation |

The `dwyt` binary itself has no dependencies — it's a static Go executable with the React UI embedded.

---

## Repositories

- [DWYT](https://github.com/fvmoraes/dwyt)
- [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)
- [RTK](https://github.com/rtk-ai/rtk)
- [Headroom](https://github.com/chopratejas/headroom)
- [Obsidian](https://obsidian.md) — Project vault
