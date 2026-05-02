# DWYT Orchestrator v3 — Fase 1: Core CLI + Dashboard

> Status: **CONCLUÍDO** (2026-05-01)

## Objetivo

Migrar `dwyt.sh` (~2850 linhas Bash) para CLI em **Go** com **dashboard web local** em `http://localhost:2737`. Zero regressão. CLI é o core; dashboard é facilitador visual.

---

## Arquitetura

```
dwyt-orchestrator/
├── main.go
├── go.mod / go.sum
├── .goreleaser.yaml
├── cmd/dwyt/cli/
│   ├── execute.go
│   └── root/
│       ├── root.go              # flags: -d, -t, -l, -R + subcomandos
│       ├── install.go           # dwyt install + runInstall()
│       ├── repo.go              # dwyt repo / reinstall / uninstall
│       └── dashboard.go         # dwyt dashboard / status / version
└── internal/
    ├── detect/   detect.go      # OS, shell, arch, paths (~/.dwyt)
    ├── deps/     deps.go        # verifica curl, git, python3, node
    ├── install/  
    │   ├── install.go           # CBMCP, RTK, Headroom, MemStack
    │   └── integrate.go         # gera todos os arquivos de projeto
    ├── env/      env.go         # env.sh + shell RC injection
    ├── state/    state.go       # ~/.dwyt/state.json
    ├── status/   status.go      # polling de status e métricas
    └── server/
        ├── server.go            # Gin HTTP + REST API + SSE
        └── dashboard/
            ├── index.html       # grid 2x2, dark theme
            ├── app.js           # SSE + polling + ações
            └── style.css        # CSS puro, sem framework
```

---

## Decisões técnicas

| O que | Escolha | Motivo |
|---|---|---|
| Linguagem | Go 1.26+ | Binário estático, cross-compile |
| CLI | `cobra` + flags nativas | Padrão Go |
| HTTP | `gin-gonic/gin v1.12` | Roteamento + middleware + SSE |
| Dashboard | HTML+JS+CSS vanilla | Zero build step, `embed.FS` (~5KB) |
| Comunicação | REST + SSE | Simples, suficiente |
| Estado | `~/.dwyt/state.json` | Persistência sem DB |
| Build | `goreleaser` | 4 targets (linux/darwin × amd64/arm64 + windows) |

---

## Comandos CLI

```
dwyt                          # modo interativo (prompts de texto)
dwyt -d                       # dashboard em localhost:2737
dwyt -t crhm                  # instalar ferramentas (c=codebase, r=rtk, h=headroom, m=memstack)
dwyt -l cxo                   # integrar clientes (c=claude, x=codex, o=opencode, p=copilot, k=kiro, r=cursor)
dwyt -R <path>                # caminho do repositório
dwyt dashboard --port 2737    # porta customizada
dwyt status                   # status ao vivo das 4 ferramentas
dwyt repo <path>              # integrar e indexar um repo
dwyt reinstall                # limpar ~/.dwyt
dwyt uninstall                # remover tudo
dwyt version                  # dwyt v3.0.0
```

---

## Mapeamento `dwyt.sh` → Go (checklist concluído)

| Bash | Go | Status |
|---|---|---|
| `detect_env()` | `internal/detect` | ✅ |
| `check_deps()` | `internal/deps` | ✅ |
| `select_tools()` | `runInstall()` (flag -t) | ✅ |
| `select_clients()` | `runInstall()` (flag -l) | ✅ |
| `select_repo()` | `runInstall()` (flag -R) | ✅ |
| `install_cbmcp()` | `internal/install.CBMCP()` | ✅ |
| `install_rtk()` | `internal/install.RTK()` | ✅ |
| `install_headroom()` | `internal/install.Headroom()` | ✅ |
| `install_memstack()` | `internal/install.MemStack()` | ✅ |
| `integrate_project()` | `internal/install.Integrate()` | ✅ |
| `finalize_env()` | `internal/env` | ✅ |
| `start_ui()` | via shell script (dwyt-ui symlink) | ✅ |
| `show_summary()` | output no terminal | ✅ |

---

## Dashboard — API REST

```
GET  /api/status            # status agregado das 4 ferramentas (JSON)
GET  /api/metrics           # métricas RTK + Headroom
GET  /api/events            # SSE (3s broadcast)
POST /api/codebase/index    # indexa repo {path: string}
POST /api/headroom/start    # inicia proxy
POST /api/headroom/stop     # para proxy
GET  /api/rtk/gain          # tokens economizados
POST /api/memstack/search   # busca {query: string}
```

---

## `state.json` — formato

```json
{
  "version": "3.0.0",
  "installed_at": "...",
  "tools": {
    "cbmcp":    {"installed": true},
    "rtk":      {"installed": true},
    "headroom": {"installed": true, "port": 8787},
    "memstack": {"installed": true}
  },
  "clients": ["claude", "codex", "opencode"],
  "integrated_projects": {
    "/path/to/repo": {"path": "...", "indexed_at": "...", "nodes": 82, "edges": 190}
  },
  "metrics": {"rtk_tokens_saved": 30900000, "headroom_tokens_saved": 0}
}
```

---

## Binários (cross-compile)

```
build/
├── dwyt-linux-amd64        (22 MB)
├── dwyt-darwin-amd64       (22 MB)
├── dwyt-darwin-arm64       (21 MB)
└── dwyt-windows-amd64.exe  (22 MB)
```

---

## Arquivos de projeto gerados por cliente

| Cliente | Arquivos |
|---|---|
| Claude Code | `.claude/CLAUDE.md` |
| Codex | `AGENTS.md`, `.codex/` (dir) |
| GitHub Copilot | `.github/copilot-instructions.md` |
| Kiro | `.kiro/steering/dwyt.md` |
| Cursor | `.cursor/rules/dwyt.mdc` |
| OpenCode | `opencode.json`, `AGENTS.md` |
| Todos | `.mcp.json`, `.gitignore` |

---

## Pendências da Fase 1 (pós-lançamento)

- [ ] TUI interativa com bubbletea (hoje usa prompts de texto via stdin)
- [ ] Wrappers `dwyt-codex` / `dwyt-opencode` / `dwyt-ui` totalmente em Go
- [ ] Testes de integração em container Docker limpo
- [ ] `dwyt daemon` — modo serviço (systemd/launchd)
