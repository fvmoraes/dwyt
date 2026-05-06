# 05 — Codebase e Headroom

## Fase 4 — Corrigir Codebase Memory e Métricas Reais

**Objetivo:** Fazer o Dashboard refletir a indexação real do Codebase.

---

## Tarefas Codebase

### 4.1 — Revisar fluxo de indexação

Endpoints envolvidos (todos já existem no `routes.go`):

```
POST /api/codebase/index
GET  /api/codebase/index/status
POST /api/codebase/open-ui
POST /api/services/codebase/start
POST /api/services/codebase/stop
GET  /api/services/codebase/status
GET  /api/services/codebase/logs
```

### 4.2 — Contar nodes/edges reais após indexação

A função `countCodebaseGraph()` já existe em `server.go`. Verificar se está sendo chamada
corretamente após a indexação e se os resultados são persistidos no SQLite:

```go
// Verificar que após indexação:
nodes, edges := countCodebaseGraph(ds.DwytHome, projectPath)
ds.Store.MarkIndexed(projectPath, nodes, edges)
// nodes e edges devem ser > 0 para projetos com código
```

### 4.3 — Persistir métricas reais no SQLite

```go
// Substituir:
db.MarkIndexed(path, 0, 0)

// Por:
nodes, edges := countCodebaseGraph(ds.DwytHome, path)
db.MarkIndexed(path, nodes, edges)
```

### 4.4 — Remover qualquer uso de `MarkIndexed(path, 0, 0)`

Buscar e eliminar todas as ocorrências de métricas zeradas hardcoded.

### 4.5 — Open Graph assíncrono

`POST /api/codebase/open-ui` deve:

1. Iniciar o serviço Codebase se não estiver rodando
2. Retornar imediatamente com `{"status": "starting"}`
3. Frontend faz polling de `GET /api/services/codebase/status` até `online`
4. Abrir URL do grafo quando disponível

Não deve travar o botão nem a UI.

### 4.6 — Start/Stop dedicados para Codebase

```
POST /api/services/codebase/start
POST /api/services/codebase/stop
GET  /api/services/codebase/status
```

---

## Critérios de Aceite — Codebase

- [ ] Após indexar, `/api/projects/current` mostra nodes/edges reais
- [ ] Dashboard não mostra 0/0 quando há grafo indexado
- [ ] Open Graph não deixa botão travado
- [ ] Codebase pode ser iniciado/parado de forma dedicada
- [ ] `CBM_CACHE_DIR` aponta para `~/.dwyt/codebase`

---

---

## Fase 5 — Corrigir Headroom e Integração com Codex

**Objetivo:** Tornar o Headroom útil sem quebrar o fluxo do Codex com OAuth.

---

## Tarefas Headroom

### 5.1 — Revisar start/stop do Headroom

Endpoints existentes no `routes.go`:

```
POST /api/headroom/start          → apiHeadroomStartPM (legacy compat)
POST /api/headroom/stop           → apiHeadroomStopPM  (legacy compat)
POST /api/services/headroom/start → apiHeadroomStartPM (mesmo handler)
POST /api/services/headroom/stop  → apiHeadroomStopPM  (mesmo handler)
GET  /api/services/headroom/status → apiHeadroomStatusPM
GET  /api/services/headroom/logs   → apiHeadroomLogsPM
GET  /api/headroom/stats-url       → apiHeadroomStatsURL
```

> **Nota:** `/api/headroom/start` e `/api/services/headroom/start` apontam para o mesmo handler.
> Isso é intencional (compatibilidade). Não remover a rota legacy.

### 5.2 — Garantir proxy na porta 8787

Quando ativo, o Headroom deve estar acessível em `http://127.0.0.1:8787`.

### 5.3 — Injeção segura de env vars

```bash
# env.sh deve exportar:
export HEADROOM_PORT=8787
export OPENAI_BASE_URL="http://127.0.0.1:8787/v1"
export ANTHROPIC_BASE_URL="http://127.0.0.1:8787"
```

### 5.4 — `headroom wrap codex` como falha não-fatal

```go
if err := headroom.WrapCodex(); err != nil {
    log.Warn("headroom wrap codex failed (non-fatal): %v", err)
    // Continuar normalmente — não retornar erro
}
```

### 5.5 — Detectar Codex com OAuth/ChatGPT

Se o Codex estiver logado via ChatGPT/OAuth, o wrap não pode ser aplicado. Logar warning claro:

```
WARN: Codex uses OAuth login — headroom wrap not applicable
```

### 5.6 — Stop deve tentar unwrap

Ao parar o Headroom, tentar reverter a injeção de proxy config nos arquivos de cliente.

### 5.7 — Métricas do Headroom

Quando disponíveis, exibir no Dashboard:

```
GET /api/headroom/stats-url → URL do painel de métricas
```

---

## Critérios de Aceite — Headroom

- [ ] Headroom inicia e para pelo Dashboard
- [ ] Falha no wrap do Codex não derruba instalação nem daemon
- [ ] Usuário vê warning compreensível, não erro fatal
- [ ] Métricas do Headroom aparecem quando disponíveis
- [ ] Stop remove proxy config dos arquivos de cliente

---

## Verificação

```bash
# Testar start/stop Codebase
curl -s -X POST http://localhost:2737/api/services/codebase/start | jq .
curl -s http://localhost:2737/api/services/codebase/status | jq .
curl -s -X POST http://localhost:2737/api/services/codebase/stop | jq .

# Testar indexação
curl -s -X POST http://localhost:2737/api/codebase/index \
  -H "Content-Type: application/json" \
  -d '{"path":"/path/to/project"}' | jq .

# Polling de status
curl -s http://localhost:2737/api/codebase/index/status | jq .

# Verificar métricas reais
curl -s http://localhost:2737/api/projects/current | jq '.nodes, .edges'

# Testar Headroom
curl -s -X POST http://localhost:2737/api/services/headroom/start | jq .
curl -s http://localhost:2737/api/services/headroom/status | jq .
curl -s http://127.0.0.1:8787/health  # deve responder quando ativo
```
