# HOW THIS WORKS — DWYT Architecture & Internals

## Overview

DWYT (Don't Waste Your Tokens) is a self-contained, single-binary orchestrator that reduces AI token consumption by managing four tools behind a unified web UI.

```
User runs: dwyt .
  → Detects project directory
  → Creates/loads Obsidian vault (~/.dwyt/projects/<id>/)
  → Starts Headroom proxy in background (port 8787)
  → Codebase sits idle (on-demand indexing)
  → RTK is active as CLI tool
  → Serves React UI at http://localhost:2737
```

---

## Technology Stack

| Layer | Technology | Purpose |
|-------|-----------|---------|
| **Binary** | Go 1.25 | Single static binary, embeds frontend via `//go:embed` |
| **Frontend** | React 19 + TypeScript + Vite 8 + Tailwind | Dashboard & Setup Wizard |
| **Routing** | React Router v7 (HashRouter) | SPA navigation |
| **HTTP Server** | Gin (Go) | 35+ API routes, SSE broadcasting, SPA fallback |
| **Database** | SQLite (`modernc.org/sqlite`) | Project catalog, config storage |
| **CLI** | Cobra (Go) | `dwyt .`, `dwyt stop`, `dwyt status`, subcommands |
| **Releases** | GoReleaser + GitHub Actions | Multi-platform builds, changelog |

### Embedded Frontend

The React app is compiled to static files and embedded into the Go binary:

```go
//go:embed dashboard/dist
var reactFS embed.FS
```

Vite outputs to `core/internal/server/dashboard/dist/`. At build time, GoReleaser compiles `core/` which picks up the embedded files. The binary is fully self-contained — no external files needed to serve the UI.

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│ dwyt binary (single static executable, ~37MB)           │
│                                                         │
│  ┌──────────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │ CLI (Cobra)   │  │ Gin HTTP │  │ React SPA        │  │
│  │ root.go       │  │ server   │  │ (embedded via    │  │
│  │ daemon/stop/  │  │ :2737    │  │ //go:embed)      │  │
│  │ status        │  │          │  │                  │  │
│  └──────┬───────┘  └────┬─────┘  └──────────────────┘  │
│         │               │                               │
│  ┌──────┴───────────────┴──────────────────────────┐   │
│  │ Internal Packages                                │   │
│  │ ┌────────┐ ┌────────┐ ┌────────┐ ┌───────────┐  │   │
│  │ │ brain  │ │procman │ │ state  │ │ integrate  │  │   │
│  │ │Obsidian│ │Start/  │ │PIDs/   │ │AGENTS.md  │  │   │
│  │ │ vault  │ │Stop/   │ │ports/  │ │CLAUDE.md  │  │   │
│  │ │ markdn │ │Logs    │ │errors  │ │generator  │  │   │
│  │ └────────┘ └────────┘ └────────┘ └───────────┘  │   │
│  │ ┌────────┐ ┌────────┐ ┌────────┐ ┌───────────┐  │   │
│  │ │ status │ │ health │ │  env   │ │  install   │  │   │
│  │ │polling │ │probes  │ │env.sh  │ │Codebase/rtk/  │  │   │
│  │ │        │ │        │ │PATH    │ │headroom    │  │   │
│  │ └────────┘ └────────┘ └────────┘ └───────────┘  │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

### Package responsibilities

| Package | File | Purpose |
|---------|------|---------|
| `cmd/dwyt/cli/root` | `root.go` | CLI entry point, daemon spawn, service orchestration |
| `internal/server` | `server.go` | Gin HTTP server, 35+ API routes, SSE, SPA embedding |
| `internal/brain` | `brain.go` | Obsidian vault: markdown files, frontmatter YAML, search |
| `internal/procman` | `procman.go` | ProcessManager: Start/Stop/Status/Logs with healthcheck |
| `internal/integrate` | `integrate.go` | AI client file generator (AGENTS.md, CLAUDE.md, etc.) |
| `internal/state` | `state.go` | RuntimeState: PID tracking, errors, current project |
| `internal/status` | `status.go` | Tool health polling, RTK/Headroom metrics parsing |
| `internal/health` | `health.go` | HTTP health probes, service start/stop helpers |
| `internal/install` | `install.go` | Tool installers: Codebase, RTK, Headroom |
| `internal/env` | `env.go` | Shell RC injection, env.sh, PATH symlinks |
| `internal/db` | `db.go` | SQLite store: projects table, config key-value |
| `internal/mcpregistry` | `registry.go` | MCP server registry, Claude/VSCode/Kiro config sync |
| `internal/detect` | `detect.go` | OS/Shell/Home detection |
| `internal/workspace` | `workspace.go` | Per-project `.dwyt/` state |
| `internal/log` | `log.go` | File-based logger (DEBUG/INFO/WARN/ERROR) |

---

## Startup Flow

```
dwyt .
  │
  ├─ 1. detect.Detect()           → OS, shell, home dir, DWYT paths
  ├─ 2. env.Init()                → creates ~/.dwyt/env.sh, injects into .zshrc/.bashrc
  ├─ 3. obsidian check            → prints status (warning if not installed)
  ├─ 4. probeDaemon()             → checks :2737 and reads daemon version
  │    └─ YES, same version → switchProject() → POST /api/project/switch, open browser, exit
  │    └─ YES, stale/unknown version → stop old daemon and continue
  │    └─ NO → continue
  ├─ 5. startServicesAsync()      → prints tool availability (no blocking)
  ├─ 6. spawn daemon process      → exec.Command(exe, "daemon")
  │    └─ detached with Setsid    → DWYT_PROJECT, DWYT_HEADROOM_PORT env vars
  ├─ 7. waitForDaemon()           → health probe loop (3s timeout, 300ms interval)
  └─ 8. openBrowserURL()          → xdg-open http://localhost:2737
```

### Daemon Process

```
dwyt daemon
  │
  ├─ server.New(2737, dwytBin, dwytHome)
  │   ├─ db.New()                 → open/create ~/.dwyt/dwyt.db (SQLite)
  │   ├─ brain.MigrateOldMemoryDirs()  → convert old memory.json → .md files
  │   ├─ state.Init()             → load/create ~/.dwyt/state.json
  │   ├─ brain.NewProjectObsidian()  → create/load Obsidian vault
  │   ├─ procman.New()            → create ProcessManager
  │   ├─ procman.Register("codebase", ...) → register Codebase service
  │   ├─ procman.Register("headroom", ...) → register Headroom service
  │   └─ store.TouchProject()     → register project in SQLite
  │
  └─ server.Start()
      ├─ gin router setup
      ├─ SPA middleware (serves embedded React)
      ├─ API routes (/api/*)
      ├─ broadcastLoop()          → SSE every 3s
      ├─ startHeadroomIfNeeded()  → procman.Start("headroom") in goroutine
      └─ r.Run("127.0.0.1:2737")  → blocking listen
```

---

## Obsidian Vault (Knowledge Base)

### Structure

```
~/.dwyt/projects/<sha256[:12]>/
├── index.md                  # project index with structure overview
├── context.md                # full summary, rebuilt from all files
├── instructions/
│   ├── obsidian-law.md       # mandatory memory workflow
│   └── codebase-law.md       # mandatory code graph workflow
├── maps/
│   └── project-map.md        # navigation hub with internal links
├── templates/
│   ├── decision-template.md
│   ├── task-template.md
│   └── session-context-template.md
├── decisions/
│   └── index.md              # architecture decisions log
├── tasks/
│   └── index.md              # active task/status log
├── debug/
│   └── index.md              # investigation notes and failures
├── context/                  # complete task/session context snapshots
├── knowledge/                # knowledge base articles
└── logs/
    ├── sessions/             # legacy session records
    ├── errors/               # legacy error records
    └── commands/             # command records
```

### Obsidian Law

The vault is the official memory of the project. Every AI agent must:

1. Search and summarize Obsidian before acting.
2. Save technical decisions as `decision` and task/status updates as `task` during work.
3. Save debug/investigation notes as `debug` when useful.
4. Save complete context at the end of every relevant task via `/api/obsidian/context`.

Required context fields: `client`, `summary`, `user_request`, `files`, `decisions`, `actions`, `commands`, `errors`, `outcome`, `next_steps`, and `context`.

The vault should remain rich and navigable through folders, internal links, templates, instructions, and project maps. New generated files include links such as `[[instructions/obsidian-law]]`, `[[instructions/codebase-law]]`, `[[maps/project-map]]`, `[[decisions/index]]`, and `[[tasks/index]]`.

Legacy `decisions.md` and `tasks.md` can exist as compatibility pointers, but new entries are routed to folder indexes.

### Codebase Law

The Codebase MCP graph is the primary source for current code structure. Agents should use it before structural diagnosis, refactors, bug fixes, and impact analysis:

1. Validate the project index.
2. Use `search_graph` for symbols and relationships.
3. Use `trace_path` for calls, dependencies, data flow, and impact.
4. Use `get_code_snippet` for exact source before editing.
5. Validate changes and save final context in Obsidian.

Shell search remains appropriate for docs, configs, literal strings, and cases where the graph is unavailable.

### File Format (Frontmatter YAML)

```markdown
---
tags: [dwyt, decision, architecture]
date: 2026-05-04T15:30:00Z
type: decision
---

# Use SQLite for config storage

SQLite provides embedded persistence without external dependencies...
```

### API

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/obsidian/status` | Stats (file count, types, last update) |
| GET | `/api/obsidian/search?q=` | Full-text search across all .md files |
| POST | `/api/obsidian/save` | Save entry `{"type":"decision","content":"..."}` |
| POST | `/api/obsidian/summarize` | Rebuild context.md from all files |
| POST | `/api/obsidian/context` | Save complete task/session context |
| POST | `/api/obsidian/open` | Open vault in Obsidian (`obsidian://open?path=`) |
| POST | `/api/obsidian/open-dir` | Open vault directory in file manager |
| POST | `/api/obsidian/install` | Download and install Obsidian app (Linux: AppImage) |
| GET | `/api/obsidian/install-status` | Obsidian installation progress |

### SaveEntry routing by type

| Entry Type | Destination File |
|-----------|-----------------|
| `decision` | Append to `decisions/index.md` |
| `task` | Append to `tasks/index.md` |
| `debug`, `error` | New file in `debug/` |
| `command` | New file in `logs/commands/` |
| `session` | New file in `logs/sessions/` |
| `context` | New file in `context/` |
| `note` | New file in `knowledge/` |

---

## ProcessManager

Manages daemon services with robust lifecycle control.

### Managed Services

| Service | Default Port | Health URL | Args |
|---------|-------------|------------|------|
| `codebase` | 9749 | `/health` | `--ui=true --port={port}` |
| `headroom` | 8787 | `/health` | `proxy --port {port}` |

### Methods

```
Start(name)  → check PID → find free port → spawn → healthcheck → return status
Stop(name)   → SIGTERM → wait 5s → SIGKILL → return status
Status(name) → return {running, healthy, pid, port, uptime, error}
Logs(name,n) → read last N lines from ~/.dwyt/logs/<name>-*.log
Restart(name)→ Stop + wait 500ms + Start
```

### Healthcheck

```
5 retries with exponential backoff:
  Attempt 1: wait 500ms  → GET http://127.0.0.1:<port>/health
  Attempt 2: wait 1s     → GET ...
  Attempt 3: wait 2s     → GET ...
  Attempt 4: wait 4s     → GET ...
  Attempt 5: wait 8s     → GET ...
  Timeout total: 10s
```

### Port conflicts

If default port is occupied, tries +1, +2, +3, +4. Updates state.json.

### Log capture

Stdout and stderr are captured to:
```
~/.dwyt/logs/<service>-stdout.log
~/.dwyt/logs/<service>-stderr.log
```

Available via `GET /api/services/<service>/logs?tail=50`.
---

## MCP Registry

The MCP registry manages MCP server configurations for AI clients (Claude Desktop, VSCode, Kiro). It is saved to `~/.dwyt/config/mcp-registry.json`.

### Default Servers

| Server | Binary | Endpoint |
|--------|--------|----------|
| `codebase` | `codebase-memory-mcp` | HTTP on port 9749 |
| `obsidian` | `dwyt-obsidian-mcp` | stdio (launched by AI agents) |

### Registry Operations

- **Load** — reads `mcp-registry.json` from `~/.dwyt/config/`, ensures default entries exist
- **Save** — persists changes to disk
- **ConfigureMCP** — writes MCP configs for all enabled servers to Claude Desktop, VSCode, and project AI client configs (`.mcp.json`, `.claude/mcp.json`, `.vscode/mcp.json`, `.kiro/settings/mcp.json`, `.kiro/mcp.json`, `opencode.json`)
- **ConfigureMCPByName(name)** — targets a single MCP server for configuration
- **Toggle(name, enabled)** — enables/disables a server without removing it

### Status Detection

The `/api/mcp/registry` endpoint uses two-tier detection:
1. **ProcMan status** — checks if ProcessManager has the service registered as running+healthy
2. **Direct probe fallback** — if ProcMan is unaware (e.g., service started externally), probes the health-URL port directly. Status "online" when health-check passes, "port_open_no_health" when port is occupied but health fails (shown as 🟡 in UI)

### Config Sync

When `ConfigureMCP` is called (via the "Configure MCP" button on dashboard cards):
1. Saves the registry to disk
2. Creates backup of current entries
3. Writes Claude Desktop config → `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `~/.config/claude-desktop/` (Linux)
4. Writes per-project MCP configs (`.mcp.json`, `.claude/mcp.json`, `.vscode/mcp.json`, `.kiro/settings/mcp.json`, `.kiro/mcp.json`, `opencode.json`)
5. Rolls back on failure

### API Endpoints

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/mcp/registry` | List all servers with status |
| POST | `/api/mcp/configure` | Write MCP configs (optional `name` for single server) |
| POST | `/api/mcp/services/start` | Start a server by name |
| POST | `/api/mcp/services/stop` | Stop a server by name |
| POST | `/api/mcp/services/restart` | Restart a server by name |
| GET | `/api/mcp/services/status?name=` | Get server process status |
| GET | `/api/mcp/services/logs?name=` | Get server logs |

---

## Kiro Power

When Kiro is enabled in setup, DWYT ensures a local Power exists at
`~/.dwyt/powers/dwyt-power` and registers it through the symlink
`~/.kiro/powers/dwyt-power`.

Generated files:

```txt
~/.dwyt/powers/dwyt-power/
├── POWER.md
├── mcp.json              # only codebase + obsidian MCPs when binaries exist
└── steering/
    ├── dwyt-context.md
    ├── obsidian.md
    ├── codebase.md
    ├── rtk.md
    └── headroom.md
```

RTK and Headroom are not MCP servers in the Power. They are steering instructions
because RTK is a CLI and Headroom is an API proxy.

The Power frontmatter is:

```yaml
---
name: dwyt-power
displayName: DWYT Project Context
description: DWYT integration for Codebase MCP, Obsidian memory, RTK command compression and compatible Headroom usage.
keywords:
  - dwyt
  - codebase
  - obsidian
  - mcp
  - memory
  - project memory
  - token savings
  - repo analysis
  - arquitetura
  - refatoracao
  - debugging
  - documentacao
  - contexto do projeto
author: DWYT
---
```

For workspace MCP, DWYT writes `.kiro/settings/mcp.json` as the primary path and `.kiro/mcp.json` only for legacy compatibility. Existing user MCP servers are merged and preserved.

If Kiro cannot be linked automatically, `/api/kiro/power/status` returns an `activation_hint` telling the user to add the local Power path manually.

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/kiro/power/status` | Show installed state, symlink, MCP binary availability, activation hint, and errors |
| POST | `/api/kiro/power/refresh` | Regenerate the Power and recreate the Kiro symlink |

---

## Headroom Proxy

Headroom is a Python HTTP proxy that compresses API calls to AI providers.

### Startup

1. Daemon calls `procman.Start("headroom")`
2. ProcessManager: finds free port → spawns `headroom proxy --port <port>` → healthcheck
3. PID registered in RuntimeState
4. Proxy config injected into client files (AGENTS.md, CLAUDE.md, etc.)

### Auto-config (transparent to user)

`env.sh` exports (injected into shell RC):
```bash
export HEADROOM_PORT=8787
export OPENAI_BASE_URL="http://127.0.0.1:8787/v1"
export ANTHROPIC_BASE_URL="http://127.0.0.1:8787"
```

Client config injection (via `integrate.WriteHeadroomProxyConfig()`):
```markdown
<!-- dwyt:headroom-proxy-start -->
**Headroom proxy is ACTIVE** on http://127.0.0.1:8787
<!-- dwyt:headroom-proxy-end -->
```

### Stop cleanup

`integrate.RemoveHeadroomProxyConfig()` removes all marked blocks from client files.

---

## Codebase (Codebase)

MCP server that provides a knowledge graph of the codebase.

### Default = off

Indexing is **on-demand only**. No automatic indexing on startup or project switch. User clicks "Index" in the UI.

### ProcessManager integration

- Start: `procman.Start("codebase")` with healthcheck
- Stop: graceful (SIGTERM → SIGKILL)
- Logs: captured to `~/.dwyt/logs/codebase-*.log`
- Open Graph: `POST /api/codebase/open-ui` → starts UI on port 9749

### Indexing flow

1. User clicks "Index" → `POST /api/codebase/index {"path":"..."}`
2. Backend spawns `codebase-memory-mcp cli index_repository` in goroutine
3. Frontend polls `GET /api/codebase/index/status` every 2s
4. On completion: `countCodebaseGraph()` walks the cache directory counting nodes/edges from stored JSON files, then marks project as indexed in SQLite with real metrics
5. Indexing has a 10-minute timeout and can be cancelled when switching projects

---

## RTK

CLI tool for terminal output compression. Not a daemon — no process management needed.

### Usage

```bash
rtk git status
rtk cargo test
rtk git log --oneline
```

### Metrics

- `rtk gain` → returns total commands + tokens saved (global)
- `rtk gain --project` → per-project metrics (runs in project directory)
- Parsed by `status.GetRTKMetrics()` and `status.GetRTKMetricsForPath()`

## Tokens Saved Metrics

DWYT separates real telemetry from transparent local estimates:

| Tool | Source | Kind |
|------|--------|------|
| RTK | `rtk gain` / `rtk gain --project` | real metric |
| Headroom | Headroom `/stats` | real metric |
| Codebase MCP | graph metadata such as nodes and edges | local estimate |
| Obsidian MCP | vault markdown count and total bytes | local estimate |

`/api/metrics` and `/api/tool-details` expose `without_dwyt_tokens`, `with_dwyt_tokens`, `tokens_saved`, and `estimation_source` where available. The global dashboard summary includes all tools but stays defensive when a tool is inactive, unindexed, empty, or returning an older response shape.

Codebase estimates use indexed graph size to approximate manual repository exploration avoided by MCP queries. Obsidian estimates use vault markdown bytes to approximate manual context rereads avoided by search/summarize/context APIs. Both are conservative and labeled local until those tools expose native telemetry.

---

## UI Architecture

### Routing (HashRouter)

```
/#/               → Boot component (decides Setup vs Dashboard)
/#/dashboard      → Dashboard (4 tool cards + totals banner)
/#/setup          → Setup Wizard (tools + clients + project)
```

### Query Parameters

| Param | Description |
|-------|------------|
| `?project=/path` | Active project path |
| `?reload=5` | Auto-refresh interval (0/5/10 seconds) |
| `?logs=1` | Show logs panel |
| `?from=dashboard` | Navigation source |

### Data Flow

```
Component mounts
  → GET /api/context       (project, tools, config, all repos)
  → GET /api/status        (tool health)
  → GET /api/tool-details  (per-tool metrics)
  → GET /api/logs          (service status)
  → GET /api/obsidian/status  (obsidian stats)
  → SSE /api/events        (real-time project_switch, status updates)
  → setInterval(pollAll, reloadSecs * 1000) if auto-reload enabled
```

### Components

| Component | File | Responsibility |
|-----------|------|----------------|
| Dashboard | `Dashboard.tsx` | Main dashboard: 4 cards, totals banner, logs, global repo view |
| SetupWizard | `SetupWizard.tsx` | Tool/IA client selection + install progress |
| Sidebar | `Sidebar.tsx` | Project list with switching |
| FileBrowser | `FileBrowser.tsx` | Directory browser for project path selection |
| Button | `Button.tsx` | Unified button with variants (primary, secondary, success, danger, ghost, icon), sizes, loading/disabled states |
| Toggle | `Toggle.tsx` | Generic toggle switch with disabled state |
| Logo | `Logo.tsx` | SVG mascot (nerd character) |
| LangToggle | `LangToggle.tsx` | EN/PT language switcher |

---

## API Endpoints (Complete Reference)

### Core

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/health` | Daemon liveness check |
| GET | `/api/status` | All tool health status |
| GET | `/api/metrics` | RTK + Headroom metrics |
| GET | `/api/events` | SSE stream (3s interval) |
| GET | `/api/cwd` | Current working directory |
| GET | `/api/state` | Runtime state snapshot |
| GET | `/api/context` | Bootstrap (screen, config, projects, memory) |

### Brain (Obsidian)

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/obsidian/status` | Obsidian stats |
| GET | `/api/obsidian/search?q=` | Search vault files |
| POST | `/api/obsidian/save` | Save entry |
| POST | `/api/obsidian/summarize` | Rebuild context.md |
| POST | `/api/obsidian/context` | Save complete task/session context |
| POST | `/api/obsidian/open` | Open in Obsidian |

### MCP Registry

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/mcp/registry` | List all MCP servers with status |
| POST | `/api/mcp/configure` | Write MCP configs (optional `name` for single server) |
| POST | `/api/mcp/services/start` | Start a server by name |
| POST | `/api/mcp/services/stop` | Stop a server by name |
| POST | `/api/mcp/services/restart` | Restart a server by name |
| GET | `/api/mcp/services/status?name=` | Get server process status |
| GET | `/api/mcp/services/logs?name=` | Get server logs |

### ProcessManager

| Method | Route | Purpose |
|--------|-------|---------|
| POST | `/api/services/codebase/start` | Start Codebase via ProcMan |
| POST | `/api/services/codebase/stop` | Stop Codebase via ProcMan |
| GET | `/api/services/codebase/status` | Codebase process status |
| GET | `/api/services/codebase/logs?tail=` | Codebase log tail |
| POST | `/api/services/headroom/start` | Start Headroom via ProcMan |
| POST | `/api/services/headroom/stop` | Stop Headroom via ProcMan |
| GET | `/api/services/headroom/status` | Headroom process status |
| GET | `/api/services/headroom/logs?tail=` | Headroom log tail |

### Headroom (legacy compat)

| Method | Route | Purpose |
|--------|-------|---------|
| POST | `/api/headroom/start` | Start with proxy config injection |
| POST | `/api/headroom/stop` | Stop with proxy config removal |
| GET | `/api/headroom/stats-url` | Get Headroom stats URL |

### Codebase

| Method | Route | Purpose |
|--------|-------|---------|
| POST | `/api/codebase/index` | Trigger indexing |
| GET | `/api/codebase/index/status` | Indexing progress |
| POST | `/api/codebase/open-ui` | Start/open Codebase UI |

### Services

| Method | Route | Purpose |
|--------|-------|---------|
| POST | `/api/services/start-all` | Start all services |
| POST | `/api/services/stop-all` | Stop all services |
| GET | `/api/services/status` | Service health summary |

### Setup / Config

| Method | Route | Purpose |
|--------|-------|---------|
| POST | `/api/setup/save` | Save setup config to SQLite |
| GET | `/api/setup/load` | Load setup config (with migration) |
| GET | `/api/setup/status` | Check if configured |
| POST | `/api/setup/install` | Full install pipeline (async) |
| GET | `/api/install/status` | Install progress |

### Projects

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/projects` | List all tracked projects |
| GET | `/api/projects/current` | Current project with stats |
| POST | `/api/project/switch` | Switch active project |

### Other

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/rtk/gain` | RTK token metrics |
| GET | `/api/tool-details?path=` | Per-tool details |
| GET | `/api/fs/browse?path=` | Filesystem browser |
| GET | `/api/logs` | Tool log status |

---

## File System Layout

```
~/.dwyt/                          # DWYT data directory
├── bin/                          # Tool binaries + dwyt symlink
│   ├── dwyt                      # symlink to binary
│   ├── rtk
│   ├── headroom
│   └── Codebase
├── codebase/                     # Codebase indexes (CBM_CACHE_DIR=~/.dwyt/codebase)
├── data/                         # (reserved)
├── headroom-venv/                # Python virtualenv
├── logs/                         # ProcessManager captured logs
│   ├── codebase-stdout.log
│   ├── codebase-stderr.log
│   ├── headroom-stdout.log
│   └── headroom-stderr.log
├── projects/                     # Per-project data
│   └── <sha12>/                  # project ID = SHA256(path)[:12]
│       ├── obsidian/              # Obsidian vault
│       │   ├── index.md
│       │   ├── context.md
│       │   ├── instructions/
│       │   ├── maps/
│       │   ├── templates/
│       │   ├── decisions/
│       │   ├── tasks/
│       │   ├── debug/
│       │   ├── context/
│       │   ├── knowledge/
│       │   └── logs/
│       ├── project.json          # project metadata + last_open
│       └── headroom-proxy.json   # headroom proxy state (when active)
├── powers/
│   └── dwyt-power/               # Kiro Power files (regenerable)
├── dwyt.db                       # SQLite (projects + config)
├── dwyt.log                      # DWYT log file
├── env.sh                        # Shell environment (sourced in .zshrc)
├── config.json                   # (legacy — now in SQLite)
└── state.json                    # Runtime state (PIDs, ports, errors)
```

> **Note:** DWYT never creates files inside your project directory except the AI client config files
> (`AGENTS.md`, `CLAUDE.md`, `.mcp.json`, etc.) selected during Setup.
> All DWYT state lives exclusively in `~/.dwyt/`.
> `~/.dwyt/projects/` is persistent project memory and is protected from automatic cleanup.

---

## Generated Project Files

The Setup creates these files in the user's project directory:

```
<project>/
├── .mcp.json                     # MCP config (codebase + obsidian servers)
├── AGENTS.md                     # instructions for Codex, Kiro, Cursor, OpenCode
├── CLAUDE.md                     # instructions for Claude Code
├── opencode.json                 # OpenCode config
├── .github/copilot-instructions.md
├── .cursor/rules/dwyt.mdc
├── .claude/mcp.json              # Claude Desktop MCP config
├── .vscode/mcp.json              # VSCode MCP config
├── .kiro/
│   ├── settings/mcp.json         # Kiro MCP config (primary)
│   ├── mcp.json                  # Kiro MCP config (legacy compatibility)
│   └── steering/dwyt.md
└── .gitignore                    # updated with dwyt entries
```

Local files with absolute paths are ignored by default (`.mcp.json`, `.claude/mcp.json`,
`.kiro/settings/mcp.json`, `.kiro/mcp.json`, `.vscode/mcp.json`, `opencode.json`, `CLAUDE.md`, `.cursorrules`).
Shared instruction files such as `AGENTS.md`, `.cursor/rules/dwyt.mdc`,
`.kiro/steering/dwyt.md`, and `.github/copilot-instructions.md` are not ignored by DWYT.

### Instruction Priority (all files)

```
1. RTK          → prefix shell commands with 'rtk'
2. Codebase MCP → use the graph before structural code work
3. Obsidian MCP → recover memory and save context
4. Headroom     → compatible proxy/cache optimization only
```

All generated instruction files also enforce the Codebase Law and Obsidian Law, require append-only safe DWYT blocks, and preserve user content outside managed sections. See [CODEBASE-LAW.md](CODEBASE-LAW.md) and [OBSIDIAN-LAW.md](OBSIDIAN-LAW.md).

---

## Development & Build

### Local Build

```bash
# Frontend
cd core/web
npm install
npm run build              # outputs to ../internal/server/dashboard/dist/

# Backend
cd core
go build -o dwyt .         # embeds frontend via //go:embed

# Install locally
cp dwyt ~/.local/bin/dwyt
```

### Automated Releases

DWYT uses **automatic releases on every commit** to `main`. See [RELEASE-PROCESS.md](RELEASE-PROCESS.md) for details.

**Quick Summary:**
- Every push to `main` triggers a release
- Version is auto-calculated from commit messages (SemVer)
- Changelog is auto-generated and categorized
- Binaries built for 5 platforms (Linux, macOS, Windows)

**Commit Message Convention:**
```bash
feat: new feature        # Minor version bump (0.x.0)
fix: bug fix            # Patch version bump (0.0.x)
breaking: breaking change # Major version bump (x.0.0)
docs: documentation     # Patch bump
chore: maintenance      # Patch bump
```

**Example:**
```bash
git commit -m "fix: resolve race condition in ProcessManager"
git push origin main
# → Automatically creates release with version bump and changelog
```

### CI Release (Automated)

Push to `main` triggers `.github/workflows/release.yml`:

1. Checkout + setup Go 1.25 + Node 22
2. `npm ci && npm run build` (frontend)
3. Calculate version from commit messages (SemVer)
4. Generate categorized changelog
5. Create and push version tag
6. GoReleaser: build for 5 platforms (linux/darwin/windows × amd64, linux arm64)
7. Create GitHub Release with:
   - Version tag
   - Categorized changelog
   - Binary archives
   - SHA256 checksums
   - Installation instructions

### GoReleaser Config

```yaml
builds:
  - id: linux-amd64     → GOOS=linux   GOARCH=amd64
  - id: linux-arm64     → GOOS=linux   GOARCH=arm64
  - id: darwin-amd64    → GOOS=darwin  GOARCH=amd64
  - id: darwin-arm64    → GOOS=darwin  GOARCH=arm64
  - id: windows-amd64   → GOOS=windows GOARCH=amd64

archives:
  - formats: [tar.gz]   → dwyt_linux_amd64.tar.gz
  - formats: [zip]      → dwyt_windows_amd64.zip (Windows only)

ldflags: -s -w -X main.version={{.Version}}
```

### Version Injection

```go
// main.go
var version = "dev"           // default for local builds

// CLI
cli.SetVersion(version)       // passed from main
root.SetVersion(v)            // stored in package var

// version command
fmt.Printf("dwyt %s — Don't Waste Your Tokens\n", version)
```

---

## Data Flow: AI Client + DWYT

```
User runs: dwyt .
  │
  ├─ DWYT starts daemon → Obsidian vault loaded
  ├─ Headroom starts → env.sh exports proxy vars
  ├─ UI opens → Dashboard with project stats
  │
  └─ User opens Claude Code / Codex / Cursor (in new terminal)
       │
       ├─ Shell sources ~/.dwyt/env.sh
       │   → OPENAI_BASE_URL=http://127.0.0.1:8787/v1
       │   → ANTHROPIC_BASE_URL=http://127.0.0.1:8787
       │
       ├─ Shell commands prefixed with rtk
       │   → 60-98% output compression
       │
       ├─ AI client reads AGENTS.md / CLAUDE.md
       │   → Instructed to use Codebase MCP for structural code work
       │   → search_graph / trace_path / get_code_snippet
       │
       ├─ For project memory:
       │   → GET /api/obsidian/search?q=<task description>
       │   → POST /api/obsidian/summarize
       │
       ├─ Compatible API clients may pass through Headroom proxy
       │   → compression/cache optimization when supported
       │
       ├─ During important changes:
           → POST /api/obsidian/save {"type":"decision","content":"..."}
       │   → POST /api/obsidian/save {"type":"task","content":"..."}
       │
       └─ At task end:
           → POST /api/obsidian/context {"client":"...","user_request":"...","summary":"...","files":["..."],"decisions":["..."],"actions":["..."],"commands":["..."],"errors":["..."],"outcome":"...","next_steps":["..."],"context":"..."}
```

---

## Key Design Decisions

1. **Single binary** — no runtime dependencies. Frontend embedded via `//go:embed`.
2. **Everything via UI** — no CLI configuration commands. Setup Wizard handles all tool/IA selection.
3. **Obsidian as memory** — markdown files are universal, version-controllable, and visually navigable in Obsidian. Vaults preserve decisions, tasks, debug notes, and handoff context.
4. **ProcessManager** — centralized process lifecycle with healthchecks, log capture, and graceful shutdown. Prevents zombie processes.
5. **Codebase as structure source** — structural analysis should use graph tools before manual file exploration when available.
6. **Headroom transparency** — proxy config injected/removed automatically only for compatible clients. Codex ChatGPT/OAuth is not proxied.
7. **Codebase on-demand** — no automatic indexing. User controls when to index their codebase. Non-blocking if fails.
8. **Resilience** — each tool can fail independently without crashing the dashboard. Errors are displayed in the UI and logged.
9. **RTK preservation** — simplest and most reliable tool, left unchanged from its original design. Just prefix commands with `rtk`.

## Uninstall

To completely remove DWYT from your system:

```bash
dwyt uninstall
```

This command performs cleanup while preserving project vaults in `~/.dwyt/projects/`:

1. **Stops all running processes** — daemon, Headroom, Codebase, RTK
2. **Cleans tool/config data under `~/.dwyt/`** — bins, SQLite database, `state.json`, logs, Headroom venv, and regenerable cache
3. **Removes symlinks** from `~/.local/bin/` — `dwyt`, `rtk`, `headroom`, `codebase-memory-mcp`
4. **Removes RTK data** — `~/.rtk/`, `~/.config/rtk/`, `~/.local/share/rtk/`, RTK binaries outside `~/.dwyt`
5. **Removes Headroom data** — `~/.headroom/`, `~/.config/headroom/`, pip uninstall `headroom-ai`
6. **Removes Codebase cache** — `~/.dwyt/codebase/` (indexes), `~/.cache/codebase-memory-mcp/` (fallback), runs `codebase-memory-mcp uninstall -y`
7. **Cleans shell RC files** — removes the `# dwyt:source` block from `.zshrc`, `.bashrc`, `.zprofile`, `.profile`

> **Note:** The Obsidian app itself is NOT removed, and DWYT-managed project vaults inside `~/.dwyt/projects/` are preserved.

> **Windows:** also removes the `dwytBin` entry from `HKCU\Environment\PATH` and cleans the PowerShell profile.

After uninstall, restart your terminal to apply shell changes.

---

DWYT documentation follows a structured approach to maintain clarity and historical context.

### Documentation Structure

```
docs/
├── CHANGELOG.md              # Chronological list of all changes (organized by date)
├── HOW-IT-WORKS.md          # This file - always kept up-to-date with latest architecture
├── CODEBASE-LAW.md          # Mandatory code graph workflow for agents
├── OBSIDIAN-LAW.md          # Mandatory memory workflow for agents
├── TOKENS-SAVED.md          # Real metrics and local estimate formulas
├── KIRO-POWER.md            # Kiro Power paths, frontmatter, and MCP behavior
└── DDMMYYYY/                # Date-specific folders for detailed change documentation
    ├── FIXES.md             # Technical details of fixes implemented on this date
    ├── SUMMARY.md           # Final status, test results, and executive summary
    └── VALIDATION.md        # Validation commands and procedures
```

### Documentation Maintenance Rules

**CRITICAL:** Follow these rules when making changes to DWYT:

1. **NEVER create documentation in the root directory** (except README.md and LICENSE)
   - All documentation must be in `docs/` folder
   - Use dated folders for change-specific documentation

2. **ALWAYS update HOW-IT-WORKS.md** when making architectural changes
   - This file is the single source of truth for current architecture
   - Update relevant sections immediately after code changes
   - Keep it synchronized with the actual codebase

3. **ALWAYS update CHANGELOG.md** with every release
   - Add new entries at the top (most recent first)
   - Use date format: YYYY-MM-DD
   - Include version number and brief description
   - Reference the dated folder for detailed information

4. **Create dated folders with EXACTLY 3 files**
   - Format: `docs/DDMMYYYY/` (e.g., `docs/04052026/`)
   - Required files: `FIXES.md`, `SUMMARY.md`, `VALIDATION.md`
   - No additional files allowed - consolidate content into these 3 files

### When to Create Dated Documentation

Create a new dated folder (`docs/DDMMYYYY/`) when:
- Fixing critical bugs or security issues
- Implementing major features
- Making architectural changes
- Releasing a new version
- Making changes that require validation procedures

### Dated Folder Contents (EXACTLY 3 FILES)

Each dated folder must contain exactly these 3 files:

**FIXES.md** (Required)
- Technical details of all fixes/changes
- Code snippets showing before/after
- Impact analysis for each change
- Root cause analysis for bugs
- Implementation details

**SUMMARY.md** (Required)
- Executive summary and ROI
- Final status of changes
- Test results and metrics
- Files modified
- Next steps and recommendations
- Suggested commit message
- Lessons learned

**VALIDATION.md** (Required)
- Commands to validate the changes
- Manual testing procedures
- Expected results
- Troubleshooting guide
- Performance testing
- Debugging commands

### Example Workflow

When implementing changes:

```bash
# 1. Make code changes
vim core/internal/procman/procman.go

# 2. Create dated folder
mkdir -p docs/$(date +%d%m%Y)

# 3. Document changes (EXACTLY 3 files)
vim docs/$(date +%d%m%Y)/FIXES.md       # Technical details
vim docs/$(date +%d%m%Y)/SUMMARY.md     # Status + executive summary + commit message
vim docs/$(date +%d%m%Y)/VALIDATION.md  # Validation commands

# 4. Update main documentation
vim docs/HOW-IT-WORKS.md      # Update architecture sections
vim docs/CHANGELOG.md          # Add entry at the top

# 5. Commit everything together
git add .
git commit -m "fix: critical stability improvements (v3.1.0)"
```

### Documentation Best Practices

1. **Be specific and technical** in dated folders
   - Include code snippets
   - Show before/after comparisons
   - Explain root causes

2. **Keep HOW-IT-WORKS.md current** 
   - Update immediately after changes
   - Remove outdated information
   - Add new sections as needed

3. **Make CHANGELOG.md scannable**
   - Use consistent formatting
   - Group related changes
   - Link to dated folders for details

4. **Consolidate content properly**
   - FIXES.md: Technical implementation details
   - SUMMARY.md: Executive summary, ROI, status, commit message, lessons learned
   - VALIDATION.md: All testing and validation procedures

5. **Write for different audiences**
   - FIXES.md: developers and code reviewers
   - SUMMARY.md: team leads, QA, and management
   - VALIDATION.md: QA engineers and testers
   - HOW-IT-WORKS.md: new developers and contributors

### Finding Documentation

**For current architecture:** Read `docs/HOW-IT-WORKS.md`

**For agent laws:** Read `docs/CODEBASE-LAW.md` and `docs/OBSIDIAN-LAW.md`

**For savings calculations:** Read `docs/TOKENS-SAVED.md`

**For Kiro integration:** Read `docs/KIRO-POWER.md`

**For recent changes:** Check `docs/CHANGELOG.md` (top entries)

**For specific date:** Navigate to `docs/DDMMYYYY/` folder

**For historical context:** Browse dated folders chronologically
