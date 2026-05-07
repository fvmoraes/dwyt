# DWYT — Don't Waste Your Tokens

Este projeto usa ferramentas automaticas para reduzir consumo de tokens.
Todas as integracoes devem preservar conteudo manual do usuario fora dos blocos controlados pelo DWYT.

## Commits

- Use **Conventional Commits**: `<tipo>(escopo opcional): descricao`.
  Tipos validos: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`.
- Mensagem curta, uma linha, idealmente ate 72 caracteres.
- Nao inclua linhas `Co-Authored-By:` nem qualquer outro coautor.

## Ordem de Prioridade

1. **RTK** — use para comandos shell, automacoes de terminal e comandos repetitivos.
2. **Codebase MCP** — use para entender, validar, diagnosticar ou alterar estrutura real do codigo.
3. **Obsidian MCP** — use para recuperar e salvar memoria persistente, decisoes, historico e contexto futuro.
4. **Headroom** — use apenas como proxy/cache quando compativel com o cliente de IA.

## Lei do Codebase

Antes de propor ou aplicar alteracoes tecnicas que dependam da estrutura do codigo:

- valide se o projeto esta indexado;
- use `search_graph` para localizar simbolos, rotas, handlers, componentes, modulos e relacoes;
- use `trace_path` para chamadas, fluxos, dependencias e impacto;
- use `get_code_snippet` antes de editar codigo;
- evite grep/glob/find como primeira estrategia quando o Codebase MCP estiver disponivel.

## Lei do Obsidian

O vault Obsidian em `~/.dwyt/projects/<id>/obsidian/` e a memoria oficial do projeto.

- Antes de trabalho relevante, busque notas e leia/reconstrua o resumo.
- Durante a tarefa, salve decisoes e tarefas/status importantes.
- Ao final, salve contexto completo com `user_request`, `summary`, `files`, `decisions`, `actions`, `commands`, `errors`, `outcome`, `next_steps` e `context`.
- Use links internos como `[[index]]`, `[[maps/project-map]]`, `[[instructions/obsidian-law]]` e `[[instructions/codebase-law]]`.
- Nunca apague vaults, projetos, notas ou historico como tentativa de correcao automatica.

## Headroom

- Use Headroom somente quando `OPENAI_BASE_URL` ou `ANTHROPIC_BASE_URL` apontarem para proxy compativel.
- Codex autenticado via ChatGPT/OAuth nao deve usar Headroom.
- Se Headroom estiver inativo, use endpoints padrao.

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

Este projeto usa RTK, Codebase MCP, Obsidian MCP e Headroom para reduzir desperdicio de tokens sem sobrescrever configuracoes manuais do usuario.

## Ordem de Prioridade

1. RTK — para comandos shell e automacoes de terminal.
2. Codebase MCP — fonte primaria para estrutura real do codigo.
3. Obsidian MCP — memoria persistente oficial do projeto.
4. Headroom — apenas proxy/cache compativel.

## Lei do Codebase

Quando precisar entender, validar, diagnosticar ou alterar a estrutura real do codigo, consulte o Codebase MCP. Use `search_graph`, `trace_path` e `get_code_snippet` antes de aplicar mudancas estruturais.

## Lei do Obsidian

Busque/resuma o vault antes de trabalho relevante, salve decisoes/tarefas durante a acao e salve contexto completo ao final. Use links internos como `[[index]]`, `[[maps/project-map]]`, `[[instructions/obsidian-law]]` e `[[instructions/codebase-law]]`.

Payload minimo:

```json
{
  "client": "codex",
  "user_request": "...",
  "summary": "...",
  "files": ["..."],
  "decisions": ["..."],
  "actions": ["..."],
  "commands": ["..."],
  "errors": ["..."],
  "outcome": "...",
  "next_steps": ["..."],
  "context": "..."
}
```

## Validacao

Antes de concluir mudancas, rode testes/build/lint relevantes e confirme que estados `installed`, `inactive` e `launch on demand` nao sao tratados como erro.
<!-- dwyt:instructions:end -->
