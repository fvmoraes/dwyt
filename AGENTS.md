# DWYT — Don't Waste Your Tokens

Este projeto usa ferramentas automáticas para reduzir consumo de tokens.
**Todas as integrações são transparentes: você não precisa configurar nada.**

## Ordem de prioridade (siga sempre esta ordem):

### 1. Obsidian — VAULT DO PROJETO (PRIMEIRA FONTE DE CONTEXTO)
The project vault is an Obsidian vault with markdown files.
Search: GET http://localhost:2737/api/obsidian/search?q=<query>
Save: POST http://localhost:2737/api/obsidian/save

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
- Use `search_graph`, `trace_call_path`, `get_code_snippet` ao invés de grep/glob.
