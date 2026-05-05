# DWYT — Don't Waste Your Tokens

> O orquestrador invisível que reduz o consumo de tokens dos seus clientes de IA.

DWYT orquestra quatro ferramentas que reduzem drasticamente o consumo de tokens em clientes como Claude Code, Codex, Copilot, Kiro, Cursor e OpenCode — tudo controlado por uma UI web, sem comandos externos.

---

## Instalação em um comando

```bash
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
```

O script detecta sua plataforma, baixa o binário da release, configura o PATH e orienta os próximos passos.

---

## Como usar

```bash
cd ~/meu-projeto
dwyt .
```

A UI abre em `http://localhost:2737` com o projeto pré-carregado. **Nenhuma configuração por CLI — tudo é feito pela interface.**

### Comandos disponíveis

| Comando | O que faz |
|---------|-----------|
| `dwyt .` | Abre no diretório atual |
| `dwyt /path` | Abre em um diretório específico |
| `dwyt` | Abre no CWD |
| `dwyt stop` | Para todos os serviços |
| `dwyt status` | Status rápido no terminal |
| `dwyt version` | Versão atual |
| `dwyt reinstall` | Apaga `~/.dwyt` e reinstala |
| `dwyt uninstall` | Remove todas as ferramentas |

---

## Arquitetura

O DWYT é um binário único (~37MB) que carrega a UI React embutida. Tudo funciona sem dependências externas — a UI, API e serviços são servidos pelo mesmo processo.

```
dwyt .
  ├── Detecta o projeto
  ├── Carrega o vault Obsidian (~/.dwyt/projects/<id>/obsidian/)
  ├── ProcessManager sobe Codebase + Headroom em background
  ├── RTK ativo como CLI tool
  └── UI abre em http://localhost:2737
```

---

## As ferramentas

### Obsidian (Project Brain) — **obrigatório**

O cérebro do DWYT. Cada projeto ganha um **vault Obsidian** em `~/.dwyt/projects/<id>/obsidian/` com markdowns estruturados:

```
brain/
├── index.md         # índice do projeto
├── context.md       # resumo completo (rebuild automático)
├── decisions.md     # log de decisões de arquitetura
├── tasks.md         # tarefas ativas
├── knowledge/       # artigos de conhecimento
└── logs/            # sessões, erros, comandos executados
```

**Formato**: frontmatter YAML (`tags`, `date`, `type`) em cada arquivo. Compatível com busca nativa do Obsidian e plugins como Dataview.

**Botão "Open in Obsidian"** no card da UI abre o vault diretamente no app.

**IAs são instruídas** a consultar o vault antes de qualquer operação — eliminando reconstrução de contexto.

| API | Uso |
|-----|-----|
| `GET /api/brain/search?q=` | Buscar contexto antes de começar tarefa |
| `POST /api/brain/save` | Salvar decisão, erro, tarefa ou nota |

### Headroom — compressão de API automática

Proxy que comprime chamadas às APIs de IA em trânsito (~34% de redução). Usa o comando nativo do Headroom para configurar cada cliente de IA automaticamente:

```bash
# DWYT runs these automatically when Headroom starts:
headroom wrap claude      # Claude Code
headroom wrap codex       # Codex
headroom wrap cursor      # Cursor
headroom wrap copilot     # GitHub Copilot CLI
```

Ao iniciar o Headroom (pelo botão Start ou automaticamente com `dwyt .`), o DWYT executa `headroom wrap` para cada cliente de IA habilitado no Setup. Ao parar, executa `headroom unwrap` para remover a configuração.

As variáveis de ambiente também são exportadas automaticamente pelo `env.sh`:

```bash
export OPENAI_BASE_URL="http://127.0.0.1:8787/v1"
export ANTHROPIC_BASE_URL="http://127.0.0.1:8787"
```

| Botão | Ação |
|-------|------|
| **Open Stats** | Abre estatísticas de compressão em tempo real |
| **Start/Stop** | Inicia/para o proxy + wrap/unwrap automático dos clientes |

### RTK — compressão de terminal

CLI tool que comprime output de comandos shell em 60–98%. Basta prefixar comandos com `rtk`:

```bash
rtk git status
rtk git log --oneline
rtk cargo test
```

Métricas filtradas por projeto — o card mostra comandos executados e tokens economizados no diretório atual.

### Codebase — mapa estrutural do código (opcional)

Grafo de código que permite navegação estrutural sem grep arquivo por arquivo. **Indexação sob demanda** — o usuário clica "Index" quando quiser.

Gerenciado pelo **ProcessManager** interno com:
- Start/Stop com healthcheck (5 tentativas, backoff exponencial)
- Logs stdout/stderr capturados (`~/.dwyt/logs/codebase-*.log`)
- Porta dinâmica (se 9749 ocupada, tenta alternativas)
- Botão **View Logs** para diagnóstico real em caso de erro

---

## Dashboard

```
┌───────────────────────────────────────────────────────────────────┐
│  🤓 DWYT          [Auto Off 5s 10s] [↺ Refresh] [Logs] [← Setup] │
├───────────────────────────────────────────────────────────────────┤
│  🛡️ meu-projeto  DWYT is protecting this project  🧠 12 obsidian files │
│                                                                   │
│  ┌───────────────────────────────────────────────────────────┐    │
│  │  Sem DWYT        │  Com DWYT        │  Economia total     │    │
│  │  2.4M tokens     │  480K tokens     │  1.9M  ↓ 80%       │    │
│  │  seriam gastos   │  gastos          │                     │    │
│  │                  │                  │  Obsidian | RTK     │    │
│  │                  │                  │  Headroom | Codebase│    │
│  └───────────────────────────────────────────────────────────┘    │
│                                                                   │
│  ┌────────────────────────┐  ┌────────────────────────┐          │
│  │  CODEBASE         🟢   │  │  RTK               🟢 │          │
│  │  Code graph — …        │  │  Terminal output —  … │          │
│  │  ─────────────────────  │  │  ─────────────────────  │          │
│  │  UPTIME       2m 3s    │  │  COMANDOS          847 │          │
│  │  STATUS     Indexed    │  │  TOKENS SAVED     31M │          │
│  │  ▶ Start  ■ Stop       │  │  % SAVED          61% │          │
│  │  [/path] [Index]       │  │  ─────────────────────  │          │
│  │  Open Graph →          │  │  ████████████░░░░░░░░░  │          │
│  └────────────────────────┘  └────────────────────────┘          │
│  ┌────────────────────────┐  ┌────────────────────────┐          │
│  │  HEADROOM         🟢   │  │  OBSIDIAN          🟢 │          │
│  │  API call compression  │  │  Obsidian vault — …   │          │
│  │  ─────────────────────  │  │  ─────────────────────  │          │
│  │  REQUESTS         234  │  │  FILES             12 │          │
│  │  TOKENS SAVED     8M  │  │  UPTIME         1h 2m │          │
│  │  COMPRESSION      34%  │  │  ▶ Save  [Search...]  │          │
│  │  PORT             8787  │  │  Rebuild | Forget     │          │
│  │  ▶ Start  ■ Stop       │  │  🧠 Open in Obsidian  │          │
│  │  Open Stats →          │  └────────────────────────┘          │
│  └────────────────────────┘                                       │
└───────────────────────────────────────────────────────────────────┘
```

**Cada card** mostra nome da ferramenta, descrição do que faz e status real (🟢 online / 🟡 parado / 🔴 não instalado).

---

## Setup

Na primeira execução, a UI abre no Setup. **Obsidian é obrigatório** e já vem pré-selecionado. As demais ferramentas são opcionais.

```
┌─────────────────────────────────────────────────────────┐
│  🤓 DWYT                    [Instalar →] [Dashboard →]  │
├─────────────────────────────────────────────────────────┤
│  ▾ Ferramentas              4 de 4 selecionadas         │
│  ┌─────────────────────────────────────────────────┐    │
│  │ ● Obsidian (ON)  Obsidian vault — project       │    │
│  │ ● Codebase       Code graph — structural        │    │
│  │ ● Headroom       API call compression           │    │
│  │ ● RTK            Terminal output compression    │    │
│  └─────────────────────────────────────────────────┘    │
│                                                         │
│  ▾ IAs / Clientes           6 de 6 selecionados         │
│  ┌─────────────────────────────────────────────────┐    │
│  │ ● Claude Code   ● Codex   ● GitHub Copilot      │    │
│  │ ● Kiro          ● Cursor  ● OpenCode            │    │
│  └─────────────────────────────────────────────────┘    │
│                                                         │
│  ▾ Projeto                  /home/user/meu-projeto      │
│  ┌─────────────────────────────────────────────────┐    │
│  │ /home/user/meu-projeto          [Selecionar]    │    │
│  └─────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

Ao clicar em **Instalar →**, o DWYT baixa e configura Codebase, Headroom e RTK. Gera os arquivos de instrução para cada cliente de IA. Sobe os serviços. Abre o Dashboard.

---

## Onde os dados ficam

### Linux / macOS

```
~/.dwyt/
├── bin/                         # binários das ferramentas
├── data/                        # banco SQLite
├── headroom-venv/               # Python virtualenv do Headroom
├── logs/                        # stdout/stderr dos serviços
│   ├── codebase-stdout.log
│   ├── codebase-stderr.log
│   ├── headroom-stdout.log
│   └── headroom-stderr.log
├── projects/                    # per-project Obsidian vaults
│   └── <sha12>/
│       ├── obsidian/              # vault Obsidian (markdowns)
│       └── project.json         # metadata do projeto
├── env.sh                       # variáveis de ambiente (OPENAI_BASE_URL, etc.)
├── dwyt.db                      # SQLite (projetos, config)
└── state.json                   # estado runtime (PIDs, portas, erros)
```

### Windows

```
%APPDATA%\dwyt\                  # C:\Users\<user>\AppData\Roaming\dwyt\
├── bin\
├── data\
├── headroom-venv\
├── logs\
├── projects\                    # per-project Obsidian vaults
├── env.ps1
├── dwyt.db
└── state.json
```

---

## Arquivos gerados por projeto

O Setup cria estes arquivos no diretório do projeto (todos adicionados ao `.gitignore`):

```
<projeto>/
├── .mcp.json                      # config do codebase-memory-mcp
├── AGENTS.md                      # instruções para Codex, Kiro, Cursor, OpenCode
├── CLAUDE.md                      # instruções para Claude Code
├── opencode.json                  # config do OpenCode
├── .github/
│   └── copilot-instructions.md
├── .cursor/
│   └── rules/dwyt.mdc
└── .kiro/
    └── steering/dwyt.md
```

**Todos instruem as IAs** nesta ordem de prioridade:
1. **Obsidian FIRST** — consulte o vault antes de qualquer operação
2. **Headroom** — compressão automática via env vars
3. **RTK** — prefixe comandos shell com `rtk`
4. **Codebase MCP** — apenas para exploração estrutural

---

## Clientes suportados

| Cliente | Arquivos gerados |
|---|---|
| **Claude Code** | `CLAUDE.md`, `.claude/` |
| **Codex** | `AGENTS.md`, `.codex/`, `.mcp.json` |
| **GitHub Copilot** | `.github/copilot-instructions.md`, `AGENTS.md` |
| **Kiro** | `.kiro/steering/dwyt.md`, `AGENTS.md` |
| **Cursor** | `.cursor/rules/dwyt.mdc`, `AGENTS.md` |
| **OpenCode** | `opencode.json`, `AGENTS.md`, `.mcp.json` |

---

## URLs da UI

| URL | Descrição |
|---|---|
| `/#/` | Setup |
| `/#/dashboard` | Dashboard (todos os repositórios) |
| `/#/dashboard?project=/path/repo` | Dashboard com projeto específico |
| `/#/dashboard?reload=5` | Auto-reload de 5s |
| `/#/dashboard?logs=1` | Painel de logs aberto |

---

## Headroom — detalhes técnicos

O Headroom sobe automaticamente com `dwyt .` em background na porta 8787. O `env.sh` injetado no shell RC exporta:

```bash
export HEADROOM_PORT=8787
export OPENAI_BASE_URL="http://127.0.0.1:8787/v1"
export ANTHROPIC_BASE_URL="http://127.0.0.1:8787"
```

Ao iniciar, injeta blocos `<!-- dwyt:headroom-proxy -->` nos arquivos de config dos clientes. Ao parar (pelo botão Stop na UI), remove esses blocos. **Fallback automático**: se o Headroom cair, os clientes voltam a usar os endpoints padrão das APIs.

---

## Codebase — detalhes técnicos

Gerenciado pelo **ProcessManager** interno:
- **Start**: healthcheck HTTP com retry (5 tentativas, backoff exponencial, timeout 10s)
- **Stop**: `SIGTERM` → espera 5s → `SIGKILL`
- **Logs**: `~/.dwyt/logs/codebase-stdout.log` + `codebase-stderr.log`
- **Porta dinâmica**: se 9749 ocupada, tenta 9750, 9751, 9752
- **Botão "View Logs"** na UI mostra os logs reais para diagnóstico

**Indexação**: sob demanda, não automática. O usuário clica "Index" na UI. Progresso visível com polling.

---

## Requisitos

| Ferramenta | Necessário para |
|---|---|
| Obsidian | **Obrigatório** — engine principal de conhecimento |
| Python 3 | Instalação do Headroom |
| Node.js | Instalação do Codebase |
| curl ou wget | Download do instalador |
| Git | Instalação de dependências |

O binário `dwyt` em si não tem dependências — é um executável Go estático com a UI React embutida.

---

## Repositórios

- [DWYT](https://github.com/fvmoraes/dwyt)
- [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)
- [RTK](https://github.com/rtk-ai/rtk)
- [Headroom](https://github.com/chopratejas/headroom)
- [Obsidian](https://obsidian.md) — Project vault
