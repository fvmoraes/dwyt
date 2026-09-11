# DWYT — Don't Waste Your Tokens

DWYT is a Context Optimizer and token usage reduction platform with an integrated AI client. It is built around three MCP servers with strictly separated responsibilities, a persistent knowledge layer, and deterministic rules for context budget, retrieval, memory lifecycle, output control, prompt cache optimization, and cost management.

It works with Claude Code, Codex, Copilot, Kiro, Cursor, and OpenCode, all managed through a single web interface with no CLI configuration required.

| Component | Role |
|---|---|
| **`dwyt_optimizer`** | Efficiency policy: context budget, Token ROI ranking, output contracts, cache guidance, deterministic routing, raw object store |
| **`dwyt_obsidian`** | Brain: durable project memory (canonical knowledge, decisions, tasks, sessions) |
| **`dwyt_codebase`** | Code intelligence: symbols, routes, call paths, dependencies, impact |
| **RTK** | Terminal output compression (CLI, not an MCP) |
| **Headroom** | Optional transport-level API compression — never a source of truth |

## What v5 adds

- **Session intelligence** — the dashboard answers "what did DWYT save *in this sitting*": per-session savings, MCP calls, observed LLM throughput (tokens/s) and models used, on the main screen.
- **Ghost vault cleanup** — legacy hash-only vaults with no content are swept automatically; vaults are only created for registered projects and always follow the `<hash>_<project-name>` layout.
- **Sane defaults** — savings window defaults to **6h** (lifetime totals one click away) and auto-refresh defaults to **10s**.
- **Honest telemetry** — request/task ledgers with NULL-preserving fields, estimated vs observed kept separate, cost per *completed* task, and a deterministic benchmark (`dwyt bench`) that refuses to fake claims.
- **Consolidated configuration** — `dwyt config show|init|validate` over `~/.dwyt/config/dwyt.json`.

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

See the dedicated [Windows documentation](docs/windows/readme.md) for installation, updating, troubleshooting, and Windows Terminal / PowerShell integration.

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
| `dwyt status` | Quick terminal status |
| `dwyt bench` | Deterministic benchmark (five scenarios, four arms, honest `claim_allowed: false`) |
| `dwyt config show\|init\|validate` | Manage `~/.dwyt/config/dwyt.json` (prints the effective config) |
| `dwyt stop` | Stop all services |
| `dwyt version` | Current version |
| `dwyt reinstall` | Clean tool cache and reinstall while preserving project vaults |
| `dwyt uninstall` | Remove DWYT tools/config while preserving project vaults |

---

## Architecture

DWYT is a single self-contained binary (~40MB) with the React UI embedded inside. No runtime dependencies — the UI, API, and services all run from one process. Optimizer session state lives in the **daemon**, not in the MCP processes: an MCP server is spawned per client, so state held there would fragment across clients.

```
AI client (Kiro / Codex / Claude / Cursor / OpenCode / Copilot)
   │  stdio MCP
   ▼
dwyt_optimizer  ──HTTP──▶  daemon :2737  ──▶  session state, budget, telemetry
dwyt_obsidian   ──HTTP──▶  daemon :2737  ──▶  vault (markdown on disk)
dwyt_codebase   ──HTTP──▶  :9749         ──▶  code knowledge graph
```

```
dwyt .
  ├── Resolves the registered project directory
  ├── Attaches the project vault (~/.dwyt/projects/<hash>_<project-name>/)
  ├── Syncs MCP configs for the AI clients selected in setup
  ├── Warms the Codebase service and sweeps ghost vaults
  ├── ProcessManager manages Codebase + Headroom
  ├── RTK active as CLI tool
  └── UI opens at http://localhost:2737
```

---

## The Tools

There is no global priority order. Tools are used in the stage that calls
for them (see the [Optimizer Law](docs/optimizer-law.md)):

1. **PLAN** — `dwyt_optimizer` establishes the context budget, output contract and retrieval envelope before broad retrieval.
2. **RETRIEVE** — `dwyt_codebase` supplies current code structure; `dwyt_obsidian` supplies memory, decisions, tasks and handoff context.
3. **EXECUTE** — RTK compresses shell commands and terminal output.
4. **REDUCE / REUSE** — `dwyt_optimizer` reuses, compacts or evicts context when the Token ROI is positive.
5. **TRANSPORT** — Headroom optimizes compatible API traffic.

A supplier supplies; the Optimizer decides.

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

The Codebase card shows a local `Tokens Saved` estimate when an index exists, and the global dashboard total includes that estimate. See [Codebase Law](docs/codebase-law.md) and [Tokens Saved](docs/tokens-saved.md).

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
├── 90-sessions/             # compact session snapshots (housekeeper keeps 100)
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

The Obsidian card shows a local `Tokens Saved` estimate based on markdown vault size. See [Obsidian Law](docs/obsidian-law.md) and [Tokens Saved](docs/tokens-saved.md).

### Context Optimizer — `dwyt_optimizer`

The efficiency-policy MCP. Every decision is a deterministic function of metadata — no LLM call is spent deciding how to save tokens:

- **Context planning** — budget computed before retrieval, candidates ranked by Token ROI (usefulness per cost-adjusted token), progressive expansion, confidence gate and stop conditions.
- **Delta reuse** — content the session already delivered is referenced by hash, not resent.
- **Output contracts** — per-phase visible-token targets, with the artifact exception: a requested document is never truncated.
- **Tool output compaction** — large tool output is reduced to status, deduplicated diagnostics, counts and a tail; full bytes go to the Raw Object Store behind a `dwyt://objects/<id>` reference.
- **Cache guidance** — prompt assembly ordered for provider prefix caches, with capability states shown honestly (`observed` / `advised` / `unsupported`).
- **Deterministic routing** — complexity and risk scores recommend a model tier; escalation is evidence-based.

Its dashboard card sits next to the **Current Session** card, which reports the sitting's savings, MCP calls, and observed LLM throughput (tokens/s, models) — estimated figures are always labelled `(est.)`, never passed off as observations.

### Headroom — compatible API compression

A proxy/cache optimization for compatible AI clients. DWYT owns the proxy through its ProcessManager and configures supported clients with Headroom's non-interactive, durable `init` command rather than interactive `wrap` commands. Codex authenticated through ChatGPT/OAuth is skipped. Headroom is an optimization only; it is not memory and not a source of code truth. If installed but inactive, DWYT reports it as `installed (launch on demand)` instead of a critical error.

---

## Dashboard

```
┌───────────────────────────────────────────────────────────────────┐
│  🤓 DWYT       [Auto 10s Off 5s] [Period 1h 6h 24h 2d 7d All]     │
│                [↺ Refresh] [Logs] [← Setup]                       │
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
│  └────────────────────────┘  └────────────────────────┘          │
│  ┌────────────────────────┐  ┌────────────────────────┐          │
│  │  HEADROOM         🟢   │  │  OBSIDIAN          🟢 │          │
│  └────────────────────────┘  └────────────────────────┘          │
│  ┌────────────────────────┐  ┌────────────────────────┐          │
│  │  CONTEXT OPTIMIZER 🟢  │  │  CURRENT SESSION   🟢 │          │
│  │  Context reduction 42% │  │  Tokens saved   31.2K │          │
│  │  Avoided tokens  1.2M  │  │  Tokens / s   ~14.2   │  (est.)  │
│  │  Cache hit        38%  │  │  Models: gpt-5 · 78%  │          │
│  │  Cost (obs.) $0.42    │  │  MCP calls          9 │          │
│  │  [Preview] [Run]       │  │  ▾ previous sessions  │          │
│  └────────────────────────┘  └────────────────────────┘          │
└───────────────────────────────────────────────────────────────────┘
```

**Each card** shows the tool name, a one-line description, and its real state — lifecycle states come from the service reconciler, so a warming service shows **🟡 Starting** (never a red offline), two consecutive failed probes show 🟡 Degraded, and a state DWYT cannot observe renders as ⚪ Unknown, not offline. Cards fill their grid cell, so each pair aligns perfectly. The savings window defaults to **6h** and auto-refresh to **10s** — a value the backend could not measure renders as "—", never as a fake zero. See [Startup Lifecycle & Service Status](docs/startup-lifecycle.md).

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
├── config/
│   ├── dwyt.json                # consolidated v5 configuration
│   └── pricing.json             # optional provider pricing catalog
├── codebase/                    # code graph data (CBM_CACHE_DIR)
├── headroom-venv/               # Python virtualenv
├── logs/                        # service stdout/stderr
│   ├── codebase-stdout.log
│   ├── codebase-stderr.log
│   ├── headroom-stdout.log
│   └── headroom-stderr.log
├── objects/                     # raw object store (dwyt://objects/<id>, 0600)
├── projects/                    # per-project vaults
│   └── <sha12>_<project-name>/  # canonical vault layout
│       ├── index.md             # vault root is the Obsidian vault
│       └── ...                  # see vault layout above
├── powers/
│   └── dwyt-power/              # local Kiro Power (regenerable)
├── env.sh                       # environment variables
├── dwyt.db                      # SQLite (projects, config, telemetry ledgers)
└── state.json                   # runtime state (PIDs, ports, errors)
```

`~/.dwyt/projects/` contains persistent project vaults and is protected from automatic cleanup. Vault directories follow the `<hash>_<project-name>` layout; legacy hash-only vaults are renamed when a project name can be recovered and swept when they hold nothing but DWYT scaffolding.

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
├── .mcp.json                      # MCP config (dwyt_optimizer + dwyt_codebase + dwyt_obsidian)
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

**All instruct IAs** with the same stage-based flow:
1. **PLAN** — `dwyt_optimizer` sets the context budget before broad retrieval
2. **RETRIEVE** — `dwyt_codebase` for the code graph, `dwyt_obsidian` for memory and context
3. **EXECUTE** — RTK prefix for shell commands
4. **REDUCE** — tool-output compaction, cache guidance, output contracts

The generated instructions enforce the [Optimizer Law](docs/optimizer-law.md), the [Codebase Law](docs/codebase-law.md) and the [Obsidian Law](docs/obsidian-law.md). DWYT updates only its managed blocks and preserves user content outside those blocks.

For which component owns what — Optimizer, Brain, Code Intelligence, Housekeeper, Memory Compiler — read [Architecture v5](docs/architecture-v5.md).

---

## Why DWYT exists

Every token an agent re-reads is money and latency. In a typical session the same files are re-explored, the same context is rebuilt from scratch, tool output floods the window, and the model narrates what it already knows. DWYT attacks each of those:

- **Deterministic, not magical** — every saving decision (budget, ranking, compaction, routing) is a pure function of metadata. No LLM call is spent deciding how to save tokens, because that would be self-defeating.
- **Honest by construction** — a value that was not measured renders as "—", never as a flattering zero; estimated and observed live in separate columns all the way to the UI, and the benchmark refuses to turn fixtures into product claims.
- **Memory that outlives the session** — the Obsidian brain keeps canonical knowledge, decisions and sessions per project, with a housekeeper that promotes knowledge *before* deleting anything and never touches notes it did not create.

## Technologies

| Layer | Choice |
|---|---|
| Backend | Go 1.25, single static binary (cobra CLI + gin HTTP) |
| Storage | SQLite (`modernc.org/sqlite`, pure Go — no CGO) + markdown vaults |
| Frontend | React + TypeScript + Vite, embedded in the binary at build time |
| MCP | stdio servers (`dwyt_optimizer`, `dwyt_obsidian`, `dwyt_codebase`) + a transparent/opt-in stdio shim (`dwyt mcp-proxy`) |
| Telemetry | Hand-rolled OTLP/HTTP JSON exporter (no OTel SDK dependency tree) |
| Release | GoReleaser for 5 platforms (linux amd64/arm64, macOS amd64/arm64, Windows amd64), automatic on `main` |

## Project structure

```
.
├── core/                        # the DWYT module
│   ├── main.go                  # entry point + MCP subcommand dispatch
│   ├── cmd/dwyt/cli/            # cobra commands (root, config, bench, daemon, mcp-proxy…)
│   ├── cmd/obsidian-mcp/        # standalone Obsidian MCP entry (legacy path)
│   ├── internal/                # all packages below
│   │   ├── optimizer/           # Optimizer runtime (plan, register, usage, route)
│   │   ├── contextopt/          # candidates, budgeter, Token ROI, GC, routing
│   │   ├── toolopt/             # tool output compaction
│   │   ├── outputopt/           # output contracts + structured responses
│   │   ├── cacheintel/          # stable prefix builder + diagnostics
│   │   ├── provider/            # capabilities + pricing catalog
│   │   ├── rawstore/            # content-addressed raw object store
│   │   ├── telemetry/           # request/task ledgers, OTLP export
│   │   ├── housekeeper/         # memory lifecycle + ghost vault sweep hook
│   │   ├── brain/               # vaults, canonical memory, snapshots, migration
│   │   ├── mcp/                 # MCP tool definitions (optimizer, obsidian)
│   │   ├── mcpregistry/         # client config generation per AI tool
│   │   ├── mcpproxy/            # stdio shim (transparent / optimized)
│   │   ├── server/              # daemon: HTTP API + embedded dashboard
│   │   ├── dwytconfig/          # consolidated v5 configuration
│   │   ├── db/                  # SQLite store (projects, metrics, usage)
│   │   ├── benchmark/           # deterministic bench (spec §69)
│   │   ├── procman/             # managed services (codebase, headroom)
│   │   ├── security/            # home guards, protected paths
│   │   └── …                    # detect, install, integrate, kiropow, platform…
│   └── web/                     # React dashboard source (built into server/dist)
├── docs/                        # this documentation set
├── install-lib/                 # shared installer helpers
├── install.sh / install.ps1     # one-command installers (Unix / Windows)
└── .github/workflows/           # CI (test) and automatic releases
```

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

Only real MCPs are placed in `mcp.json`: `dwyt_optimizer`, `dwyt_codebase` and `dwyt_obsidian`. RTK and Headroom are provided as steering instructions because RTK is a CLI tool and Headroom is an API proxy.

DWYT writes Kiro workspace MCP config to `.kiro/settings/mcp.json` and also updates `.kiro/mcp.json` for legacy compatibility. Existing user MCP servers are merged and preserved.

If the symlink cannot be created automatically, the dashboard shows an activation hint with the local path to add through Kiro's "Add power from Local Path" flow.

Status endpoints:

```txt
GET  /api/kiro/power/status
POST /api/kiro/power/refresh
```

See [Kiro Power](docs/kiro-power.md).

---

## UI URLs

| URL | Description |
|---|---|
| `/#/` | Setup Wizard |
| `/#/dashboard` | Dashboard (all repositories) |
| `/#/dashboard?project=/path` | Dashboard with specific project |
| `/#/dashboard?reload=30` | Auto-reload every 30s (default is 10s) |
| `/#/dashboard?window=24h` | Savings window: `1h`, `6h` (default), `24h`, `2d`, `7d`, `all` |
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

`DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS` sets the budget for the CLI's wait on the dashboard itself, and `DWYT_SERVICE_HEALTHCHECK_TIMEOUT_SECONDS` sets the daemon's own budget per managed service (Headroom, Codebase) — two independent knobs so raising one for a slow machine doesn't also widen the other. Both default to 60 seconds on Linux/macOS and 120 seconds on Windows. Set a positive integer in seconds to override either for the process, for example:

```bash
DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS=180 DWYT_SERVICE_HEALTHCHECK_TIMEOUT_SECONDS=180 dwyt .
```

The Codebase service is warmed in the background and never blocks the dashboard from opening, even if it never becomes healthy.

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

## Documentation

| Document | Contents |
|---|---|
| [How It Works](docs/how-it-works.md) | Architecture & internals: packages, startup flow, APIs, data layout, build/release |
| [Startup Lifecycle & Service Status](docs/startup-lifecycle.md) | Dashboard-first boot, service reconciler, lifecycle states, honest status rules |
| [Architecture v5](docs/architecture-v5.md) | Component roles and ownership (Optimizer, Brain, Code Intelligence), v5 rules |
| [Optimizer Law](docs/optimizer-law.md) | Context budget, Token ROI, reuse, compression and output invariants |
| [Codebase Law](docs/codebase-law.md) | Mandatory code-graph workflow for agents |
| [Obsidian Law](docs/obsidian-law.md) | Mandatory memory workflow for agents |
| [Tokens Saved](docs/tokens-saved.md) | Where the savings numbers come from; sessions and windows |
| [Kiro Power](docs/kiro-power.md) | Kiro Power paths, frontmatter, MCP behavior |
| [Release Process](docs/release-process.md) | Automatic releases, semver conventions (scopes, `!`, BREAKING CHANGE) |
| [Changelog](docs/CHANGELOG.md) | Notable changes per release |
| [Windows docs](docs/windows/readme.md) | Installation, update, troubleshooting, PowerShell/Terminal notes |
| [Agent rules](docs/rules/rules.md) | Repo conventions for agents working on DWYT itself |

---

## Repositories

- [DWYT](https://github.com/fvmoraes/dwyt)
- [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)
- [RTK](https://github.com/rtk-ai/rtk)
- [Headroom](https://github.com/chopratejas/headroom)
- [Obsidian](https://obsidian.md) — Project vault
