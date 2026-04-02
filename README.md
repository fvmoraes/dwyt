# DWYT — Don't Waste Your Tokens

Um script que instala e integra quatro ferramentas open source para reduzir drasticamente o consumo de tokens em clientes como Claude Code, Codex, Copilot, Kiro e Cursor.

```bash
chmod +x dwyt.sh && ./dwyt.sh
```

---

## Regra geral

Todas as integrações do DWYT são opcionais:

- Se o Headroom estiver ativo via wrapper, use ele; se não estiver, não use
- Se o codebase-memory-mcp estiver conectado e respondendo no cliente, use ele; se não estiver, faça fallback para busca manual
- Se o RTK estiver instalado e funcionando, use ele; se não estiver, rode os comandos normalmente
- Se o MemStack estiver disponível no cliente atual, use ele; se não estiver, siga sem memória persistente

## O problema

Com múltiplas sessões abertas e projetos paralelos, o consumo de tokens não escala linearmente — ele explode. O limite semanal do Claude Max aparece na quinta de manhã com uma mensagem que ninguém quer ver.

## As ferramentas

| Ferramenta | O que faz | Redução |
|---|---|---|
| **[codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)** | Grafo do código com UI visual — respostas estruturais sem grep arquivo por arquivo | ~99% de tokens por consulta |
| **[RTK](https://github.com/rtk-ai/rtk)** | Comprime output de terminal antes de entrar no contexto | 60–98% por comando |
| **[Headroom](https://github.com/chopratejas/headroom)** | Proxy que comprime chamadas à API em trânsito | ~34% por requisição |
| **[MemStack](https://github.com/cwinvestments/memstack)** | Memória persistente entre sessões — elimina reconstrução de contexto | variável |

## O que o script faz

1. Apresenta um **checklist** para escolher quais ferramentas instalar
2. Apresenta um **checklist** para escolher quais clientes LLM integrar
3. Abre um **menu de navegação** para selecionar o projeto a integrar
4. Instala **tudo em `~/.dwyt/`** — nenhum arquivo fora dessa pasta
5. Configura `.mcp.json` e os arquivos corretos para cada cliente (`AGENTS.md`, `.codex/`, `CLAUDE.md`, `.github/copilot-instructions.md`, `.cursor/rules/`, `.kiro/steering/`)
6. Adiciona ao `.gitignore` os diretórios gerados do tipo `.ferramenta/` e arquivos locais como `AGENTS.md`
7. Indexa o projeto com codebase-memory-mcp
8. **Sobe a UI visual do grafo automaticamente** em `http://localhost:9749`
9. Mostra o resumo completo de uso

## Modos de execução

```bash
./dwyt.sh               # instalação interativa (checklist + menu)
./dwyt.sh --reinstall   # apaga ~/.dwyt e reinstala tudo do zero
./dwyt.sh --uninstall   # remove todas as ferramentas instaladas
./dwyt.sh --help        # mostra os modos disponíveis
```

## Requisitos

- `curl`, `git`, `python3`, `dialog`
- Linux (Ubuntu/Debian/Fedora) ou macOS
- O restante (Node.js, python3-venv, etc.) é instalado automaticamente

## Fluxo de trabalho

```bash
# 1. Se quiser usar Headroom em um cliente compatível, suba o proxy e abra com wrapper
headroom proxy --port 8787
headroom wrap claude      # usa Headroom no Claude Code
headroom wrap codex       # usa Headroom no Codex
headroom wrap cursor      # usa Headroom no Cursor

# Se não abrir com wrapper, siga normalmente sem Headroom

# 2. No chat do LLM, se o MCP estiver conectado, valide e use os comandos principais
/mcp                              # valida se o servidor MCP está conectado no cliente
"Index this project"              # dispara a tool index_repository
"Quem chama a função X?"          # usa trace_call_path para rastrear chamadores

# Se o MCP não estiver disponível no cliente, faça busca manual normalmente

# 3. Trabalhe normalmente
#    Claude Code pode usar hooks automáticos de RTK e MemStack quando disponíveis
#    Codex, Copilot, Kiro e Cursor usam instruções de projeto e MCP quando disponíveis

# 4. UI visual do grafo já está rodando (subiu com o script)
#    Acesse: http://localhost:9749
#    Gerenciar: dwyt-ui / dwyt-ui stop

# 5. Veja os tokens economizados
rtk gain

# 6. Ao final da sessão
headroom learn --apply        # salva aprendizados no CLAUDE.md (Claude Code)
curl localhost:8787/stats     # relatório de compressão
```

## Comandos de referência

### codebase-memory-mcp

| Tool | Purpose |
|---|---|
| `index_repository` | Indexa um projeto |
| `index_status` | Verifica o progresso da indexação |
| `detect_changes` | Encontra o que mudou desde a última indexação |
| `search_graph` | Busca nós por padrão |
| `search_code` | Faz busca textual no código-fonte |
| `query_graph` | Executa consultas em Cypher |
| `trace_call_path` | Percorre a cadeia de chamadas |
| `get_code_snippet` | Lê o código-fonte de uma função |
| `get_graph_schema` | Mostra o catálogo de tipos de nós e relações |
| `get_architecture` | Gera um resumo de alto nível da arquitetura |
| `list_projects` | Lista projetos indexados |
| `delete_project` | Remove um projeto |
| `manage_adr` | Gerencia registros de decisão arquitetural |
| `ingest_traces` | Importa traces de runtime |

```bash
# Validação rápida no cliente
"/mcp"                            # valida se o servidor MCP está conectado no cliente
"Index this project"               # dispara a tool index_repository

# UI visual do grafo
dwyt-ui                            # inicia/reinicia na porta 9749
dwyt-ui stop                       # para a UI

# RTK
rtk gain                           # tokens economizados total
rtk discover                       # oportunidades ainda não capturadas
rtk git status                     # uso manual com qualquer comando

# Headroom
headroom proxy --port 8787         # só o proxy
headroom wrap claude               # proxy + Claude Code
headroom wrap codex                # proxy + Codex
headroom wrap cursor               # proxy + Cursor
headroom wrap aider                # proxy + Aider
curl http://localhost:8787/stats   # estatísticas em tempo real
headroom learn --apply             # salva aprendizados no CLAUDE.md

# MemStack (via chat no LLM)
/memstack-search <query>           # busca nas memórias persistidas
/memstack-headroom                 # status do proxy Headroom
memstack help                     # lista os comandos disponíveis
memstack start                    # inicia o proxy Headroom do MemStack
memstack stop                     # para o proxy Headroom do MemStack
memstack stats                    # estatísticas do banco MemStack
memstack search "<query>"         # busca direta no banco
memstack get-sessions <project>   # últimas sessões de um projeto
memstack get-insights <project>   # insights salvos do projeto
memstack get-context <project>    # contexto salvo do projeto
memstack get-plan <project>       # tarefas/planejamento do projeto
memstack export-md <project>      # exporta a memória do projeto em markdown
```

## Clientes suportados

| Cliente | Arquivos gerados | Observações |
|---|---|---|
| **Claude Code** | `CLAUDE.md`, `.claude/settings.json`, `.claude/hooks/`, `.claude/rules/` | integração mais profunda hoje; `headroom wrap claude` é opcional; `.claude/` entra no ignore |
| **Codex** | `AGENTS.md`, `.codex/`, `.mcp.json` | `AGENTS.md` é o arquivo que o Codex lê; `headroom wrap codex` é opcional; `.codex/` e `AGENTS.md` ficam locais |
| **GitHub Copilot** | `.github/copilot-instructions.md`, `AGENTS.md`, `.mcp.json` | usa instruções de repositório + contexto compartilhado com fallback quando integrações não estiverem disponíveis |
| **Kiro** | `.kiro/steering/dwyt.md`, `AGENTS.md`, `.mcp.json` | sem `wrap` oficial do Headroom; `.kiro/` entra no ignore |
| **Cursor** | `.cursor/rules/dwyt.mdc`, `AGENTS.md`, `.mcp.json` | `headroom wrap cursor` é opcional; `.cursor/` entra no ignore |

## Localização dos dados — tudo em `~/.dwyt/`

```
~/.dwyt/
├── bin/                           # binários (no PATH)
│   ├── codebase-memory-mcp
│   ├── codebase-memory-mcp-ui
│   ├── rtk
│   ├── headroom
│   └── dwyt-ui                    # gerenciador da UI
├── data/                          # banco SQLite do grafo
│   └── codebase-memory-mcp/
│       └── codebase-memory.db
├── headroom-venv/                 # Python virtualenv do Headroom
├── memstack/                      # MemStack clonado
├── env.sh                         # variáveis de ambiente (PATH, XDG_CACHE_HOME)
└── .ui.pid                        # PID da UI em execução

<projeto>/
├── .mcp.json                      # config do codebase-memory-mcp
├── AGENTS.md                      # instruções universais para Codex, Cursor e Kiro (local, ignorado pelo git)
├── CLAUDE.md                      # instruções específicas do Claude Code
├── .codex/
│   └── README.md                  # pasta auxiliar da integração do Codex (ignorada pelo git)
├── .github/
│   └── copilot-instructions.md    # instruções de repositório para GitHub Copilot
├── .cursor/
│   └── rules/
│       └── dwyt.mdc               # regra alwaysApply do Cursor (ignorada pelo git)
├── .kiro/
│   └── steering/
│       └── dwyt.md                # steering file do Kiro (ignorado pelo git)
└── .claude/
    ├── settings.json              # hooks/permissões locais do Claude Code (ignorado pelo git)
    ├── settings.local.json        # opcional/local (ignorado pelo git)
    ├── hooks/
    │   └── rtk-rewrite.sh         # reescreve comandos automaticamente
    ├── rules/                     # regras do MemStack
    ├── skills/                    # skills do MemStack (symlink → ~/.dwyt/memstack/skills)
    └── memory/                    # memórias persistentes entre sessões
```

## Repositórios

- [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)
- [RTK](https://github.com/rtk-ai/rtk)
- [Headroom](https://github.com/chopratejas/headroom)
- [MemStack](https://github.com/cwinvestments/memstack)
