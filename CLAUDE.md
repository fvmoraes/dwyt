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
