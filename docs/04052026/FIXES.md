# DWYT - Correções Implementadas

Este documento lista todas as correções críticas implementadas no DWYT para resolver problemas de estabilidade, race conditions e bugs identificados na análise de código.

## 🔴 Correções Críticas

### 1. Race Condition no ProcessManager ✅

**Problema:** PID reutilização e zombie processes causavam falsos positivos no status de processos.

**Correção:** `core/internal/procman/procman.go`
- Adicionada verificação de zombie processes no Linux via `/proc/<pid>/stat`
- PID é zerado quando processo não está mais válido
- Processo é morto se healthcheck falhar (antes ficava em estado inconsistente)

```go
func (mp *ManagedProcess) Running() bool {
	if mp.PID == 0 {
		return false
	}
	// Check if process is not a zombie (Linux only)
	if runtime.GOOS == "linux" {
		statPath := fmt.Sprintf("/proc/%d/stat", mp.PID)
		data, err := os.ReadFile(statPath)
		if err != nil {
			mp.PID = 0
			return false
		}
		fields := strings.Fields(string(data))
		if len(fields) > 2 && fields[2] == "Z" {
			mp.PID = 0
			return false
		}
	}
	// ...
}
```

**Impacto:** Elimina loops infinitos de restart e processos órfãos.

---

### 2. Headroom Start Race Condition ✅

**Problema:** Múltiplas chamadas simultâneas de `startHeadroomIfNeeded()` causavam múltiplas instâncias do Headroom.

**Correção:** `core/internal/server/server.go`
- Adicionado mutex `headroomStartMu` para serializar início do Headroom
- Verificação dupla dentro do lock para evitar race condition

```go
func (ds *DashboardServer) startHeadroomIfNeeded() {
	ds.headroomStartMu.Lock()
	defer ds.headroomStartMu.Unlock()
	
	// Double-check inside lock
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", ds.HeadroomPort)
	if health.ProbeURL(healthURL) {
		return
	}
	// ...
}
```

**Impacto:** Previne múltiplas instâncias e conflitos de porta.

---

### 3. Brain Save sem Lock ✅

**Problema:** Lock era liberado antes do write no arquivo, causando corrupção de dados.

**Correção:** `core/internal/brain/brain.go`
- Funções de append movidas para métodos `*Locked` que executam dentro do lock
- Write no arquivo acontece antes do `defer unlock`

```go
func (pb *ProjectBrain) SaveEntry(entryType, content string, tags []string) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	
	// All writes happen inside lock
	switch entryType {
	case "decision":
		return pb.appendToDecisionsLogLocked(content, now)
	// ...
	}
}
```

**Impacto:** Elimina corrupção de arquivos markdown do brain.

---

### 4. Codebase Index sem Cancelamento ✅

**Problema:** Indexação não podia ser cancelada, causando consumo infinito de CPU/memória.

**Correção:** `core/internal/server/server.go`
- Adicionado `context.Context` com timeout de 10 minutos
- Indexação anterior é cancelada ao trocar de projeto
- Suporte a `context.CancelFunc` armazenado no servidor

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
ds.codebaseIndexCancel = cancel

cmd := exec.CommandContext(ctx, bin, "cli", "index_repository", ...)
```

**Impacto:** Indexação pode ser cancelada e tem timeout automático.

---

### 5. State Save sem Tratamento de Erro ✅

**Problema:** Erros de save eram ignorados, causando perda de estado.

**Correção:** `core/internal/state/state.go`
- Erros de save são logados
- Backup automático criado em caso de falha

```go
func (s *RuntimeState) maybeSave() {
	if err := s.saveLocked(); err != nil {
		log.Error("failed to save state", log.Fields{"error": err.Error()})
		// Try to save backup
		backupPath := s.Path + ".backup"
		if data, marshalErr := json.MarshalIndent(s, "", "  "); marshalErr == nil {
			os.WriteFile(backupPath, data, 0644)
		}
	}
}
```

**Impacto:** Estado não é perdido silenciosamente.

---

## 🟡 Melhorias Importantes

### 6. Validação de Checksum no Install Script ✅

**Problema:** Binário baixado não era validado, permitindo MITM.

**Correção:** `install.sh`
- Download de `checksums.txt` do GitHub Releases
- Validação SHA256 antes de instalar
- Suporte a `sha256sum` e `shasum`

**Impacto:** Instalação mais segura.

---

### 7. Frontend Cache ao Trocar Projeto ✅

**Problema:** UI mostrava dados do projeto anterior após switch.

**Correção:** `core/web/src/pages/Dashboard.tsx`
- Cache limpo ao receber evento `project_switch` via SSE
- Reload forçado após troca de projeto

```typescript
if (data.event === 'project_switch') {
  // Clear cache and reload everything
  setTools([])
  setDetails({})
  setBrainStats(null)
  setIndexPath(data.message)
  setTimeout(pollAll, 100)
}
```

**Impacto:** UI sempre reflete estado correto.

---

### 8. RTK Metrics Validação ✅

**Problema:** RTK retornava dados globais ao invés de por projeto.

**Correção:** `core/internal/status/status.go`
- Verificação se `.rtk/` existe no projeto antes de executar
- Retorna `nil` se RTK não inicializado

```go
func GetRTKMetricsForPath(dwytBin, projectPath string) *RTKMetrics {
	// Check if RTK is initialized in this project
	if _, err := os.Stat(filepath.Join(projectPath, ".rtk")); err != nil {
		return nil
	}
	// ...
}
```

**Impacto:** Métricas corretas por projeto.

---

### 9. Integrate Error Handling ✅

**Problema:** Erros de file write eram ignorados silenciosamente.

**Correção:** `core/internal/integrate/integrate.go`
- Arquivo é criado se não existir
- Erros são retornados ao invés de ignorados

```go
func appendMarkedBlock(filePath, block string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(filepath.Dir(filePath), 0755)
			return os.WriteFile(filePath, []byte(block), 0644)
		}
		return err
	}
	// ...
}
```

**Impacto:** Configuração de clientes mais confiável.

---

## 🧪 Testes Implementados

### Testes Unitários

#### ProcessManager (`core/internal/procman/procman_test.go`)
- ✅ `TestProcessManager_StartStop` - Ciclo completo de start/stop
- ✅ `TestProcessManager_HealthcheckFailure` - Falha de healthcheck
- ✅ `TestProcessManager_PortConflict` - Conflito de porta
- ✅ `TestProcessManager_Logs` - Captura de logs
- ✅ `TestProcessManager_Restart` - Restart de processo
- ✅ `TestProcessManager_AllStatus` - Status de múltiplos serviços

#### RuntimeState (`core/internal/state/state_test.go`)
- ✅ `TestRuntimeState_Init` - Inicialização
- ✅ `TestRuntimeState_RegisterProcess` - Registro de processo
- ✅ `TestRuntimeState_ConcurrentWrites` - Writes concorrentes (100 goroutines)
- ✅ `TestRuntimeState_SetProcessHealthy` - Atualização de health
- ✅ `TestRuntimeState_RemoveProcess` - Remoção de processo
- ✅ `TestRuntimeState_SetCurrentProject` - Troca de projeto
- ✅ `TestRuntimeState_UpdateProjectBrain` - Atualização de brain
- ✅ `TestRuntimeState_Persistence` - Persistência em disco
- ✅ `TestRuntimeState_SaveFailureBackup` - Backup em caso de falha
- ✅ `TestRuntimeState_Snapshot` - Snapshot para API

### Testes E2E (`core/test-e2e.sh`)
- ✅ Daemon startup e health
- ✅ Brain save, search e summarize
- ✅ Project switching
- ✅ Brain isolation entre projetos
- ✅ State persistence após restart
- ✅ Todos os endpoints da API

**Executar testes:**
```bash
# Testes unitários
cd core
go test ./internal/procman -v
go test ./internal/state -v

# Testes E2E
cd core
./test-e2e.sh
```

---

## 📊 Resumo de Impacto

| Correção | Severidade | Impacto | Status |
|----------|-----------|---------|--------|
| ProcessManager Race Condition | 🔴 Crítico | Elimina loops infinitos | ✅ |
| Headroom Start Race | 🔴 Crítico | Previne múltiplas instâncias | ✅ |
| Brain Save Lock | 🔴 Crítico | Elimina corrupção de dados | ✅ |
| Codebase Index Cancel | 🔴 Crítico | Permite cancelamento | ✅ |
| State Save Error | 🔴 Crítico | Previne perda de estado | ✅ |
| Install Checksum | 🟡 Importante | Segurança na instalação | ✅ |
| Frontend Cache | 🟡 Importante | UI sempre atualizada | ✅ |
| RTK Metrics | 🟡 Importante | Métricas corretas | ✅ |
| Integrate Errors | 🟡 Importante | Config mais confiável | ✅ |

---

## 🚀 Próximos Passos

### Recomendações para Produção

1. **CI/CD Pipeline**
   - Executar testes unitários em cada commit
   - Executar testes E2E antes de release
   - Validar build em todas as plataformas

2. **Monitoramento**
   - Adicionar métricas de health dos serviços
   - Log estruturado com níveis (DEBUG/INFO/WARN/ERROR)
   - Alertas para falhas de healthcheck

3. **Documentação**
   - Guia de troubleshooting
   - Arquitetura detalhada
   - Guia de contribuição

4. **Performance**
   - Benchmark de operações críticas
   - Profiling de CPU/memória
   - Otimização de polling/SSE

---

## 📝 Notas de Desenvolvimento

### Como Testar Localmente

```bash
# 1. Build
cd core
go build -o dwyt .

# 2. Executar testes unitários
go test ./... -v

# 3. Executar testes E2E
./test-e2e.sh

# 4. Testar manualmente
./dwyt .
```

### Debugging

```bash
# Ver logs do daemon
tail -f ~/.dwyt/dwyt.log

# Ver logs de serviços
tail -f ~/.dwyt/logs/headroom-stdout.log
tail -f ~/.dwyt/logs/codebase-stdout.log

# Ver estado runtime
curl http://127.0.0.1:2737/api/state | jq

# Ver brain status
curl http://127.0.0.1:2737/api/brain/status | jq
```

---

## ✅ Checklist de Validação

Antes de fazer release, validar:

- [ ] Todos os testes unitários passam
- [ ] Teste E2E passa
- [ ] Build funciona em Linux/Mac/Windows
- [ ] Install script funciona
- [ ] Daemon inicia sem erros
- [ ] Serviços sobem corretamente
- [ ] Brain salva e busca funcionam
- [ ] Troca de projeto funciona
- [ ] State persiste após restart
- [ ] UI reflete estado correto
- [ ] Logs são gerados corretamente
- [ ] Checksum é validado na instalação

---

**Data:** 2026-05-04  
**Versão:** 3.1.0  
**Autor:** Análise e correções implementadas via Kiro
