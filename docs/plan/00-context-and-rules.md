# 00 — Contexto do Produto e Regras Não Negociáveis

## Objetivo

O DWYT é um orquestrador local que roda como um binário Go único, abre uma UI React em:

```
http://localhost:2737
```

Mantém seu estado em `~/.dwyt/` e integra quatro frentes principais:

| Frente | Papel |
|--------|-------|
| **Obsidian** | Memória obrigatória por projeto — vault persistente, busca, save, resumo, abertura |
| **RTK** | CLI para compressão de saídas de terminal e economia de tokens |
| **Headroom** | Proxy local de compressão de chamadas de API (porta `8787`) |
| **Codebase** | Mapa estrutural do código e exploração do repositório (porta `9749`) |

Clientes de IA suportados: Claude Code, Codex, GitHub Copilot, Kiro, Cursor, OpenCode.

---

## Arquivos Principais

```
core/internal/server/server.go
core/web/src/pages/Dashboard.tsx
core/web/src/pages/SetupWizard.tsx
core/internal/integrate/integrate.go
core/internal/mcpregistry/registry.go
core/internal/install/install.go
core/internal/brain/brain.go
core/internal/status/status.go
core/test-e2e.sh
```

---

## Regras Não Negociáveis

### R1 — Tudo em `~/.dwyt/`

Ferramentas, binários, logs, estado, cache, banco SQLite, registry MCP e vaults por projeto ficam em:

```
~/.dwyt/
├── bin/
│   ├── codebase-memory-mcp
│   ├── rtk
│   ├── headroom
│   ├── dwyt-obsidian-mcp
│   └── dwyt
├── codebase/
├── headroom-venv/
├── logs/
├── powers/
│   └── dwyt-power/          # Kiro Power local (arquivo 12)
│       ├── POWER.md
│       ├── mcp.json
│       └── steering/
├── projects/
│   └── <sha12>/
│       ├── obsidian/
│       │   ├── index.md
│       │   ├── context.md
│       │   ├── decisions.md
│       │   ├── tasks.md
│       │   ├── knowledge/
│       │   └── logs/
│       ├── project.json
│       └── headroom-proxy.json
├── config/
│   └── mcp-registry.json
├── dwyt.db
├── dwyt.log
├── env.sh
└── state.json
```

### R2 — Vaults do Obsidian nunca podem ser apagados

Nenhum processo do DWYT pode deletar, limpar ou sobrescrever os vaults de projeto.

Isso vale para: install, uninstall, reinstall, clean, reset, update, rebuild, troca de projeto, migração de versão.

O caminho abaixo é dado persistente e sagrado:

```
~/.dwyt/projects/<id>/obsidian/
```

Se algum comando precisar limpar algo, limpa apenas binários, caches temporários, logs descartáveis ou arquivos explicitamente marcados como regeneráveis.

### R3 — Memória isolada por projeto

```
~/.dwyt/projects/<sha256(projectPath)[:12]>/obsidian/
```

Ao trocar de projeto, o backend recarrega o `ProjectObsidian` correspondente.

### R4 — MCPs obrigatórios com nomes exatos

Os MCPs obrigatórios do DWYT devem usar os nomes curtos `codebase` e `obsidian` em **todos os lugares** — registry, dashboard, arquivos de projeto e Kiro Power. Esses nomes já expressam a ferramenta exposta ao cliente de IA e evitam divergência entre UI, API e arquivos gerados.

Nomes canônicos:

```
codebase
obsidian
```

Nomes proibidos como **chave de MCP** (legados): `dwyt`, `dwyt-codebase`, `dwyt-obsidian`, `obsidian-mcp`. O binário `dwyt-obsidian-mcp` continua correto.

A UI, o registry, os arquivos `.mcp.json`, `.kiro/mcp.json`, `.claude/mcp.json`, `.vscode/mcp.json` e o Kiro Power devem usar os mesmos nomes `codebase` e `obsidian` de forma consistente.

### R5 — RTK não é daemon

O card do RTK não deve ter botão Start/Stop. RTK é ferramenta CLI:

```
Prefix commands with rtk
```

### R6 — Dashboard reflete a realidade

Se uma ferramenta está ativa, instalada, online ou offline, todos os endpoints e a UI devem concordar. Contradições entre `/api/status` e endpoints específicos são proibidas.

### R7 — Nenhum arquivo excede 250 linhas

Arquivos grandes devem ser divididos durante a fase de refatoração, sem bloquear correções funcionais urgentes. Funções devem ter responsabilidade única; se um arquivo existente ainda exceder 250 linhas, registrar como dívida técnica e reduzir incrementalmente.

### R8 — Sem regressões

Não remover funcionalidades, não mudar tecnologias principais, não introduzir complexidade desnecessária, não piorar performance.

---

### R9 — URL canônica é `localhost:2737`, não `127.0.0.1:2737`

Todos os arquivos gerados, templates de instruções para IAs, documentação, mensagens de terminal e abertura de browser devem usar:

```
http://localhost:2737
```

O endereço `127.0.0.1` **só pode aparecer** em:
- Bind do servidor Go: `r.Run("127.0.0.1:2737")` — correto, mantém
- Health probes internas: `http.Get("http://127.0.0.1:2737/api/health")` — correto, mantém

**Arquivos de código que precisam ser corrigidos:**

| Arquivo | Ocorrências | Ação |
|---------|-------------|------|
| `core/cmd/dwyt/cli/root/root.go` | `openBrowserURL("http://127.0.0.1:2737/...")` | → `localhost:2737` |
| `core/cmd/dwyt/cli/root/root.go` | `fmt.Printf("Dashboard → http://127.0.0.1:2737")` | → `localhost:2737` |
| `core/internal/integrate/integrate.go` | URLs nos templates `agentsMDTemplate`, `claudeMD`, `cursorRule`, `kiroSteering`, `copilotMD` | → `localhost:2737` |
| `core/internal/mcp/obsidian.go` | `var dwytAPI = "http://127.0.0.1:2737/api"` | → `localhost:2737` |

**Nunca usar `127.0.0.1:2737` em:**
- `AGENTS.md`, `CLAUDE.md`, `POWER.md`, steering files gerados
- Mensagens `fmt.Printf` visíveis no terminal
- Documentação (`HOW-IT-WORKS.md`, `README.md`)
- Templates de `integrate.go`

### R10 — "Install Obsidian" pertence ao Setup, não ao Dashboard

O botão "Install Obsidian" **não deve existir no Dashboard**. Sua presença no card do Obsidian é um erro de UX — o Dashboard é para operar ferramentas já instaladas, não para instalar.

A instalação do app Obsidian deve ocorrer **exclusivamente no SetupWizard**, como parte do fluxo de configuração inicial.

O Dashboard do Obsidian deve ter apenas:
- `Save to Obsidian` — salvar entrada no vault
- `Search` — buscar no vault
- `Configure MCP` — configurar MCP
- `Rebuild summary` — reconstruir context.md
- `Open Vault` — abrir app Obsidian
- `Open Dir` — abrir diretório no file manager

Se o app Obsidian não estiver instalado, o Dashboard deve mostrar um aviso com link para o Setup, não um botão de instalação.

### R11 — Tipografia e elementos da UI devem ser legíveis

A UI usa fontes e elementos pequenos demais. A regra é:

- Fonte base do body: **mínimo 15px**
- Nenhum elemento de conteúdo visível abaixo de **12px**
- Títulos de cards: mínimo 18px
- Botões com padding adequado (não comprimidos)
- Toda a interface deve ser legível em **1280×800 sem zoom**

Ver detalhes de implementação em `06-rtk-and-dashboard-ux.md` → seção 7.7.

### R12 — Tema visual: Glassmorphism cinza escuro

Toda a UI do frontend adota visual **glassmorphism** com tom cinza frio e opacidade de 95%.

- Fundo da página: gradiente escuro fixo (`#0f1117` → `#1a1d27`)
- Cards e painéis: `background: rgba(30, 33, 48, 0.95)` + `backdrop-filter: blur(12px)`
- Bordas com opacidade baixa: `rgba(255,255,255,0.07)`
- Nenhum componente usa `background: white` ou `background: #fff`

Ver detalhes completos em `06-rtk-and-dashboard-ux.md` → seção 7.8.

---

## Ordem de Prioridade de Execução

1. Auditar estado atual do repo
2. Corrigir nomes e geração de MCPs
3. Corrigir contrato de status
4. Fortalecer Obsidian e proteção dos vaults
5. Corrigir Codebase metrics e Open Graph
6. Corrigir Headroom/Codex OAuth como falha não-fatal
7. Ajustar RTK como CLI
8. Refatorar botões e UX do Dashboard
9. Revisar SetupWizard
10. Atualizar configs de IAs e `.gitignore`
11. Atualizar E2E e testes de API
12. Atualizar documentação
13. Rodar validação final
14. Gerar changelog
