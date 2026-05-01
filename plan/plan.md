# DWYT Orchestrator — Go CLI + Dashboard (v3.0)

## Objetivo

Migrar `dwyt.sh` (~2850 linhas Bash) para uma CLI em **Go** com **dashboard web local** em `http://localhost:2737`. Zero regressão: toda funcionalidade atual é preservada. A CLI continua sendo o core; a UI é um facilitador visual.

---

## Arquitetura

```
dwyt (Go binary)
├── cmd/
│   ├── install/      # ./dwyt (modo interativo, checklist + navegador de repo)
│   ├── repo/         # ./dwyt --repo <path>    (integrar repo sem reinstalar)
│   ├── reinstall/    # ./dwyt --reinstall      (limpa ~/.dwyt e reinstala)
│   ├── uninstall/    # ./dwyt --uninstall
│   └── dashboard/    # ./dwyt dashboard        (sobe UI e mantém no ar)
├── internal/
│   ├── detect/       # detecta OS, shell, distro, arch
│   ├── deps/         # verifica/instala curl, git, python3, node
│   ├── install/      # instala cbmcp, rtk, headroom, memstack
│   │   ├── cbmcp.go
│   │   ├── rtk.go
│   │   ├── headroom.go
│   │   └── memstack.go
│   ├── integrate/    # gera arquivos de projeto (.mcp.json, opencode.json, AGENTS.md, etc.)
│   ├── env/          # gerencia dwyt/env.sh, PATH, shell rc injection
│   ├── status/       # poll de status das 4 ferramentas (para dashboard)
│   └── config/       # parsing/serialização de ~/.dwyt/state.json
├── web/
│   └── dashboard/    # SPA estática (HTML+JS vanilla, ~50KB)
│       ├── index.html
│       ├── app.js
│       └── style.css
├── go.mod
├── go.sum
└── main.go           # entrypoint, registra comandos cobra
```

### Decisões técnicas

| O que | Escolha | Motivo |
|---|---|---|
| Linguagem | Go 1.26+ | Binário estático único, cross-compile trivial, sem runtime |
| CLI framework | `cobra` + `viper` | Padrão do ecossistema Go |
| UI frontend | HTML + vanilla JS (sem framework) | Zero build step, ~50KB total, embute com `embed.FS` |
| UI estilo | CSS puro, grid 2x2, dark theme | Leve, sem dependências externas |
| Comunicação | REST (`/api/status`) + SSE (`/api/events`) | Simples, suficiente para métricas em tempo real |
| Build | `goreleaser` (linux/macOS arm64+amd64) | CI/CD pronto, brew tap incluso |
| Estado | `~/.dwyt/state.json` em disco | Persiste o que foi instalado, sem DB externo |

### Por que não React/Astro/Tailwind?

O dashboard é 4 quadrantes estáticos com atualização periódica. Um HTML de 200 linhas + 300 linhas de JS vanilla resolve. Build step zero, bundle de 50KB embutido no binário via `embed.FS`. Sem dependência de Node.js para buildar a UI.

---

## Ciclo de vida: da Bash ao Go

### Mapeamento `dwyt.sh` → `dwyt (Go)`

| Função Bash | Pacote Go | Notas |
|---|---|---|
| `detect_env()` | `internal/detect` | OS, shell rc, arch |
| `check_deps()` | `internal/deps` | curl, git, python3, node |
| `select_tools()` | `cmd/install` | Usa [bubbletea](https://github.com/charmbracelet/bubbletea) para TUI interativa |
| `select_clients()` | `cmd/install` | Mesmo TUI |
| `select_repo()` | `cmd/install` | File picker com bubbletea |
| `install_cbmcp()` | `internal/install/cbmcp` | Download binário, configura PATH |
| `install_rtk()` | `internal/install/rtk` | Download binário, `rtk init -g` |
| `install_headroom()` | `internal/install/headroom` | Cria venv, pip install, wrapper |
| `install_memstack()` | `internal/install/memstack` | git clone, configura |
| `integrate_project()` | `internal/integrate` | Gera .mcp.json, opencode.json, AGENTS.md, etc. |
| `finalize_env()` | `internal/env` | Escreve env.sh, injeta no shell rc |
| `start_ui()` | `internal/install/cbmcp` | Sobe codebase-memory-mcp-ui na porta 9749 |
| `show_summary()` | `cmd/install` | Output no terminal + notificação no dashboard |

### Validação de regressão

Cada pacote Go é testado contra o comportamento atual do `dwyt.sh`. O teste de aceitação é: rodar `./dwyt --reinstall` (Go) e `./dwyt.sh --reinstall` (Bash) em um container limpo e comparar `diff -r ~/.dwyt/`.

---

## Comandos CLI

```
dwyt                          # modo interativo (checklist + navegador de repo)
dwyt --repo <path>            # integrar e indexar um repo sem reinstalar
dwyt --reinstall              # apagar ~/.dwyt e reinstalar tudo do zero
dwyt --uninstall              # remover tudo
dwyt dashboard                # iniciar dashboard na porta 2737
dwyt dashboard --port 2737    # porta customizada
dwyt status                   # status rápido das 4 ferramentas (texto)
dwyt version                  # versão do binário Go
```

### Subcomandos de atalho (wrappers existentes retirar, reimplementados em Go)

```
dwyt -h codex, opencode, claude e etc (fazer para todas)... # ≡ dwyt-codex (configura apenas para aquela IA)
dwyt -t codebase, rtk, memstack e headroom (fazer para todas)... # ≡ dwyt  (Configura apenas para aquela Ferramenta)
dwyt -d # ≡ dwyt ui localhost na 2737
```

---

## Dashboard Web (`http://localhost:2737`)

### Layout

```
┌──────────────────────────┬──────────────────────────┐
│                          │                          │
│   🔍 CODEBASE (DWYT)     │   ⚡ RTK                 │
│                          │                          │
│   Status: ● Conectado    │   Status: ● Ativo        │
│   Projetos indexados: 5  │   Tokens salvos: 30.9M   │
│   [Indexar repo]         │   Economia: 61.0%        │
│   [Ver grafo → :9749]    │   [rtk gain]             │
│                          │                          │
├──────────────────────────┼──────────────────────────┤
│                          │                          │
│   🔄 HEADROOM            │   🧠 MEMSTACK           │
│                          │                          │
│   Status: 🟢 Rodando     │   Status: ● Disponível   │
│   Porta: 8787            │   Sessões: 12            │
│   Tokens salvos: 4.2M    │   Projetos: 3            │
│   [Iniciar] [Parar]      │   [Buscar...]            │
│   [curl :8787/stats]     │   [Salvar sessão]        │
│                          │                          │
└──────────────────────────┴──────────────────────────┘
```

### Quadrantes — o que cada um mostra

| Quadrante | Dados | Fonte | Refresh |
|---|---|---|---|
| **Codebase** | Status MCP, projetos indexados, lista de projetos, botão indexar | `codebase-memory-mcp` MCP (stdio), `list_projects` | 5s poll |
| **RTK** | Tokens salvos, % economia, último comando, top comandos | `rtk gain --json` (se disponível) ou parse de `rtk gain` | 10s poll |
| **Headroom** | Status proxy (🟢/🔴), porta, tokens salvos na sessão | `curl http://localhost:8787/stats` | 3s poll |
| **MemStack** | Status, nº de sessões, projetos com memória | `memstack stats --json` | 10s poll |

### API REST

```
GET  /api/status          # status agregado das 4 ferramentas (JSON)
POST /api/codebase/index  # dispara index_repository (body: {path: string})
POST /api/headroom/start  # inicia headroom proxy
POST /api/headroom/stop   # para headroom proxy
POST /api/memstack/search # busca no MemStack (body: {query: string})
GET  /api/rtk/gain        # retorna tokens economizados
GET  /api/metrics         # métricas agregadas (tokens, tempo, projetos)
```

### Eventos SSE

```
GET /api/events           # stream de eventos em tempo real
    event: headroom-status   data: {"running":true,"port":8787}
    event: rtk-saved         data: {"tokens":30900000,"pct":61.0}
    event: cbmcp-indexed     data: {"project":"foo","nodes":82}
    event: memstack-update   data: {"sessions":12}
```

---

## `state.json` — Estado persistente

Arquivo `~/.dwyt/state.json` mantido pelo binário Go:

```json
{
  "version": "3.0.0",
  "installed_at": "2026-05-01T18:00:00Z",
  "tools": {
    "cbmcp": {"installed": true, "version": "0.5.7", "ui_port": 9749},
    "rtk": {"installed": true, "version": "latest"},
    "headroom": {"installed": true, "venv": "~/.dwyt/headroom-venv", "port": 8787},
    "memstack": {"installed": true, "dir": "~/.dwyt/memstack"}
  },
  "clients": ["claude", "codex", "opencode"],
  "integrated_projects": [
    {"path": "/home/user/projects/foo", "indexed_at": "2026-05-01T18:05:00Z", "nodes": 82}
  ],
  "metrics": {
    "rtk_tokens_saved": 30900000,
    "headroom_tokens_saved": 4200000
  }
}
```

---

## Wrappers — binários de conveniência

O binário Go `dwyt` substitui os wrappers shell atuais:

```
~/.dwyt/bin/dwyt           → binário Go principal
~/.dwyt/bin/dwyt-codex     → symlink para dwyt (dispatch via argv[0])
~/.dwyt/bin/dwyt-opencode  → symlink para dwyt (dispatch via argv[0])
~/.dwyt/bin/dwyt-ui        → symlink para dwyt (dispatch via argv[0])
```

O binário detecta `argv[0]` e faz dispatch:
- Se chamado como `dwyt-codex` → executa `dwyt codex`
- Se chamado como `dwyt-opencode` → executa `dwyt opencode`
- Se chamado como `dwyt-ui` → executa `dwyt ui`

---

## Fases de implementação

### Fase 1 — Core CLI (substituição 1:1 do `dwyt.sh`)

- [ ] Estrutura do projeto Go (cobra + viper)
- [ ] `internal/detect` — OS, shell, arch
- [ ] `internal/deps` — verificação + instalação de curl, git, python3, node
- [ ] `internal/install/cbmcp` — download binário, wrapper UI, PATH
- [ ] `internal/install/rtk` — download binário, `rtk init -g`
- [ ] `internal/install/headroom` — Python venv, pip install, wrapper, patch Codex WS
- [ ] `internal/install/memstack` — git clone, dependências Python
- [ ] `internal/integrate` — gerar .mcp.json, opencode.json, AGENTS.md, CLAUDE.md, .cursor/rules/, .kiro/steering/, .github/copilot-instructions.md
- [ ] `internal/env` — env.sh, shell rc injection
- [ ] `internal/state` — ler/escrever state.json
- [ ] `cmd/root` — cobra root + flags
- [ ] TUI interativa com bubbletea (checklist tools, checklist clients, file picker)
- [ ] Modos: install, repo, reinstall, uninstall

### Fase 2 — Dashboard web

- [ ] `internal/status` — polling de status das 4 ferramentas
- [ ] `cmd/dashboard` — servidor HTTP + SSE
- [ ] API REST: `/api/status`, `/api/metrics`
- [ ] SSE: `/api/events`
- [ ] Frontend: HTML+JS vanilla, grid 2x2, dark theme
- [ ] Embed com `embed.FS`

### Fase 3 — Testes e distribuição

- [ ] Testes de integração: container Docker limpo, comparar com `dwyt.sh`
- [ ] CI/CD: GitHub Actions, `goreleaser`, brew tap
- [ ] Documentação: README atualizado, GIF/screenshot do dashboard

### Fase 4 — Pós-lançamento

- [ ] `dwyt daemon` — modo serviço (systemd/launchd) que mantém headroom + UI rodando
- [ ] Notificações desktop (erro no proxy, projeto indexado)
- [ ] Suporte a plugins (extensões comando customizadas)
- [ ] `dwyt update` — auto-update do binário Go

---

## Métricas no dashboard — fontes de dados

| Métrica | Como obter | Ferramenta |
|---|---|---|
| Tokens salvos (total) | `rtk gain --json` | RTK |
| Tokens salvos (sessão) | `curl localhost:8787/stats` | Headroom |
| Projetos indexados | MCP `list_projects` | codebase-memory-mcp |
| Nós/arestas no grafo | MCP `index_status` | codebase-memory-mcp |
| Sessões MemStack | `memstack stats` | MemStack |
| Status proxy | `curl localhost:8787/health` | Headroom |
| Status MCP | stdin ping para o binary | codebase-memory-mcp |

---

## Princípios de design

1. **CLI-first**: O terminal é a interface principal. O dashboard é opcional e auxiliar.
2. **Zero regressão**: Todo comportamento do `dwyt.sh` é preservado.
3. **Binário único**: `go build` produz um binário estático que contém o dashboard embutido.
4. **Estado local**: Tudo em `~/.dwyt/`. Sem dependência de cloud, sem telemetria.
5. **Idempotente**: Rodar `dwyt --reinstall` múltiplas vezes produz o mesmo resultado.
6. **Cross-platform**: Linux (Debian/Fedora), macOS (Intel + Apple Silicon), Windows (via WSL/Git Bash).
7. **Fallback silencioso**: Se uma ferramenta não está instalada ou não responde, o dashboard mostra "indisponível" em vez de quebrar.

---

## Estrutura final de diretórios

Regras
use o ginger
deixe os diretorios organizados dentro de dwyt-orchestrator
Gere os 3 binarios
Linux MacOS e Windows

```
dwyt-orchestrator/
├── cmd/
│   └── dwyt/
│       └── main.go
├── internal/
│   ├── detect/
│   ├── deps/
│   ├── install/
│   ├── integrate/
│   ├── env/
│   ├── status/
│   ├── state/
│   └── server/
├── web/
│   └── dashboard/
│       ├── index.html
│       ├── app.js
│       └── style.css
├── dwyt.sh              # mantido durante transição, removido na v3.1
├── Plan/
│   └── plan.md
├── AGENTS.md
├── opencode.json
├── go.mod
├── go.sum
├── .goreleaser.yaml
├── README.md
└── LICENSE
```
