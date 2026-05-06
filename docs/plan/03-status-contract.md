# 03 — Contrato Único de Status

## Fase 2 — Unificar Status Real das Ferramentas

**Objetivo:** Eliminar contradições entre endpoints e UI.

---

## Endpoints a Revisar

```
GET /api/status
GET /api/tool-details
GET /api/services/codebase/status
GET /api/services/headroom/status
GET /api/obsidian/status
GET /api/mcp/registry
```

---

## Contrato de Status

### Estados Mínimos Esperados

| Estado | Significado |
|--------|-------------|
| `online` | Processo rodando + health check passou |
| `offline` | Processo não está rodando |
| `starting` | Processo iniciado, aguardando health check |
| `port_open_no_health` | Porta ocupada mas health check falhou |
| `installed` | Binário ou recurso existe, mas não é processo persistente |
| `inactive` | Recurso de projeto não carregado, por exemplo vault ausente |
| `not_installed` | Binário não encontrado em `~/.dwyt/bin/` |
| `error` | Erro ao iniciar ou verificar |

### Detecção em Dois Níveis (para MCPs e serviços)

```
Nível 1: ProcessManager
  → verifica PID registrado + processo vivo

Nível 2: Porta + health probe (fallback)
  → se ProcMan não conhece o processo
  → tenta GET http://127.0.0.1:<port>/health
  → porta aberta + health OK → "online"
  → porta aberta + health falha → "port_open_no_health"
  → porta fechada → "offline"
```

---

## Regras de Consistência

### Obsidian

- Se `ProjectObsidian == nil` → nunca retornar `active` ou `online`
- `/api/obsidian/status` deve retornar `status:"inactive"` quando vault não carregado
- Durante a migração, `/api/obsidian/status` pode manter `active:false` para compatibilidade, mas `status` é o campo canônico
- `/api/status` deve concordar com `/api/obsidian/status`

### Codebase

- Se porta 9749 responde ao health check → `online` (mesmo que ProcMan não saiba)
- Se ProcMan diz `running: false` mas porta está aberta → `port_open_no_health`
- Após indexação → nodes/edges devem ser reais (não zero)

### Headroom

- Se proxy na porta 8787 responde → `online`
- Se processo morreu mas porta ainda está ocupada → `port_open_no_health`
- Falha no wrap do Codex → não muda status do Headroom

### RTK

- RTK não tem status de processo (é CLI)
- Status deve ser: `installed` ou `not_installed`
- Métricas de economia são opcionais

---

## Tarefas

### 2.1 — Definir struct de status comum

```go
// Preferir um tipo compartilhado em core/internal/status/status.go.
// Se precisar expor também pelo pacote server, criar type alias em core/internal/server/types.go.
type ToolStatus struct {
    Name        string `json:"name"`
    Status      string `json:"status"`      // online|offline|starting|port_open_no_health|installed|inactive|not_installed|error
    Installed   bool   `json:"installed"`
    Running     bool   `json:"running"`
    Healthy     bool   `json:"healthy"`
    Port        int    `json:"port,omitempty"`
    PID         int    `json:"pid,omitempty"`
    Error       string `json:"error,omitempty"`
    LastChecked string `json:"last_checked"`
}
```

> **Nota:** O código atual usa `state` em `internal/status.ToolStatus` e `active` em `/api/obsidian/status`. Migrar de forma compatível: adicionar `status` como campo canônico, manter campos antigos somente enquanto o frontend e testes forem atualizados.

### 2.2 — Atualizar `detailObsidian()`

```go
// Se ProjectObsidian == nil → retornar inactive
if ds.ProjectObsidian == nil {
    return ToolStatus{Name: "obsidian", Status: "inactive", Installed: true}
}
```

### 2.3 — Atualizar `apiMCPRegistry`

Usar detecção em dois níveis para cada servidor MCP registrado.

Resposta canônica:

```json
{
  "mcpServers": {
    "codebase": { "status": "online", "installed": true },
    "obsidian": { "status": "installed", "installed": true }
  }
}
```

Não usar o formato legado `{"servers":[...]}` nos testes ou no frontend.

### 2.4 — Atualizar `/api/status`

Garantir que o endpoint agregado use os mesmos dados dos endpoints específicos.

### 2.5 — Dashboard usa apenas contrato real

Remover qualquer inferência frágil no frontend. O Dashboard deve exibir exatamente o que a API retorna.

---

## Critérios de Aceite

- [ ] Obsidian não aparece ativo quando não há vault carregado
- [ ] Codebase não aparece offline se a porta/health probe estiver funcionando
- [ ] `/api/status` e endpoints específicos não se contradizem
- [ ] Dashboard mostra estados coerentes com API
- [ ] UI exibe 🟢 online, 🟡 port_open_no_health, 🔴 offline/not_installed

---

## Verificação

```bash
# Iniciar daemon
dwyt .

# Verificar consistência
curl -s http://localhost:2737/api/status | jq .
curl -s http://localhost:2737/api/obsidian/status | jq .
curl -s http://localhost:2737/api/services/codebase/status | jq .
curl -s http://localhost:2737/api/services/headroom/status | jq .
curl -s http://localhost:2737/api/mcp/registry | jq .

# Comparar: nenhum campo deve contradizer outro

# Verificar endpoint /api/state (existe no routes.go)
curl -s http://localhost:2737/api/state | jq .
```

> **Nota:** O endpoint `/api/tool-details` existe como `GET /api/tool-details?path=` no `routes.go`.
> Incluir na verificação de consistência.
