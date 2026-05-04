# DWYT Testing Guide

Este guia explica como executar e criar testes para o DWYT.

## 📋 Tipos de Testes

### 1. Testes Unitários

Testam componentes individuais isoladamente.

**Localização:** `core/internal/*/`

**Executar todos:**
```bash
cd core
go test ./... -v
```

**Executar pacote específico:**
```bash
go test ./internal/procman -v
go test ./internal/state -v
go test ./internal/brain -v
```

**Com coverage:**
```bash
go test ./... -cover
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

---

### 2. Testes de Integração

Testam interação entre componentes.

**Executar:**
```bash
go test ./internal/server -v -tags=integration
```

---

### 3. Testes E2E

Testam o sistema completo end-to-end.

**Executar:**
```bash
cd core
./test-e2e.sh
```

**O que é testado:**
- Daemon startup e health
- Brain save, search, summarize
- Project switching
- Brain isolation entre projetos
- State persistence após restart
- Todos os endpoints da API

---

## 🧪 Escrevendo Testes

### Estrutura de Teste Unitário

```go
package mypackage

import (
	"testing"
)

func TestMyFunction(t *testing.T) {
	// Arrange
	input := "test"
	expected := "expected result"
	
	// Act
	result := MyFunction(input)
	
	// Assert
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}
```

### Usando t.TempDir()

Para testes que precisam de filesystem:

```go
func TestWithFiles(t *testing.T) {
	tmpDir := t.TempDir() // Cleanup automático
	
	filePath := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(filePath, []byte("content"), 0644)
	
	// Test code...
}
```

### Testando Concorrência

```go
func TestConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Test code...
		}(i)
	}
	
	wg.Wait()
	// Verify results...
}
```

### Testando HTTP Endpoints

```go
func TestAPIEndpoint(t *testing.T) {
	// Start test server
	srv := setupTestServer(t)
	defer srv.Close()
	
	// Make request
	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	
	// Verify response
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}
```

---

## 🎯 Cobertura de Testes

### Pacotes Testados

| Pacote | Cobertura | Status |
|--------|-----------|--------|
| `procman` | ✅ Alta | 6 testes |
| `state` | ✅ Alta | 11 testes |
| `brain` | 🟡 Média | Adicionar mais |
| `server` | 🟡 Média | Adicionar mais |
| `integrate` | 🔴 Baixa | Adicionar |
| `install` | 🔴 Baixa | Adicionar |

### Metas de Cobertura

- **Crítico (procman, state, brain):** > 80%
- **Importante (server, integrate):** > 60%
- **Outros:** > 40%

---

## 🐛 Debugging de Testes

### Verbose Output

```bash
go test -v ./internal/procman
```

### Run Specific Test

```bash
go test -v -run TestProcessManager_StartStop ./internal/procman
```

### Race Detector

```bash
go test -race ./...
```

### Memory Profiling

```bash
go test -memprofile=mem.prof ./internal/state
go tool pprof mem.prof
```

### CPU Profiling

```bash
go test -cpuprofile=cpu.prof ./internal/brain
go tool pprof cpu.prof
```

---

## 🔍 Testes de Regressão

### Casos Conhecidos

1. **ProcessManager Loop Infinito**
   - Teste: `TestProcessManager_HealthcheckFailure`
   - Verifica que processo é morto após falha de healthcheck

2. **Brain Corrupção de Arquivo**
   - Teste: `TestBrain_ConcurrentSaves` (TODO)
   - Verifica que writes concorrentes não corrompem arquivo

3. **State Perda de Dados**
   - Teste: `TestRuntimeState_SaveFailureBackup`
   - Verifica que backup é criado em caso de falha

4. **UI Cache Stale**
   - Teste: E2E project switch
   - Verifica que UI atualiza após troca de projeto

---

## 📊 CI/CD Integration

### GitHub Actions

```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      
      - name: Unit Tests
        run: |
          cd core
          go test ./... -v -cover
      
      - name: E2E Tests
        run: |
          cd core
          ./test-e2e.sh
```

---

## 🚀 Performance Tests

### Benchmark

```go
func BenchmarkBrainSearch(b *testing.B) {
	brain := setupTestBrain(b)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		brain.Search("keyword")
	}
}
```

**Executar:**
```bash
go test -bench=. ./internal/brain
```

### Load Testing

```bash
# Install hey
go install github.com/rakyll/hey@latest

# Test API endpoint
hey -n 1000 -c 10 http://127.0.0.1:2737/api/health
```

---

## ✅ Checklist de Testes

Antes de fazer commit:

- [ ] Todos os testes unitários passam
- [ ] Nenhum race condition detectado (`go test -race`)
- [ ] Cobertura não diminuiu
- [ ] Testes E2E passam (se mudou server/API)
- [ ] Documentação atualizada se necessário

Antes de fazer release:

- [ ] Todos os testes passam em Linux/Mac/Windows
- [ ] Testes E2E passam
- [ ] Performance benchmarks não regrediram
- [ ] Load tests passam
- [ ] Documentação completa

---

## 📚 Recursos

- [Go Testing Package](https://pkg.go.dev/testing)
- [Table Driven Tests](https://go.dev/wiki/TableDrivenTests)
- [Testify Library](https://github.com/stretchr/testify)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)

---

## 🤝 Contribuindo com Testes

### Prioridades

1. **Alta:** Testes para bugs críticos conhecidos
2. **Média:** Aumentar cobertura de pacotes importantes
3. **Baixa:** Testes de edge cases

### Guidelines

- Um teste deve testar uma coisa
- Nomes descritivos: `TestFunctionName_Scenario_ExpectedBehavior`
- Usar `t.Helper()` em funções auxiliares
- Cleanup automático com `t.Cleanup()` ou `defer`
- Evitar sleeps - usar channels ou polling com timeout

### Exemplo de Bom Teste

```go
func TestProcessManager_StartStop_ProcessIsKilled(t *testing.T) {
	t.Helper()
	
	// Arrange
	tmpDir := t.TempDir()
	pm := New(tmpDir)
	pm.Register("test", "/bin/sleep", "", 0, "10")
	
	// Act - Start
	status, err := pm.Start("test")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	
	// Assert - Running
	if !status.Running {
		t.Error("Expected process to be running")
	}
	
	// Act - Stop
	status, err = pm.Stop("test")
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	
	// Assert - Stopped
	if status.Running {
		t.Error("Expected process to be stopped")
	}
	
	// Verify process was actually killed
	time.Sleep(100 * time.Millisecond)
	proc, _ := os.FindProcess(status.PID)
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		t.Error("Process should be dead but is still running")
	}
}
```

---

**Última atualização:** 2026-05-04
