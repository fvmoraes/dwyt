# DWYT — Don't Waste Your Tokens

> **Mascote:** um nerd determinado que te ajuda a não desperdiçar tokens.

DWYT instala e orquestra quatro ferramentas open source que reduzem drasticamente o consumo de tokens em clientes como Claude Code, Codex, Copilot, Kiro, Cursor e OpenCode.

---

## Como funciona em 30 segundos

```
cd ~/meu-projeto
dwyt
```

Isso é tudo. O DWYT:

1. Detecta o diretório atual
2. Sobe os serviços (codebase-memory-mcp, headroom)
3. Abre o dashboard em `http://localhost:2737`
4. Pré-carrega o projeto no contexto

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

## Instalação

Baixe o binário para sua plataforma e execute:

### Linux

```bash
chmod +x dwyt-linux-amd64
./dwyt-linux-amd64
```

O DWYT cria automaticamente um symlink em `~/.local/bin/dwyt`.
Após a primeira execução, use apenas `dwyt` de qualquer diretório.

### macOS

```bash
chmod +x dwyt-darwin-arm64   # Apple Silicon
# ou
chmod +x dwyt-darwin-amd64   # Intel

./dwyt-darwin-arm64
```

### Windows

```powershell
.\dwyt-windows-amd64.exe
```

O DWYT adiciona `%APPDATA%\dwyt\bin` ao PATH do usuário via registro.
Abra um novo terminal e use `dwyt` normalmente.

> **Onde os dados ficam no Windows?**
> `%APPDATA%\dwyt\` → `C:\Users\<usuario>\AppData\Roaming\dwyt\`
> Este é o local padrão para dados de aplicativos por usuário no Windows.

---

## Fluxo de uso

### Primeira vez (instalação)

```
dwyt
```

```
╔══════════════════════════════════════╗
║  DWYT — Don't Waste Your Tokens     ║
╚══════════════════════════════════════╝

  Projeto: /home/user/meu-projeto

  →  codebase-memory-mcp       não instalado (use a UI para instalar)
  →  headroom                  não instalado (use a UI para instalar)

  ✓ Dashboard → http://localhost:2737
  Parar: dwyt stop
```

O browser abre automaticamente no Setup:

```
┌─────────────────────────────────────────────────────────┐
│  🤓 DWYT                              [Instalar →] [Dashboard →] │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ▾ Ferramentas                  4 de 4 selecionadas     │
│  ┌─────────────────────────────────────────────────┐    │
│  │ ● Codebase   Grafo de código — exploração       │    │
│  │ ● MemStack   Memória persistente entre sessões  │    │
│  │ ● Headroom   Compressão de chamadas à API       │    │
│  │ ● RTK        Compressão de output de terminal   │    │
│  └─────────────────────────────────────────────────┘    │
│                                                         │
│  ▾ IAs / Clientes               6 de 6 selecionados     │
│  ┌─────────────────────────────────────────────────┐    │
│  │ ● Claude Code    CLAUDE.md + .claude/           │    │
│  │ ● Codex          AGENTS.md + .codex/            │    │
│  │ ● GitHub Copilot .github/copilot-instructions   │    │
│  │ ● Kiro           .kiro/steering/dwyt.md         │    │
│  │ ● Cursor         .cursor/rules/dwyt.mdc         │    │
│  │ ● OpenCode       opencode.json + AGENTS.md      │    │
│  └─────────────────────────────────────────────────┘    │
│                                                         │
│  ▾ Projeto                      /home/user/meu-projeto  │
│  ┌─────────────────────────────────────────────────┐    │
│  │ /home/user/meu-projeto          [Selecionar]    │    │
│  │ ← Subir  /home/user/meu-projeto                 │    │
│  │ 📁 src   📁 tests   📄 README.md                │    │
│  └─────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

Clique em **Instalar →** e acompanhe o progresso:

```
┌─────────────────────────────────────────────────────────┐
│  Instalando...                                          │
│  Ferramentas sendo instaladas em background. Aguarde.   │
│                                                         │
│  🔄  cbmcp        installing                            │
│  ⏳  rtk          pending                               │
│  ⏳  headroom     pending                               │
│  ⏳  memstack     pending                               │
└─────────────────────────────────────────────────────────┘
```

---

### Uso diário (ferramentas já instaladas)

```bash
cd ~/qualquer-projeto
dwyt
```

O browser abre direto no **Dashboard** com o projeto pré-carregado:

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
│  │  ─────────────────────   │  │  ATIVO HÁ    instalado   │        │
│  │  [/home/user/proj][Idx]  │  │  REPOS            global │        │
│  │  Abrir Grafo →           │  │  ─────────────────────   │        │
│  └──────────────────────────┘  │  ████████████░░░░░░░░░   │        │
│                                └──────────────────────────┘        │
│  ┌──────────────────────────┐  ┌──────────────────────────┐        │
│  │  HEADROOM            🟢  │  │  MEMSTACK            🟢  │        │
│  │  🟢 OK                   │  │  🟢 OK                   │        │
│  │  ─────────────────────   │  │  ─────────────────────   │        │
│  │  TOKENS ECONOMIZADOS 8M  │  │  TOKENS ECONOMIZADOS var │        │
│  │  REQUISIÇÕES         234 │  │  ATIVO HÁ    instalado   │        │
│  │  COMPRESSÃO         34%  │  │  REPOS  📁 meu-projeto   │        │
│  │  UPTIME           1h 2m  │  │  ─────────────────────   │        │
│  │  PORTA             8787  │  │  [Buscar memória...][Bsc] │        │
│  │  ─────────────────────   │  └──────────────────────────┘        │
│  │  [  Iniciar  ][  Parar ] │                                       │
│  └──────────────────────────┘                                       │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Comandos CLI

```bash
dwyt                # inicia tudo e abre o dashboard
dwyt stop           # para todos os serviços
dwyt status         # status rápido no terminal
dwyt version        # versão atual
dwyt reinstall      # apaga ~/.dwyt e reinstala tudo
dwyt uninstall      # remove todas as ferramentas
```

### Launchers com Headroom

```bash
dwyt-opencode       # OpenCode com proxy Headroom ativo
dwyt-codex          # Codex com proxy Headroom ativo
dwyt-ui             # inicia/para a UI do grafo (porta 9749)
dwyt-ui stop
```

---

## As ferramentas

| Ferramenta | O que faz | Economia típica |
|---|---|---|
| **[codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)** | Grafo do código — respostas estruturais sem grep arquivo por arquivo | ~99% por consulta |
| **[RTK](https://github.com/rtk-ai/rtk)** | Comprime output de terminal antes de entrar no contexto | 60–98% por comando |
| **[Headroom](https://github.com/chopratejas/headroom)** | Proxy que comprime chamadas à API em trânsito | ~34% por requisição |
| **[MemStack](https://github.com/cwinvestments/memstack)** | Memória persistente entre sessões — elimina reconstrução de contexto | variável |

---

## Onde os dados ficam

### Linux / macOS

```
~/.dwyt/
├── bin/                    # binários (no PATH)
│   ├── codebase-memory-mcp
│   ├── rtk
│   ├── headroom
│   ├── memstack
│   ├── dwyt                # symlink para o binário principal
│   ├── dwyt-codex          # launcher Codex + Headroom
│   ├── dwyt-opencode       # launcher OpenCode + Headroom
│   └── dwyt-ui             # gerenciador da UI do grafo
├── data/                   # banco SQLite do grafo
├── headroom-venv/          # Python virtualenv do Headroom
├── memstack/               # MemStack clonado
├── env.sh                  # variáveis de ambiente
├── config.json             # configuração salva pelo Setup
└── state.json              # estado das ferramentas
```

### Windows

```
%APPDATA%\dwyt\             # C:\Users\<user>\AppData\Roaming\dwyt\
├── bin\
│   ├── codebase-memory-mcp.exe
│   ├── rtk.exe
│   ├── headroom.bat
│   ├── memstack.bat
│   ├── dwyt.exe
│   ├── dwyt-codex.bat
│   ├── dwyt-opencode.bat
│   └── dwyt-ui.bat
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
│   └── copilot-instructions.md   # instruções para GitHub Copilot
├── .cursor/
│   └── rules/dwyt.mdc            # regra alwaysApply do Cursor
└── .kiro/
    └── steering/dwyt.md          # steering file do Kiro
```

Todos esses arquivos são adicionados ao `.gitignore` automaticamente.

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

## Dashboard — Status das ferramentas

Cada card mostra um dos 3 estados:

| Estado | Cor | Significado |
|---|---|---|
| 🔴 Não instalado | Vermelho | Binário não existe em `~/.dwyt/bin/` |
| 🟡 Parado | Amarelo | Instalado mas não está rodando |
| 🟢 OK | Verde | Instalado e funcionando |

Os botões **▶ Iniciar** e **■ Parar** em cada card são sutis (fundo transparente com borda colorida) para não poluir a interface.

---

## Dashboard — RTK por projeto

O card do RTK mostra estatísticas **filtradas pelo diretório atual** (onde você rodou `dwyt`), não globais. O escopo aparece no card:

```
ESCOPO    📁 meu-projeto
```

Se não houver dados para o projeto específico, cai automaticamente para as estatísticas globais.

O banner no topo do dashboard compara o consumo com e sem DWYT:

```
┌──────────────────┬──────────────────┬──────────────────────┐
│  Sem DWYT        │  Com DWYT        │  Economia total      │
│  2.4M tokens     │  480K tokens     │  1.9M  ↓ 80%        │
│  seriam gastos   │  gastos          │  ████████████░░      │
└──────────────────┴──────────────────┴──────────────────────┘
```

- **Sem DWYT**: estimativa calculada a partir das métricas de economia do RTK e Headroom
- **Com DWYT**: tokens efetivamente gastos
- **Economia**: soma de todos os tokens economizados por todas as ferramentas

---

## Auto-reload

O dashboard tem um seletor de atualização automática no header:

```
[Auto  Off  5s  10s]
```

O intervalo selecionado é salvo na URL como `?reload=5`, então persiste ao navegar entre telas.

---

## URLs e query parameters

| URL | Descrição |
|---|---|
| `/#/` | Setup (sempre mostra primeiro) |
| `/#/dashboard` | Dashboard |
| `/#/dashboard?project=/path/repo` | Dashboard com projeto pré-carregado |
| `/#/dashboard?reload=5` | Dashboard com auto-reload de 5s |
| `/#/dashboard?logs=1` | Dashboard com painel de logs aberto |
| `/#/setup?project=/path/repo` | Setup com projeto pré-preenchido |

---

## Requisitos

| Plataforma | Requisitos |
|---|---|
| Linux | `curl`, `git`, Python 3, Node.js |
| macOS | `curl`, `git`, Python 3, Node.js |
| Windows | Git Bash ou PowerShell, Python 3, Node.js |

Node.js e Python são necessários apenas para instalar as ferramentas via Setup.
O binário `dwyt` em si não tem dependências.

---

## Repositórios das ferramentas

- [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)
- [RTK](https://github.com/rtk-ai/rtk)
- [Headroom](https://github.com/chopratejas/headroom)
- [MemStack](https://github.com/cwinvestments/memstack)
