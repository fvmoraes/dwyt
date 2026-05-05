# HOW THIS WORKS — DWYT Architecture & Internals

## Overview

DWYT (Don't Waste Your Tokens) is a self-contained, single-binary orchestrator that reduces AI token consumption by managing four tools behind a unified web UI.

```
User runs: dwyt .
  → Detects project directory
  → Creates/loads Obsidian vault (~/.dwyt/projects/<id>/obsidian/)
  → Starts Headroom proxy in background (port 8787)
  → Codebase sits idle (on-demand indexing)
  → RTK is active as CLI tool
  → Serves React UI at http://localhost:2737
```

---

## Quick Start

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash

# Use
cd ~/my-project
dwyt .
# UI opens at http://localhost:2737
```

## Technology Stack

| Layer | Technology |
|-------|-----------|
| **Binary** | Go 1.25 — single static binary, embeds React via `//go:embed` |
| **Frontend** | React 19 + TypeScript + Vite 8 + Tailwind |
| **HTTP Server** | Gin (Go) — 35+ API routes, SSE broadcasting |
| **Database** | SQLite — project catalog, config storage |
| **CLI** | Cobra (Go) |
| **Releases** | GoReleaser + GitHub Actions — 5 platforms, auto-changelog |

## Architecture

### Tools Orchestrated

| Tool | Purpose | Default |
|------|---------|---------|
| **Obsidian** | Project vault — markdown knowledge base | ✅ Mandatory |
| **Headroom** | API call compression (~34%) | Optional |
| **Codebase** | Code graph — structural exploration | Optional |
| **RTK** | Terminal output compression (60-98%) | Optional |

### Packages

```
core/
├── main.go                    # Entry point, version injection via ldflags
├── cmd/dwyt/cli/root/         # Cobra CLI: daemon, stop, status, version
├── internal/
│   ├── server/server.go       # Gin HTTP server, 39 API routes, embedded SPA
│   ├── brain/brain.go         # Obsidian vault: markdown + frontmatter YAML
│   ├── procman/procman.go     # ProcessManager: Start/Stop/Status/Logs
│   ├── integrate/integrate.go # AI client file generator (AGENTS.md, etc.)
│   ├── state/state.go         # RuntimeState: PIDs, ports, errors
│   ├── status/status.go       # Tool health polling, metrics parsing
│   ├── health/health.go       # HTTP health probes, service start/stop
│   ├── install/install.go     # Tool installers (CBMCP, RTK, Headroom)
│   ├── env/env.go             # Shell RC injection, env.sh, PATH symlinks
│   ├── db/db.go               # SQLite store: projects, config key-value
│   ├── detect/detect.go       # OS/Shell/Home detection
│   └── log/log.go             # File-based logger
└── web/                       # React + TypeScript + Tailwind + Vite
```

## Startup Flow

```
dwyt .
  ├─ detect.Detect()         → OS, shell, home dir, DWYT paths
  ├─ env.Init()              → creates ~/.dwyt/env.sh, injects into .zshrc
  ├─ probeDaemon()           → if daemon running, switch project via API
  ├─ spawnDaemon()           → detached process with Setsid
  └─ openBrowser()           → xdg-open http://localhost:2737
```

## Obsidian Vault (Brain)

Each project gets an Obsidian-compatible vault at `~/.dwyt/projects/<id>/obsidian/`:

```
obsidian/
├── index.md              # project index
├── context.md            # full summary (auto-rebuilt)
├── decisions.md          # architecture decisions
├── tasks.md              # active tasks checklist
├── knowledge/            # knowledge base articles
└── logs/                 # sessions, errors, commands
```

All files use frontmatter YAML (`tags`, `date`, `type`). Search is Go-native via `filepath.Walk` + `strings.Contains`.

### API Endpoints

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/obsidian/search?q=` | Full-text search across .md files |
| POST | `/api/obsidian/save` | Save entry `{"type":"decision","content":"..."}` |
| POST | `/api/obsidian/summarize` | Rebuild context.md |
| POST | `/api/obsidian/forget` | Clear all entries |
| POST | `/api/obsidian/open` | Open in Obsidian or file manager |

## ProcessManager

Manages Codebase and Headroom as daemon services:

| Method | Description |
|--------|-------------|
| `Start(name)` | Check PID → find free port → spawn → healthcheck (5 retries, 10s timeout) |
| `Stop(name)` | SIGTERM → wait 5s → SIGKILL |
| `Status(name)` | Return `{running, healthy, pid, port, uptime, error}` |
| `Logs(name, n)` | Last N lines from `~/.dwyt/logs/<name>-*.log` |

## Headroom Proxy

Auto-started with the daemon. `env.sh` exports:
```bash
export HEADROOM_PORT=8787
export OPENAI_BASE_URL="http://127.0.0.1:8787/v1"
export ANTHROPIC_BASE_URL="http://127.0.0.1:8787"
```

Proxy config injected/removed automatically in client files when started/stopped.

## Codebase

Indexing is **on-demand only**. No auto-indexing on startup or project switch. User clicks "Index" in the UI. ProcessManager handles lifecycle with healthcheck, log capture, and port auto-select.

## UI Architecture

**Routing**: HashRouter with `/#/dashboard` and `/#/setup`.

**Dashboard**: 4 tool cards (Obsidian, Headroom, RTK, Codebase) with real-time status via SSE polling. Global view shows all repositories when no project is selected.

**Setup Wizard**: Obsidian is mandatory (pre-selected, cannot uncheck). Other tools optional.

## Data Directory

```
~/.dwyt/
├── bin/                # Tool binaries + dwyt symlink
├── obsidian/           # Codebase graph data (CBM_CACHE_DIR)
├── headroom-venv/      # Python virtualenv
├── logs/               # ProcessManager stdout/stderr
├── projects/<id>/      # Per-project data
│   └── obsidian/       # Obsidian vault
├── dwyt.db             # SQLite (projects + config)
├── dwyt.log            # DWYT log file
├── env.sh              # Shell environment
└── state.json          # Runtime state (PIDs, ports, errors)
```

## Build & Release

```bash
# Local
cd core
go build -o dwyt .

# CI (GitHub Actions)
# Push to main or v* tag triggers:
#   npm ci && npm run build  →  GoReleaser  →  GitHub Release (draft)
```

GoReleaser builds for 5 platforms: linux/darwin/windows × amd64, linux arm64. Version injected via `-X main.version={{.Version}}`.

---

## Full Technical Documentation

See **[docs/HOW-IT-WORKS.md](docs/HOW-IT-WORKS.md)** for the complete 818-line reference covering all API endpoints, data flow diagrams, configuration details, and troubleshooting.
