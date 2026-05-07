# DWYT — instruções do projeto

## Lei do Obsidian

O Obsidian e a memoria oficial do projeto. Toda interacao deve:

1. Antes de agir, consultar o vault:
   - `GET http://localhost:2737/api/obsidian/search?q=<query>`
   - `POST http://localhost:2737/api/obsidian/summarize`
2. Durante a acao, salvar decisoes e status:
   - `POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"..."}`
   - `POST http://localhost:2737/api/obsidian/save {"type":"task","content":"..."}`
3. Ao final de toda tarefa, salvar contexto completo:
   - `POST http://localhost:2737/api/obsidian/context`
   - incluir `summary`, `user_request`, `files`, `decisions`, `actions`, `commands`, `errors`, `outcome`, `next_steps` e `context`.

Nunca encerre uma tarefa sem salvar contexto no Obsidian. Mantenha o vault rico, interligado e organizado com notas, folders, links internos, templates e instrucoes.

## Commits

- Use **Conventional Commits**: `<tipo>(escopo opcional): descrição`.
  Tipos válidos: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`.
- Mensagem **curta** (uma linha, idealmente ≤ 72 caracteres). Sem corpo a menos que indispensável.
- **Não** inclua linhas `Co-Authored-By:` nem qualquer outro coautor.

Exemplos:
- `fix(install): use python 3.12 quando disponível para o venv do headroom`
- `feat(obsidian): instalação automática via brew cask no macOS`
- `chore: bump dependências do go.mod`

<!-- dwyt:instructions:start -->
#dwyt

# DWYT — Don't Waste Your Tokens

## Priority order (follow strictly):
1. **Obsidian FIRST** — before any file read or context rebuild:
   - Search: GET http://localhost:2737/api/obsidian/search?q=your+query
   - Summarize: POST http://localhost:2737/api/obsidian/summarize
   - Save: POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"..."}
   - Save tasks/status: POST http://localhost:2737/api/obsidian/save {"type":"task","content":"..."}
   - Save complete context at task end: POST http://localhost:2737/api/obsidian/context {"client":"claude","user_request":"...","summary":"...","files":["..."],"decisions":["..."],"actions":["..."],"commands":["..."],"errors":["..."],"outcome":"...","next_steps":["..."],"context":"..."}
   - Types: command, decision, task, error, note, session
   - Never mix vaults between projects — isolation is automatic.
   - Keep the vault rich, linked, and organized with notes, folders, internal links, templates, and instructions.

2. **Headroom** — auto-detected via OPENAI_BASE_URL / ANTHROPIC_BASE_URL env vars.
   If set, use them. If not, use default API endpoints. No manual config needed.

3. **RTK** — always prefix shell commands with rtk. Reduces output 60-90%.

4. **Codebase MCP** — ONLY when you need structural code understanding.
   Prefer Obsidian context first. Use search_graph, trace_path, get_code_snippet.
<!-- dwyt:instructions:end -->
