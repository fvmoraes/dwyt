# 08 — Busca por Bugs e Falhas

## Objetivo

Identificar e corrigir bugs latentes, race conditions, falhas silenciosas e comportamentos inesperados antes que cheguem ao usuário.

---

## Áreas de Risco Identificadas

### BUG-01 — Race condition em `startHeadroomIfNeeded()`

**Arquivo:** `core/internal/server/server.go`

**Sintoma:** Múltiplas instâncias do Headroom podem ser iniciadas simultaneamente se o daemon receber requisições concorrentes.

**Verificação:**

```bash
# Simular requisições concorrentes
for i in $(seq 1 5); do
  curl -s -X POST http://localhost:2737/api/services/headroom/start &
done
wait
# Verificar: apenas 1 processo headroom deve existir
ps aux | grep headroom | grep -v grep
```

**Correção esperada:** Mutex `headroomStartMu` deve serializar o startup.

---

### BUG-02 — `ProjectObsidian == nil` retornando "active"

**Arquivo:** `core/internal/server/server.go` → `detailObsidian()`

**Sintoma:** Se o vault não foi carregado (ex: primeiro boot, projeto sem vault), a API pode retornar status "active" incorretamente.

**Verificação:**

```bash
# Iniciar daemon sem projeto configurado
dwyt .

# Verificar status do Obsidian
curl -s http://localhost:2737/api/obsidian/status | jq '.status'
# deve retornar "inactive" ou "not_configured", nunca "active"
```

---

### BUG-03 — Métricas zeradas após indexação do Codebase

**Arquivo:** `core/internal/server/server.go` → fluxo de indexação

**Sintoma:** `MarkIndexed(path, 0, 0)` persiste nodes=0, edges=0 mesmo após indexação bem-sucedida.

**Verificação:**

```bash
# Indexar projeto
curl -s -X POST http://localhost:2737/api/codebase/index \
  -H "Content-Type: application/json" \
  -d '{"path":"'$(pwd)'"}' | jq .

# Aguardar conclusão
sleep 30

# Verificar métricas
curl -s http://localhost:2737/api/projects/current | jq '{nodes: .nodes, edges: .edges}'
# deve retornar valores > 0
```

---

### BUG-04 — Headroom wrap fatal com Codex OAuth

**Arquivo:** `core/internal/install/install.go` ou `core/internal/integrate/integrate.go`

**Sintoma:** Se o Codex usa login OAuth/ChatGPT, `headroom wrap codex` falha e pode derrubar o setup.

**Verificação:**

```bash
# Simular falha de wrap
# (requer ambiente com Codex configurado via OAuth)
curl -s -X POST http://localhost:2737/api/services/headroom/start | jq .
# deve retornar sucesso mesmo se wrap falhar
# verificar log
tail -20 ~/.dwyt/dwyt.log | grep -i "wrap\|codex\|oauth"
```

---

### BUG-05 — Botão "Open Graph" trava a UI

**Arquivo:** `core/web/src/pages/Dashboard.tsx`

**Sintoma:** Clicar em "Open Graph" pode deixar o botão em estado de loading indefinidamente se o serviço Codebase não responder.

**Verificação:**

```bash
# Parar Codebase
curl -s -X POST http://localhost:2737/api/services/codebase/stop | jq .

# Clicar em "Open Graph" na UI
# Verificar: botão deve ter timeout e mostrar erro após ~10s
```

---

### BUG-06 — Troca de projeto não recarrega vault

**Arquivo:** `core/internal/server/server.go` → `apiProjectSwitch()`

**Sintoma:** Ao trocar de projeto via `POST /api/project/switch`, o `ProjectObsidian` pode não ser recarregado, fazendo operações de Obsidian apontarem para o vault do projeto anterior.

**Verificação:**

```bash
# Projeto A
dwyt /path/to/project-a
curl -s http://localhost:2737/api/obsidian/status | jq '.vault_path'

# Trocar para projeto B
curl -s -X POST http://localhost:2737/api/project/switch \
  -H "Content-Type: application/json" \
  -d '{"path":"/path/to/project-b"}' | jq .

# Verificar vault recarregado
curl -s http://localhost:2737/api/obsidian/status | jq '.vault_path'
# deve mostrar path do projeto B
```

---

### BUG-07 — Indexação do Codebase sem timeout

**Arquivo:** `core/internal/server/server.go` → goroutine de indexação

**Sintoma:** Se `codebase-memory-mcp cli index_repository` travar, a goroutine fica rodando indefinidamente.

**Verificação:**

```bash
# Verificar se há timeout de 10 minutos implementado
grep -n "context\|timeout\|10.*min\|600" core/internal/server/server.go
```

---

### BUG-08 — Logs de serviço não capturados

**Arquivo:** `core/internal/procman/procman.go`

**Sintoma:** Se o diretório de logs não existir, stdout/stderr dos serviços são perdidos silenciosamente.

**Verificação:**

```bash
# Verificar criação do diretório de logs
ls -la ~/.dwyt/logs/
# deve existir após iniciar qualquer serviço

# Verificar conteúdo
tail -20 ~/.dwyt/logs/codebase-stdout.log
tail -20 ~/.dwyt/logs/headroom-stdout.log
```

---

### BUG-09 — Estado inconsistente após crash do daemon

**Arquivo:** `core/internal/state/state.go`

**Sintoma:** Se o daemon crashar, `state.json` pode conter PIDs de processos mortos, causando falsos positivos no próximo boot.

**Verificação:**

```bash
# Matar daemon abruptamente
kill -9 $(cat ~/.dwyt/state.json | jq -r '.daemon_pid // empty')

# Reiniciar
dwyt .

# Verificar que estado foi limpo
curl -s http://localhost:2737/api/state | jq .
# PIDs devem ser válidos ou zero
```

---

### BUG-10 — Porta em uso ao reiniciar

**Arquivo:** `core/cmd/dwyt/cli/root/root.go`

**Sintoma:** Se o daemon anterior não foi parado corretamente, a porta 2737 pode estar ocupada, impedindo o novo daemon de iniciar.

**Verificação:**

```bash
# Verificar detecção de daemon existente
dwyt .
# deve detectar daemon rodando e fazer switchProject, não tentar iniciar novo
```

---

### BUG-11 — `127.0.0.1:2737` hardcoded em código visível ao usuário

**Arquivos:**
- `core/cmd/dwyt/cli/root/root.go` — `openBrowserURL` e `fmt.Printf`
- `core/internal/integrate/integrate.go` — todos os templates de instruções para IAs
- `core/internal/mcp/obsidian.go` — `var dwytAPI`

**Sintoma:** O browser é aberto com `http://127.0.0.1:2737` e os arquivos gerados para IAs (`AGENTS.md`, `CLAUDE.md`, steering files) contêm `127.0.0.1:2737`. A URL canônica deve ser `localhost:2737`.

**Correção:**

```go
// core/cmd/dwyt/cli/root/root.go
// Antes:
openBrowserURL("http://127.0.0.1:2737/#/dashboard?project=" + ...)
fmt.Printf("  ✓ Dashboard → http://127.0.0.1:2737\n")

// Depois:
openBrowserURL("http://localhost:2737/#/dashboard?project=" + ...)
fmt.Printf("  ✓ Dashboard → http://localhost:2737\n")
```

```go
// core/internal/mcp/obsidian.go
// Antes:
var dwytAPI = "http://127.0.0.1:2737/api"

// Depois:
var dwytAPI = "http://localhost:2737/api"
```

```go
// core/internal/integrate/integrate.go
// Substituir em todos os templates (agentsMDTemplate, claudeMD, cursorRule, kiroSteering, copilotMD):
// "http://127.0.0.1:2737" → "http://localhost:2737"
```

**Verificação:**

```bash
# Não deve retornar nada após a correção (exceto bind e health probes internas)
grep -rn "127\.0\.0\.1:2737" core/internal/integrate/ core/internal/mcp/ core/cmd/
```

---

### BUG-12 — `NewProjectObsidian` rejeita projetos fora de `~/.dwyt`

**Arquivo:** `core/internal/brain/brain.go`

**Sintoma:** O vault por projeto deveria ser criado em `~/.dwyt/projects/<sha12>/obsidian/` para qualquer `projectPath`, mas a validação pode comparar o `projectPath` diretamente com `dwytHome` e rejeitar projetos normais do usuário.

**Verificação:**

```bash
# Em um projeto fora de ~/.dwyt
dwyt /tmp/dwyt-test-project
curl -s http://localhost:2737/api/obsidian/status | jq .
# deve retornar status online/active true e vault_path/obsidian_dir dentro de ~/.dwyt/projects/<sha12>/obsidian/
```

**Correção esperada:** validar que o caminho gerado do vault fica dentro de `dwytHome`; não exigir que o projeto de origem esteja dentro de `dwytHome`.

---

## Checklist de Bugs

- [ ] BUG-01: Race condition no Headroom startup
- [ ] BUG-02: Obsidian nil retornando "active"
- [ ] BUG-03: Métricas zeradas após indexação
- [ ] BUG-04: Headroom wrap fatal com Codex OAuth
- [ ] BUG-05: Botão Open Graph trava UI
- [ ] BUG-06: Troca de projeto não recarrega vault
- [ ] BUG-07: Indexação sem timeout
- [ ] BUG-08: Logs não capturados
- [ ] BUG-09: Estado inconsistente após crash
- [ ] BUG-10: Porta em uso ao reiniciar
- [ ] BUG-11: `127.0.0.1:2737` hardcoded em código visível ao usuário e templates de IAs
- [ ] BUG-12: `NewProjectObsidian` rejeitando projeto fora de `~/.dwyt`

---

## Ferramentas de Diagnóstico

```bash
# Ver todos os processos DWYT
ps aux | grep -E "dwyt|headroom|codebase" | grep -v grep

# Ver estado atual
curl -s http://localhost:2737/api/state | jq .

# Ver logs do daemon
tail -50 ~/.dwyt/dwyt.log

# Ver logs dos serviços
tail -20 ~/.dwyt/logs/headroom-stdout.log
tail -20 ~/.dwyt/logs/codebase-stdout.log

# Verificar portas em uso
ss -tlnp | grep -E "2737|8787|9749"

# Rodar testes com race detector
cd core && go test ./... -race -v
```
