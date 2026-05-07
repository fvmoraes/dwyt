# DWYT — instrucoes do projeto

## Ordem de prioridade

1. **RTK**: prefixe comandos shell com `rtk`.
2. **Codebase MCP**: use `search_graph`, `trace_path` e `get_code_snippet` para entender estrutura real do codigo antes de diagnosticar ou editar.
3. **Obsidian MCP**: busque/resuma o vault antes de trabalho relevante, salve decisoes/tarefas durante a acao e salve contexto completo ao final.
4. **Headroom**: use apenas quando compativel; Codex via ChatGPT/OAuth nao deve ser roteado por Headroom.

## Commits

- Use **Conventional Commits**: `<tipo>(escopo opcional): descricao`.
- Mensagem curta, uma linha, idealmente ate 72 caracteres.
- Nao inclua linhas `Co-Authored-By:` nem qualquer outro coautor.

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

Quando precisar entender, validar, diagnosticar ou alterar a estrutura real do codigo, consulte o Codebase MCP. O grafo indexado e a fonte primaria para arquivos, simbolos, dependencias, chamadas, caminhos e impacto.

## Lei do Obsidian

O vault Obsidian e a memoria oficial do projeto. Salve notas com links internos como `[[index]]`, `[[maps/project-map]]`, `[[instructions/obsidian-law]]` e `[[instructions/codebase-law]]`.

Payload minimo:

```json
{
  "client": "claude",
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

Nunca encerre tarefa relevante sem salvar contexto no Obsidian.
<!-- dwyt:instructions:end -->
