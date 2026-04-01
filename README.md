# DWYT — Don't Waste Your Tokens

Um script que instala e integra quatro ferramentas open source para reduzir drasticamente o consumo de tokens em qualquer LLM (Claude Code, Cursor, Aider, Copilot, Cline, etc).

```bash
chmod +x dwyt.sh && ./dwyt.sh
```

---

## O problema

Com múltiplas sessões abertas e projetos paralelos, o consumo de tokens não escala linearmente — ele explode. O limite semanal do Claude Max aparece na quinta de manhã com uma mensagem que ninguém quer ver.

## As ferramentas

| Ferramenta | O que faz | Redução |
|---|---|---|
| **codebase-memory-mcp** | Grafo do código com UI visual — respostas estruturais sem grep arquivo por arquivo | ~99% de tokens por consulta |
| **RTK** | Comprime output de terminal antes de entrar no contexto | 60–98% por comando |
| **Headroom** | Proxy que comprime chamadas à API em trânsito | ~34% por requisição |
| **MemStack** | Memória persistente entre sessões — elimina reconstrução de contexto | variável |

## O que o script faz

1. Apresenta um **checklist** para escolher quais ferramentas instalar
2. Abre um **menu de navegação** para selecionar o projeto a integrar
3. Instala **tudo em `~/.dwyt/`** — nenhum arquivo fora dessa pasta
4. Configura `.mcp.json`, `.claude/settings.json`, `CLAUDE.md` e hooks no projeto
5. Indexa o projeto com codebase-memory-mcp
6. **Sobe a UI visual do grafo automaticamente** em `http://localhost:9749`
7. Mostra o resumo completo de uso

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
# 1. Inicia o proxy de compressão + Claude Code
headroom wrap claude      # ou: headroom wrap aider / headroom proxy --port 8787

# 2. No chat do LLM, indexe o projeto
"Index this project"

# 3. Trabalhe normalmente — RTK e MemStack são automáticos

# 4. UI visual do grafo já está rodando (subiu com o script)
#    Acesse: http://localhost:9749
#    Gerenciar: dwyt-ui / dwyt-ui stop

# 5. Veja os tokens economizados
rtk gain

# 6. Ao final da sessão
headroom learn --apply        # salva aprendizados no CLAUDE.md
curl localhost:8787/stats     # relatório de compressão
```

## Comandos de referência

```bash
# codebase-memory-mcp (via chat no LLM)
"Index this project"               # indexa o grafo do código
"Quem chama a função X?"           # rastreia chamadores
"O que a função X chama?"          # rastreia dependências
"Tem código morto no projeto?"     # funções sem callers
"Quais são as rotas REST?"         # lista endpoints
"Mostre chamadas HTTP entre serviços"

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
headroom wrap aider                # proxy + Aider
curl http://localhost:8787/stats   # estatísticas em tempo real
headroom learn --apply             # salva aprendizados no CLAUDE.md

# MemStack (via chat no LLM)
/memstack-search <query>           # busca nas memórias persistidas
/memstack-headroom                 # status do proxy Headroom
```

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
├── CLAUDE.md                      # instruções para qualquer LLM
└── .claude/
    ├── settings.json              # hooks RTK + ANTHROPIC_BASE_URL
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
