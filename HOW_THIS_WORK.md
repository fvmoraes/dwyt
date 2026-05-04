# HOW THIS WORKS — DWYT Architecture & Internals

## Overview

DWYT (Don't Waste Your Tokens) is a self-contained, single-binary orchestrator that reduces AI token consumption by managing four tools behind a unified web UI.

```
User runs: dwyt .
  → Detects project directory
  → Creates/loads Obsidian vault (~/.dwyt/projects/<id>/brain/)
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
│  │ │polling │ │probes  │ │env.sh  │ │cbmcp/rtk/  │  │   │
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
| `internal/install` | `install.go` | Tool installers: CBMCP, RTK, Headroom |
| `internal/env` | `env.go` | Shell RC injection, env.sh, PATH symlinks |
| `internal/db` | `db.go` | SQLite store: projects table, config key-value |
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
  ├─ 4. probeDaemon()             → checks if daemon already running on :2737
  │    └─ YES → switchProject()   → POST /api/project/switch, open browser, exit
  │    └─ NO  → continue
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
  │   ├─ brain.NewProjectBrain()  → create/load Obsidian vault
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

## Obsidian Brain (ProjectBrain)

### Structure

```
~/.dwyt/projects/<sha256[:12]>/brain/
├── index.md              # project index with structure overview
├── context.md            # full summary (auto-rebuilt from all files)
├── decisions.md          # architecture decisions (append-only log)
├── tasks.md              # active tasks (append-only checklist)
├── knowledge/            # knowledge base articles (timestamped files)
└── logs/                 # sessions, errors, commands
```

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
| GET | `/api/brain/status` | Stats (file count, types, last update) |
| GET | `/api/brain/search?q=` | Full-text search across all .md files |
| POST | `/api/brain/save` | Save entry `{"type":"decision","content":"..."}` |
| POST | `/api/brain/summarize` | Rebuild context.md from all files |
| POST | `/api/brain/forget` | Clear all brain files |
| POST | `/api/brain/open` | Open vault in Obsidian (`obsidian://open?path=`) |

### SaveEntry routing by type

| Entry Type | Destination File |
|-----------|-----------------|
| `decision` | Append to `decisions.md` |
| `task` | Append to `tasks.md` |
| `error`, `command`, `session` | New file in `logs/` |
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

## Codebase (codebase-memory-mcp)

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
4. On completion: marks project as indexed in SQLite

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
  → GET /api/brain/status  (brain stats)
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
| GET | `/api/brain/status` | Brain stats |
| GET | `/api/brain/search?q=` | Search brain files |
| POST | `/api/brain/save` | Save entry |
| POST | `/api/brain/summarize` | Rebuild context.md |
| POST | `/api/brain/forget` | Clear all entries |
| POST | `/api/brain/open` | Open in Obsidian |

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
│   └── codebase-memory-mcp
├── data/                         # (reserved)
├── headroom-venv/                # Python virtualenv
├── logs/                         # ProcessManager captured logs
│   ├── codebase-stdout.log
│   ├── codebase-stderr.log
│   ├── headroom-stdout.log
│   └── headroom-stderr.log
├── projects/                     # Per-project data
│   └── <sha12>/                  # project ID = SHA256(path)[:12]
│       ├── brain/                # Obsidian vault
│       │   ├── index.md
│       │   ├── context.md
│       │   ├── decisions.md
│       │   ├── tasks.md
│       │   ├── knowledge/
│       │   └── logs/
│       └── project.json          # project metadata
├── dwyt.db                       # SQLite (projects + config)
├── dwyt.log                      # DWYT log file
├── env.sh                        # Shell environment (sourced in .zshrc)
├── config.json                   # (legacy — now in SQLite)
└── state.json                    # Runtime state (PIDs, ports, errors)
```

---

## Generated Project Files

The Setup creates these files in the user's project directory:

```
<project>/
├── .mcp.json                     # codebase-memory-mcp MCP config
├── AGENTS.md                     # instructions for Codex, Kiro, Cursor, OpenCode
├── CLAUDE.md                     # instructions for Claude Code
├── opencode.json                 # OpenCode config
├── .github/copilot-instructions.md
├── .cursor/rules/dwyt.mdc
├── .kiro/steering/dwyt.md
└── .gitignore                    # updated with dwyt entries
```

### Instruction Priority (all files)

```
1. Obsidian FIRST  → consult vault before any operation
2. Headroom        → auto-detected via env vars
3. RTK             → prefix shell commands with 'rtk'
4. Codebase MCP    → ONLY for structural code exploration
```

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

### CI Release

Push to `main` or a `v*` tag triggers `.github/workflows/release.yml`:

1. Checkout + setup Go 1.25 + Node 22
2. `npm ci && npm run build` (frontend)
3. Generate changelog from `git log`
4. GoReleaser: build for 5 platforms (linux/darwin/windows × amd64, linux arm64)
5. Create GitHub Release (draft) with archives + checksums
6. Append changelog to release body

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
       ├─ AI client reads AGENTS.md / CLAUDE.md
       │   → Instructed to query Obsidian vault FIRST
       │   → GET /api/brain/search?q=<task description>
       │
       ├─ API calls pass through Headroom proxy
       │   → ~34% token compression
       │
       ├─ Shell commands prefixed with rtk
       │   → 60-98% output compression
       │
       └─ After important changes:
           → POST /api/brain/save {"type":"decision","content":"..."}
```

---

## Key Design Decisions

1. **Single binary** — no runtime dependencies. Frontend embedded via `//go:embed`.
2. **Everything via UI** — no CLI configuration commands. Setup Wizard handles all tool/IA selection.
3. **Obsidian as brain** — markdown files are universal, version-controllable, and visually navigable in Obsidian. No custom database for project knowledge.
4. **ProcessManager** — centralized process lifecycle with healthchecks, log capture, and graceful shutdown. Prevents zombie processes.
5. **Headroom transparency** — proxy config injected/removed automatically. env.sh sources into shell RC. User never touches env vars.
6. **Codebase on-demand** — no automatic indexing. User controls when to index their codebase. Non-blocking if fails.
7. **Resilience** — each tool can fail independently without crashing the dashboard. Errors are displayed in the UI and logged.
8. **RTK preservation** — simplest and most reliable tool, left unchanged from its original design. Just prefix commands with `rtk`.
