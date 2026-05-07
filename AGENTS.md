# DWYT — Don't Waste Your Tokens

Este projeto usa ferramentas automáticas para reduzir consumo de tokens.
**Todas as integrações são transparentes: você não precisa configurar nada.**

## Commits

- Use **Conventional Commits**: `<tipo>(escopo opcional): descrição`.
  Tipos válidos: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`.
- Mensagem **curta** (uma linha, idealmente ≤ 72 caracteres). Sem corpo a menos que indispensável.
- **Não** inclua linhas `Co-Authored-By:` nem qualquer outro coautor.

## Ordem de prioridade (siga sempre esta ordem):

### 1. Obsidian — VAULT DO PROJETO (PRIMEIRA FONTE DE CONTEXTO)
**LEI DO OBSIDIAN — OBRIGATORIO:** o vault e a memoria oficial do projeto. Toda tarefa deve consultar, enriquecer e salvar contexto nele.

- **ANTES de agir:** buscar notas existentes e resumir o vault.
  - Search: GET http://localhost:2737/api/obsidian/search?q=<query>
  - Summarize: POST http://localhost:2737/api/obsidian/summarize
- **DURANTE a acao:** salvar decisoes e tarefas/status importantes.
  - Decision: POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"..."}
  - Task: POST http://localhost:2737/api/obsidian/save {"type":"task","content":"..."}
- **AO FINAL de toda tarefa:** salvar contexto completo.
  - Context: POST http://localhost:2737/api/obsidian/context
  - Payload: {"client":"codex","user_request":"...","summary":"...","files":["..."],"decisions":["..."],"actions":["..."],"commands":["..."],"errors":["..."],"outcome":"...","next_steps":["..."],"context":"..."}
- Mantenha o vault rico, interligado e organizado com notas, folders, links internos, templates e instrucoes.
- Nunca encerre uma tarefa sem salvar contexto no Obsidian.

### 2. Headroom — Compressão de API (AUTOMÁTICO)
- Se `OPENAI_BASE_URL` ou `ANTHROPIC_BASE_URL` estiverem setadas, use-as.
- O Headroom comprime chamadas automaticamente — você não precisa fazer nada.
- Se não estiver ativo, use os endpoints padrão das APIs.

### 3. RTK — Compressão de Terminal
- **SEMPRE** prefixe comandos shell com `rtk`. Ex: `rtk git status`
- Isto reduz o output em 60-90% antes de entrar no contexto.
- Em comandos encadeados, prefixe cada segmento: `rtk git add . && rtk git commit -m "msg"`

### 4. Codebase — Mapa do Código (SOB DEMANDA)
- **APENAS** use o MCP codebase-memory-mcp quando precisar entender estrutura real.
- Prefira consultar o Obsidian/contexto do projeto antes de indexar ou navegar no código.
- Use `search_graph`, `trace_path`, `get_code_snippet` ao invés de grep/glob.


<!-- headroom:rtk-instructions -->
# RTK (Rust Token Killer) - Token-Optimized Commands

When running shell commands, **always prefix with `rtk`**. This reduces context
usage by 60-90% with zero behavior change. If rtk has no filter for a command,
it passes through unchanged — so it is always safe to use.

## Key Commands
```bash
# Git (59-80% savings)
rtk git status          rtk git diff            rtk git log

# Files & Search (60-75% savings)
rtk ls <path>           rtk read <file>         rtk grep <pattern>
rtk find <pattern>      rtk diff <file>

# Test (90-99% savings) — shows failures only
rtk pytest tests/       rtk cargo test          rtk test <cmd>

# Build & Lint (80-90% savings) — shows errors only
rtk tsc                 rtk lint                rtk cargo build
rtk prettier --check    rtk mypy                rtk ruff check

# Analysis (70-90% savings)
rtk err <cmd>           rtk log <file>          rtk json <file>
rtk summary <cmd>       rtk deps                rtk env

# GitHub (26-87% savings)
rtk gh pr view <n>      rtk gh run list         rtk gh issue list

# Infrastructure (85% savings)
rtk docker ps           rtk kubectl get         rtk docker logs <c>

# Package managers (70-90% savings)
rtk pip list            rtk pnpm install        rtk npm run <script>
```

## Rules
- In command chains, prefix each segment: `rtk git add . && rtk git commit -m "msg"`
- For debugging, use raw command without rtk prefix
- `rtk proxy <cmd>` runs command without filtering but tracks usage
<!-- /headroom:rtk-instructions -->

<!-- dwyt:instructions:start -->
#dwyt

# DWYT — Don't Waste Your Tokens

Este projeto usa ferramentas automáticas para reduzir consumo de tokens.
**Todas as integrações são transparentes: você não precisa configurar nada.**

## Ordem de prioridade (siga sempre esta ordem):

### 1. Obsidian — VAULT DO PROJETO (PRIMEIRA FONTE DE CONTEXTO)
**LEI DO OBSIDIAN: o vault é a memória oficial do projeto. Toda interação deve consultar, enriquecer e salvar contexto nele.**
- ANTES de agir: buscar notas existentes e resumo do vault.
  - Search: GET http://localhost:2737/api/obsidian/search?q=<query>
  - Summarize: POST http://localhost:2737/api/obsidian/summarize
- DURANTE a ação: salvar decisões e tarefas/status importantes.
  - Decision: POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"..."}
  - Task: POST http://localhost:2737/api/obsidian/save {"type":"task","content":"..."}
- AO FINAL de toda tarefa: salvar contexto completo.
  - Context: POST http://localhost:2737/api/obsidian/context
  - Payload: {"client":"codex","user_request":"...","summary":"...","files":["..."],"decisions":["..."],"actions":["..."],"commands":["..."],"errors":["..."],"outcome":"...","next_steps":["..."],"context":"..."}
- Mantenha o vault rico, interligado e organizado com notas, folders, links internos, templates e instruções.

### 2. Headroom — Compressão de API (AUTOMÁTICO)
- Se `OPENAI_BASE_URL` ou `ANTHROPIC_BASE_URL` estiverem setadas, use-as.
- O Headroom comprime chamadas automaticamente — você não precisa fazer nada.
- No Codex, o DWYT só configura Headroom quando o login é por API key; login ChatGPT/OAuth usa os endpoints padrão.
- Se não estiver ativo, use os endpoints padrão das APIs.

### 3. RTK — Compressão de Terminal
- **SEMPRE** prefixe comandos shell com `rtk`. Ex: `rtk git status`
- Isto reduz o output em 60-90% antes de entrar no contexto.
- Em comandos encadeados, prefixe cada segmento: `rtk git add . && rtk git commit -m "msg"`

### 4. Codebase — Mapa do Código (SOB DEMANDA)
- **APENAS** use o MCP codebase-memory-mcp quando precisar entender estrutura real.
- Prefira consultar o Obsidian/contexto do projeto antes de indexar ou navegar no código.
- Use `search_graph`, `trace_path`, `get_code_snippet` ao invés de grep/glob.
<!-- dwyt:instructions:end -->
