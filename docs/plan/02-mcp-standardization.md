# 02 — Padronização de MCPs

## Fase 1 — Padronizar Contrato de MCP

**Objetivo:** Garantir que os MCPs do DWYT usem o prefixo `dwyt-` em todos os lugares, de forma consistente.

Os nomes canônicos são:

```
dwyt-codebase
dwyt-obsidian
```

Nomes legados a eliminar: `codebase` (sem prefixo), `obsidian` (sem prefixo), `dwyt` (genérico), `obsidian-mcp`.

---

## Tarefas

### 1.1 — Revisar `core/internal/mcpregistry/registry.go`

Atualizar registry para usar nomes com prefixo:

```
dwyt-codebase
dwyt-obsidian
```

Remover qualquer ocorrência de: `codebase` (sem prefixo), `obsidian` (sem prefixo), `obsidian-mcp`, `dwyt` (genérico).

> **Nota:** O registry atual usa `codebase` e `obsidian` como defaults (sem prefixo).
> Migrar para `dwyt-codebase` e `dwyt-obsidian`.

### 1.2 — Revisar geração de `.mcp.json` em `integrate.go`

Garantir geração de MCPs com prefixo `dwyt-` para todos os arquivos de configuração:

| Arquivo | Local |
|---------|-------|
| `.mcp.json` | raiz do projeto |
| `.claude/mcp.json` | `.claude/` |
| `.kiro/mcp.json` | `.kiro/` |
| `.vscode/mcp.json` | `.vscode/` |
| `opencode.json` | raiz do projeto |

> **Nota:** O `integrate.go` atual gera `codebase` e `obsidian` via `mcpJSONTemplate()`.
> Atualizar para `dwyt-codebase` e `dwyt-obsidian`.

### 1.3 — Garantir que o Dashboard leia as mesmas chaves do registry

O Dashboard não pode inferir nomes de MCP — deve usar exatamente o que o endpoint `/api/mcp/registry` retorna (`dwyt-codebase`, `dwyt-obsidian`).

### 1.4 — Garantir `ConfigureMCPByName()`

O método `ConfigureMCPByName(name string)` deve aceitar `"dwyt-codebase"` e `"dwyt-obsidian"` como nomes válidos.

### 1.5 — Instalar `dwyt-obsidian-mcp` automaticamente

Quando Obsidian estiver selecionado no Setup, o binário `dwyt-obsidian-mcp` deve ser instalado em `~/.dwyt/bin/`.

---

## Critérios de Aceite

- [ ] Nenhum arquivo gerado usa `codebase` ou `obsidian` sem o prefixo `dwyt-`
- [ ] Nenhum local usa `dwyt` genérico ou `obsidian-mcp` como chave
- [ ] Dashboard mostra status de `dwyt-codebase` e `dwyt-obsidian` separadamente
- [ ] Botão "Configure MCP" de cada card configura o MCP correto (`dwyt-codebase` ou `dwyt-obsidian`)
- [ ] `dwyt-obsidian-mcp` é instalado quando Obsidian é selecionado

---

## Estrutura Esperada dos Arquivos MCP

> **Nota:** O `codebase-memory-mcp` expõe um servidor HTTP na porta 9749 (modo `--ui=true`).
> O tipo correto nos arquivos de projeto é `"type": "stdio"` com o binário como comando.
> O vault do Obsidian é resolvido em runtime pelo daemon — não passar `--vault` como argumento estático.

### `.mcp.json` (raiz)

```json
{
  "mcpServers": {
    "dwyt-codebase": {
      "type": "stdio",
      "command": "/home/<user>/.dwyt/bin/codebase-memory-mcp",
      "args": ["--ui=true", "--port=9749"]
    },
    "dwyt-obsidian": {
      "type": "stdio",
      "command": "/home/<user>/.dwyt/bin/dwyt-obsidian-mcp",
      "args": []
    }
  }
}
```

### `.claude/mcp.json`

```json
{
  "mcpServers": {
    "dwyt-codebase": {
      "type": "stdio",
      "command": "/home/<user>/.dwyt/bin/codebase-memory-mcp",
      "args": ["--ui=true", "--port=9749"]
    },
    "dwyt-obsidian": {
      "type": "stdio",
      "command": "/home/<user>/.dwyt/bin/dwyt-obsidian-mcp",
      "args": []
    }
  }
}
```

### `.kiro/mcp.json`

```json
{
  "mcpServers": {
    "dwyt-codebase": {
      "command": "/home/<user>/.dwyt/bin/codebase-memory-mcp",
      "args": ["--ui=true", "--port=9749"]
    },
    "dwyt-obsidian": {
      "command": "/home/<user>/.dwyt/bin/dwyt-obsidian-mcp",
      "args": []
    }
  }
}
```

---

## Verificação Rápida

```bash
# Verificar nomes legados no código-fonte (não deve retornar nada)
grep -r '"codebase":\|"obsidian":' core/internal/mcpregistry/ core/internal/integrate/

# Verificar arquivos gerados no projeto
cat .mcp.json | jq '.mcpServers | keys'
cat .claude/mcp.json | jq '.mcpServers | keys'
cat .kiro/mcp.json | jq '.mcpServers | keys'
cat .vscode/mcp.json | jq '.mcpServers | keys'
# Todos devem retornar: ["dwyt-codebase", "dwyt-obsidian"]

# Verificar registry
curl -s http://localhost:2737/api/mcp/registry | jq '[.servers[].name]'
# deve retornar: ["dwyt-codebase", "dwyt-obsidian"]
```
