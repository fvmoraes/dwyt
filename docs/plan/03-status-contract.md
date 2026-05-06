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
- `/api/obsidian/status` deve retornar `inactive` quando vault não carregado
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
// Adicionar em core/internal/server/types.go
type ToolStatus struct {
    Name        string `json:"name"`
    Status      string `json:"status"`      // online|offline|starting|port_open_no_health|not_installed|error
    Installed   bool   `json:"installed"`
    Running     bool   `json:"running"`
    Healthy     bool   `json:"healthy"`
    Port        int    `json:"port,omitempty"`
    PID         int    `json:"pid,omitempty"`
    Error       string `json:"error,omitempty"`
    LastChecked string `json:"last_checked"`
}
```

> **Nota:** O `types.go` atual não tem `ToolStatus`. Adicionar lá, não criar arquivo separado.

### 2.2 — Atualizar `detailObsidian()`

```go
// Se ProjectObsidian == nil → retornar inactive
if s.brain == nil {
    return ToolStatus{Name: "obsidian", Status: "inactive", Installed: false}
}
```

### 2.3 — Atualizar `apiMCPRegistry`

Usar detecção em dois níveis para cada servidor MCP registrado.

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
