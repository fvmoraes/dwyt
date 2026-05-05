# DWYT — Don't Waste Your Tokens

> The invisible orchestrator that reduces token consumption across your AI clients.

DWYT orchestrates four tools that drastically reduce token usage in clients like Claude Code, Codex, Copilot, Kiro, Cursor, and OpenCode — all managed through a single web UI, with no CLI configuration needed.

---

## One-command install

```bash
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
```

The script detects your platform, downloads the binary from GitHub Releases, configures PATH, and guides you through the next steps.

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
| `dwyt reinstall` | Wipe `~/.dwyt` and reinstall |
| `dwyt uninstall` | Remove all tools |

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

### Obsidian — mandatory

The brain of DWYT. Each project gets an **Obsidian vault** at `~/.dwyt/projects/<id>/obsidian/` with structured markdown files:

```
obsidian/
├── index.md         # project index
├── context.md       # auto-rebuilt summary
├── decisions.md     # architecture decisions log
├── tasks.md         # active tasks
├── knowledge/       # knowledge base articles
└── logs/            # sessions, errors, commands
```

**Format**: frontmatter YAML (`tags`, `date`, `type`) in every file. Compatible with Obsidian's native search and Dataview plugin.

**"Open in Obsidian"** button on the card opens the vault directory directly.

**IAs are instructed** to query the vault before any operation — eliminating context rebuilds.

| API | Purpose |
|-----|---------|
| `GET /api/obsidian/search?q=` | Search vault before starting a task |
| `POST /api/obsidian/save` | Save a decision, error, task, or note |

### Headroom — automatic API compression

A proxy that compresses AI API calls in transit (~34% reduction). Uses Headroom's native commands to configure each AI client automatically:

```bash
# DWYT runs these automatically when Headroom starts:
headroom wrap claude      # Claude Code
headroom wrap codex       # Codex
headroom wrap cursor      # Cursor
headroom wrap copilot     # GitHub Copilot CLI
```

When Headroom starts (via the Start button or automatically with `dwyt .`), DWYT runs `headroom wrap` for every enabled AI client. When stopped, `headroom unwrap` cleans up.

Environment variables are also auto-exported by `env.sh`:

```bash
export HEADROOM_PORT=8787
export OPENAI_BASE_URL="http://127.0.0.1:8787/v1"
export ANTHROPIC_BASE_URL="http://127.0.0.1:8787"
```

| Button | Action |
|--------|--------|
| **Open Stats** | Real-time compression statistics |
| **Start/Stop** | Start/stop proxy + auto wrap/unwrap clients |

### RTK — terminal compression

CLI tool that compresses shell command output by 60–98%. Just prefix commands with `rtk`:

```bash
rtk git status
rtk git log --oneline
rtk cargo test
```

Metrics are filtered per project — the card shows commands executed and tokens saved in the current directory.

### Codebase — structural code map (optional)

A code graph that enables structural navigation without file-by-file grep. **On-demand indexing** — click "Index" when you want to analyze the codebase.

Managed by the internal **ProcessManager**:
- Start/Stop with healthcheck (5 retries, exponential backoff)
- Stdout/stderr captured to `~/.dwyt/logs/codebase-*.log`
- Dynamic port (9749, falls back to alternatives if occupied)
- **View Logs** button for real diagnostics on failure

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
│  │  ▶ Start  ■ Stop       │  │  % SAVED          61% │          │
│  │  [/path] [Index]       │  │  ████████████░░░░░░░░░  │          │
│  │  Open Graph →          │  └────────────────────────┘          │
│  └────────────────────────┘                                       │
│  ┌────────────────────────┐  ┌────────────────────────┐          │
│  │  HEADROOM         🟢   │  │  OBSIDIAN          🟢 │          │
│  │  API call compression  │  │  Obsidian vault — …   │          │
│  │  ─────────────────────  │  │  ─────────────────────  │          │
│  │  REQUESTS         234  │  │  FILES             12 │          │
│  │  TOKENS SAVED     8M  │  │  ACTIVE         1h 2m │          │
│  │  COMPRESSION      34%  │  │  ▶ Save  [Search...]  │          │
│  │  PORT             8787  │  │  Rebuild | Forget     │          │
│  │  ▶ Start  ■ Stop       │  │  🧠 Open in Obsidian  │          │
│  │  Open Stats →          │  └────────────────────────┘          │
│  └────────────────────────┘                                       │
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

Click **Install →** and DWYT downloads Configures Codebase, Headroom, and RTK. Generates instruction files for each AI client. Runs `headroom wrap` for supported clients. Starts services. Opens the Dashboard.

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
├── env.sh                       # environment variables
├── dwyt.db                      # SQLite (projects + config)
└── state.json                   # runtime state (PIDs, ports, errors)
```

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

Setup creates these files in the project directory (all added to `.gitignore`):

```
<project>/
├── .mcp.json                      # codebase-memory-mcp MCP config
├── AGENTS.md                      # instructions for Codex, Kiro, Cursor, OpenCode
├── CLAUDE.md                      # instructions for Claude Code
├── opencode.json                  # OpenCode config
├── .github/
│   └── copilot-instructions.md
├── .cursor/
│   └── rules/dwyt.mdc
└── .kiro/
    └── steering/dwyt.md
```

**All instruct IAs** in this priority order:
1. **Obsidian FIRST** — query the vault before any operation
2. **Headroom** — automatic compression via `headroom wrap`
3. **RTK** — prefix shell commands with `rtk`
4. **Codebase MCP** — structural exploration only when needed

---

## Supported clients

| Client | Generated files |
|---|---|
| **Claude Code** | `CLAUDE.md`, `.claude/` |
| **Codex** | `AGENTS.md`, `.codex/`, `.mcp.json` |
| **GitHub Copilot** | `.github/copilot-instructions.md`, `AGENTS.md` |
| **Kiro** | `.kiro/steering/dwyt.md`, `AGENTS.md` |
| **Cursor** | `.cursor/rules/dwyt.mdc`, `AGENTS.md` |
| **OpenCode** | `opencode.json`, `AGENTS.md`, `.mcp.json` |

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

On start, DWYT runs `headroom wrap` for each enabled AI client, configuring their proxy settings natively. On stop, `headroom unwrap` cleans up. **Automatic fallback**: if Headroom goes down, clients fall back to direct API endpoints.

### Headroom wrap mapping

| DWYT client | Headroom command |
|-------------|-----------------|
| Claude Code | `headroom wrap claude` |
| Codex | `headroom wrap codex` |
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
