# DWYT — Don't Waste Your Tokens

> Um nerd determinado que te ajuda a não desperdiçar tokens.

DWYT instala e orquestra quatro ferramentas open source que reduzem drasticamente o consumo de tokens em clientes como Claude Code, Codex, Copilot, Kiro, Cursor e OpenCode.

---

## Instalação em um comando

```bash
curl -fsSL https://raw.githubusercontent.com/DeusData/dwyt/main/install.sh | bash
```

ou com wget:

```bash
wget -qO- https://raw.githubusercontent.com/DeusData/dwyt/main/install.sh | bash
```

O script detecta sua plataforma, baixa o binário correto, configura o PATH e orienta os próximos passos.

---

## Como usar

```bash
# Abrir no diretório atual — igual ao `code .` ou `kiro .`
cd ~/meu-projeto
dwyt .

# Ou passando o caminho diretamente
dwyt /caminho/do/projeto

# Sem argumento — usa o diretório atual
dwyt
```

O DWYT abre o dashboard em `http://localhost:2737` já com o projeto pré-carregado.

---

## O binário é self-contained

**Sim — o executável carrega tudo sozinho.** Não há arquivos externos necessários para rodar o DWYT.

A UI web (React) é compilada e **embutida dentro do binário Go** em tempo de build:

```go
//go:embed dashboard/dist
var reactFS embed.FS
```

Quando o daemon sobe, ele serve o HTML/JS/CSS diretamente da memória. O usuário recebe apenas o executável e tudo funciona: UI, API, serviços.

```
dwyt-linux-amd64   ← único arquivo, ~32MB, inclui a UI completa
```

As ferramentas externas (RTK, Headroom, etc.) são instaladas em `~/.dwyt/bin/` pelo Setup — mas o DWYT em si não precisa de nada além do executável.

---

## Instalação manual

Baixe o binário para sua plataforma:

### Linux

```bash
curl -fsSL https://raw.githubusercontent.com/DeusData/dwyt/main/dwyt-linux-amd64 -o dwyt
chmod +x dwyt
./dwyt .
```

### macOS (Apple Silicon)

```bash
curl -fsSL https://raw.githubusercontent.com/DeusData/dwyt/main/dwyt-darwin-arm64 -o dwyt
chmod +x dwyt
./dwyt .
```

### macOS (Intel)

```bash
curl -fsSL https://raw.githubusercontent.com/DeusData/dwyt/main/dwyt-darwin-amd64 -o dwyt
chmod +x dwyt
./dwyt .
```

### Windows (PowerShell)

```powershell
Invoke-WebRequest -Uri "https://raw.githubusercontent.com/DeusData/dwyt/main/dwyt-windows-amd64.exe" -OutFile "dwyt.exe"
.\dwyt.exe .
```

Na primeira execução, o DWYT configura o PATH automaticamente para que `dwyt` funcione de qualquer diretório.

---

## Fluxo de uso

### Primeira vez (instalação das ferramentas)

```
dwyt .
```

```
╔══════════════════════════════════════╗
║  DWYT — Don't Waste Your Tokens     ║
╚══════════════════════════════════════╝

  Projeto: /home/user/meu-projeto

  →  codebase-memory-mcp       não instalado (instale via UI)
  →  headroom                  não instalado (instale via UI)

  ✓ Dashboard → http://localhost:2737
  Parar: dwyt stop
```

O browser abre no Setup — selecione as ferramentas, os clientes de IA e clique em **Instalar →**.

### Uso diário

```bash
cd ~/qualquer-projeto
dwyt .
```

O browser abre direto no **Dashboard** com o projeto pré-carregado e as métricas do diretório atual.

---

## Comandos CLI

```bash
dwyt .              # abre no diretório atual
dwyt /path/repo     # abre em um diretório específico
dwyt                # abre no cwd (mesmo que dwyt .)
dwyt stop           # para todos os serviços
dwyt status         # status rápido no terminal
dwyt version        # versão atual
dwyt reinstall      # apaga ~/.dwyt e reinstala tudo
dwyt uninstall      # remove todas as ferramentas
```

---

## Dashboard

```
┌─────────────────────────────────────────────────────────────────────┐
│  🤓 DWYT          [Auto Off 5s 10s] [↺ Atualizar] [Logs] [← Setup] │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Sem DWYT          │  Com DWYT          │  Economia total   │    │
│  │  2.4M tokens       │  480K tokens       │  1.9M  ↓ 80%     │    │
│  │  seriam gastos     │  gastos            │  ████████████░░   │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  ┌──────────────────────────┐  ┌──────────────────────────┐        │
│  │  CODEBASE            🟢  │  │  RTK                 🟢  │        │
│  │  🟢 OK                   │  │  🟢 OK                   │        │
│  │  ─────────────────────   │  │  ─────────────────────   │        │
│  │  TOKENS ECONOMIZADOS  -- │  │  TOKENS ECONOMIZADOS 31M │        │
│  │  UPTIME           2m 3s  │  │  COMANDOS            847 │        │
│  │  REPOS  📁 meu-projeto   │  │  % ECONOMIA         61%  │        │
│  │  ─────────────────────   │  │  ESCOPO  📁 meu-projeto  │        │
│  │  ▶ Iniciar  ■ Parar      │  │  ─────────────────────   │        │
│  │  [/path/repo]  [Indexar] │  │  ████████████░░░░░░░░░   │        │
│  │  Abrir Grafo →           │  └──────────────────────────┘        │
│  └──────────────────────────┘                                       │
│  ┌──────────────────────────┐  ┌──────────────────────────┐        │
│  │  HEADROOM            🟢  │  │  MEMSTACK            🟢  │        │
│  │  🟢 OK                   │  │  🟢 OK                   │        │
│  │  ─────────────────────   │  │  ─────────────────────   │        │
│  │  TOKENS ECONOMIZADOS 8M  │  │  TOKENS ECONOMIZADOS var │        │
│  │  REQUISIÇÕES         234 │  │  ATIVO HÁ    instalado   │        │
│  │  COMPRESSÃO         34%  │  │  REPOS  📁 meu-projeto   │        │
│  │  UPTIME           1h 2m  │  │  ─────────────────────   │        │
│  │  PORTA             8787  │  │  ▶ Iniciar  ■ Parar      │        │
│  │  ─────────────────────   │  │  [Buscar memória...][Bsc] │        │
│  │  ▶ Iniciar  ■ Parar      │  └──────────────────────────┘        │
│  └──────────────────────────┘                                       │
└─────────────────────────────────────────────────────────────────────┘
```

### Status das ferramentas

| Estado | Cor | Significado |
|---|---|---|
| 🔴 Não instalado | Vermelho | Binário não existe — instale via Setup |
| 🟡 Parado | Amarelo | Instalado mas não está rodando |
| 🟢 OK | Verde | Instalado e funcionando |

### Totalizador

O banner no topo compara o consumo com e sem DWYT, calculado a partir das métricas reais do RTK e Headroom.

### RTK por projeto

O card do RTK mostra estatísticas **filtradas pelo diretório atual** (onde você rodou `dwyt .`). O escopo aparece no card como `📁 nome-do-projeto`.

### Auto-reload

Seletor no header: **Off / 5s / 10s**. O intervalo persiste na URL como `?reload=5`.

---

## Setup

```
┌─────────────────────────────────────────────────────────┐
│  🤓 DWYT                    [Instalar →] [Dashboard →]  │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ▾ Ferramentas              4 de 4 selecionadas         │
│  ┌─────────────────────────────────────────────────┐    │
│  │ ● Codebase   Grafo de código — exploração       │    │
│  │ ● MemStack   Memória persistente entre sessões  │    │
│  │ ● Headroom   Compressão de chamadas à API       │    │
│  │ ● RTK        Compressão de output de terminal   │    │
│  └─────────────────────────────────────────────────┘    │
│                                                         │
│  ▾ IAs / Clientes           6 de 6 selecionados         │
│  ┌─────────────────────────────────────────────────┐    │
│  │ ● Claude Code    CLAUDE.md + .claude/           │    │
│  │ ● Codex          AGENTS.md + .codex/            │    │
│  │ ● GitHub Copilot .github/copilot-instructions   │    │
│  │ ● Kiro           .kiro/steering/dwyt.md         │    │
│  │ ● Cursor         .cursor/rules/dwyt.mdc         │    │
│  │ ● OpenCode       opencode.json + AGENTS.md      │    │
│  └─────────────────────────────────────────────────┘    │
│                                                         │
│  ▾ Projeto                  /home/user/meu-projeto      │
│  ┌─────────────────────────────────────────────────┐    │
│  │ /home/user/meu-projeto          [Selecionar]    │    │
│  │ ← Subir  /home/user/meu-projeto                 │    │
│  │ 📁 src   📁 tests   📄 README.md                │    │
│  └─────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

---

## As ferramentas

| Ferramenta | O que faz | Economia típica |
|---|---|---|
| **[codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)** | Grafo do código — respostas estruturais sem grep arquivo por arquivo | ~99% por consulta |
| **[RTK](https://github.com/rtk-ai/rtk)** | Comprime output de terminal antes de entrar no contexto | 60–98% por comando |
| **[Headroom](https://github.com/chopratejas/headroom)** | Proxy que comprime chamadas à API em trânsito | ~34% por requisição |
| **[MemStack](https://github.com/cwinvestments/memstack)** | Memória persistente entre sessões — elimina reconstrução de contexto | variável |

Todas as ferramentas são controladas pela UI do dashboard. Não há comandos externos para gerenciá-las.

---

## Onde os dados ficam

### Linux / macOS

```
~/.dwyt/
├── bin/                    # binários das ferramentas + symlink dwyt
├── data/                   # banco SQLite do grafo
├── headroom-venv/          # Python virtualenv do Headroom
├── memstack/               # MemStack clonado
├── env.sh                  # variáveis de ambiente (source no shell RC)
├── config.json             # configuração salva pelo Setup
└── state.json              # estado das ferramentas
```

### Windows

```
%APPDATA%\dwyt\             # C:\Users\<user>\AppData\Roaming\dwyt\
├── bin\                    # binários + dwyt.exe
├── data\
├── headroom-venv\
├── memstack\
├── env.ps1                 # variáveis de ambiente (PowerShell)
├── config.json
└── state.json
```

---

## Arquivos gerados por projeto

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

Todos adicionados ao `.gitignore` automaticamente.

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

## URLs e query parameters

| URL | Descrição |
|---|---|
| `/#/` | Setup |
| `/#/dashboard` | Dashboard |
| `/#/dashboard?project=/path/repo` | Dashboard com projeto pré-carregado |
| `/#/dashboard?reload=5` | Auto-reload de 5s |
| `/#/dashboard?logs=1` | Painel de logs aberto |

---

## Requisitos

| Plataforma | Requisitos |
|---|---|
| Linux | `curl` ou `wget` |
| macOS | `curl` (já incluso) |
| Windows | PowerShell 5+ |

Python 3 e Node.js são necessários apenas para instalar as ferramentas via Setup. O binário `dwyt` em si não tem dependências.

---

## Repositórios das ferramentas

- [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)
- [RTK](https://github.com/rtk-ai/rtk)
- [Headroom](https://github.com/chopratejas/headroom)
- [MemStack](https://github.com/cwinvestments/memstack)
