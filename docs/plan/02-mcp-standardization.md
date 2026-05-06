# 02 — Padronização de MCPs

## Fase 1 — Padronizar Contrato de MCP

**Objetivo:** Garantir que os MCPs obrigatórios do DWYT usem os nomes `codebase` e `obsidian` em todos os lugares, de forma consistente.

Os nomes canônicos são:

```
codebase
obsidian
```

Nomes legados a eliminar como **chave de MCP**: `dwyt-codebase`, `dwyt-obsidian`, `dwyt` (genérico), `obsidian-mcp`. O binário `dwyt-obsidian-mcp` continua correto.

---

## Tarefas

### 1.1 — Revisar `core/internal/mcpregistry/registry.go`

Atualizar ou manter o registry com os nomes canônicos:

```
codebase
obsidian
```

Remover qualquer ocorrência desses nomes quando aparecem como chave de MCP: `dwyt-codebase`, `dwyt-obsidian`, `obsidian-mcp`, `dwyt` (genérico).

> **Nota:** O registry atual já usa `codebase` e `obsidian` como defaults. Preservar essa direção e eliminar resíduos com prefixo `dwyt-` somente quando forem chave de MCP.

### 1.2 — Revisar geração de `.mcp.json` em `integrate.go`

Garantir geração de MCPs `codebase` e `obsidian` para todos os arquivos de configuração:

| Arquivo | Local |
|---------|-------|
| `.mcp.json` | raiz do projeto |
| `.claude/mcp.json` | `.claude/` |
| `.kiro/mcp.json` | `.kiro/` |
| `.vscode/mcp.json` | `.vscode/` |
| `opencode.json` | raiz do projeto |

> **Nota:** O `integrate.go` atual gera `codebase` e `obsidian` via `mcpJSONTemplate()`. A correção necessária é garantir que arquivos existentes sejam migrados quando estiverem incompletos ou com nomes legados.

### 1.3 — Garantir que o Dashboard leia as mesmas chaves do registry

O Dashboard não pode inferir nomes de MCP — deve usar exatamente o que o endpoint `/api/mcp/registry` retorna (`codebase`, `obsidian`).

### 1.4 — Garantir `ConfigureMCPByName()`

O método `ConfigureMCPByName(name string)` deve aceitar `"codebase"` e `"obsidian"` como nomes válidos.

### 1.5 — Instalar `dwyt-obsidian-mcp` automaticamente

Quando Obsidian estiver selecionado no Setup, o binário `dwyt-obsidian-mcp` deve ser instalado em `~/.dwyt/bin/`.

---

## Critérios de Aceite

- [ ] Nenhum arquivo gerado usa `dwyt-codebase`, `dwyt-obsidian`, `dwyt` genérico ou `obsidian-mcp` como chave
- [ ] Nenhum local usa `dwyt` genérico ou `obsidian-mcp` como chave de MCP
- [ ] Dashboard mostra status de `codebase` e `obsidian` separadamente
- [ ] Botão "Configure MCP" de cada card configura o MCP correto (`codebase` ou `obsidian`)
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
    "codebase": {
      "type": "stdio",
      "command": "/home/<user>/.dwyt/bin/codebase-memory-mcp",
      "args": ["--ui=true", "--port=9749"]
    },
    "obsidian": {
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
    "codebase": {
      "type": "stdio",
      "command": "/home/<user>/.dwyt/bin/codebase-memory-mcp",
      "args": ["--ui=true", "--port=9749"]
    },
    "obsidian": {
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
    "codebase": {
      "command": "/home/<user>/.dwyt/bin/codebase-memory-mcp",
      "args": ["--ui=true", "--port=9749"]
    },
    "obsidian": {
      "command": "/home/<user>/.dwyt/bin/dwyt-obsidian-mcp",
      "args": []
    }
  }
}
```

---

## Verificação Rápida

```bash
# Verificar nomes legados no código-fonte (não deve retornar nada como chave MCP)
grep -r '"dwyt-codebase":\|"dwyt-obsidian":\|"dwyt":\|"obsidian-mcp":' core/internal/mcpregistry/ core/internal/integrate/

# Verificar arquivos gerados no projeto
cat .mcp.json | jq '.mcpServers | keys'
cat .claude/mcp.json | jq '.mcpServers | keys'
cat .kiro/mcp.json | jq '.mcpServers | keys'
cat .vscode/mcp.json | jq '.mcpServers | keys'
# Todos devem retornar: ["codebase", "obsidian"]

# Verificar registry
curl -s http://localhost:2737/api/mcp/registry | jq '.mcpServers | keys'
# deve retornar: ["codebase", "obsidian"]
```
