# 🔍 Guia de Validação - DWYT v3.1.0

Este guia contém todos os comandos necessários para validar as correções implementadas.

---

## 🧪 Testes Automatizados

### Testes Unitários

```bash
# Todos os testes
cd core
go test ./... -v

# Testes específicos
go test ./internal/procman -v
go test ./internal/state -v
go test ./internal/brain -v

# Com coverage
go test ./... -cover
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Com race detector
go test ./... -race

# Teste específico
go test -v -run TestProcessManager_StartStop ./internal/procman
```

### Testes E2E

```bash
cd core
./test-e2e.sh
```

---

## 🔨 Build e Instalação

### Build Local

```bash
cd core
go build -o dwyt .
```

### Instalação

```bash
# Via script
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash

# Ou local
bash install.sh
```

### Verificar Instalação

```bash
dwyt version
which dwyt
dwyt status
```

---

## 🚀 Testes Manuais

### 1. Primeira Execução

```bash
# Limpar estado anterior
rm -rf ~/.dwyt
rm -rf /tmp/test-project

# Criar projeto de teste
mkdir -p /tmp/test-project
cd /tmp/test-project
git init
echo "console.log('test')" > index.js

# Iniciar DWYT
dwyt .
```

**Validar:**
- [ ] UI abre em http://localhost:2737
- [ ] Setup wizard aparece
- [ ] Projeto detectado corretamente

### 2. Brain Functionality

```bash
# Salvar no brain
curl -X POST http://127.0.0.1:2737/api/brain/save \
  -H "Content-Type: application/json" \
  -d '{"type":"decision","content":"Test decision"}'

# Buscar no brain
curl "http://127.0.0.1:2737/api/brain/search?q=decision" | jq

# Status do brain
curl http://127.0.0.1:2737/api/brain/status | jq

# Summarize
curl -X POST http://127.0.0.1:2737/api/brain/summarize | jq
```

**Validar:**
- [ ] Save retorna sucesso
- [ ] Search encontra a entrada
- [ ] Status mostra arquivos
- [ ] Summarize funciona

### 3. Project Switching

```bash
# Criar segundo projeto
mkdir -p /tmp/test-project-2
cd /tmp/test-project-2
git init
echo "print('test')" > main.py

# Trocar projeto
curl -X POST http://127.0.0.1:2737/api/project/switch \
  -H "Content-Type: application/json" \
  -d '{"path":"/tmp/test-project-2"}' | jq

# Verificar projeto atual
curl http://127.0.0.1:2737/api/projects/current | jq

# Verificar brain isolado
curl "http://127.0.0.1:2737/api/brain/search?q=decision" | jq
```

**Validar:**
- [ ] Switch retorna sucesso
- [ ] Projeto atual mudou
- [ ] Brain está vazio (isolado)

### 4. State Persistence

```bash
# Parar daemon
pkill -f "dwyt.*daemon"

# Reiniciar
cd /tmp/test-project
dwyt .

# Verificar estado restaurado
curl http://127.0.0.1:2737/api/state | jq
curl http://127.0.0.1:2737/api/projects/current | jq
```

**Validar:**
- [ ] Daemon reinicia sem erros
- [ ] Projeto correto carregado
- [ ] Brain preservado

### 5. ProcessManager

```bash
# Status de serviços
curl http://127.0.0.1:2737/api/status | jq

# Iniciar todos
curl -X POST http://127.0.0.1:2737/api/services/start-all | jq

# Status individual
curl http://127.0.0.1:2737/api/services/headroom/status | jq
curl http://127.0.0.1:2737/api/services/codebase/status | jq

# Logs
curl "http://127.0.0.1:2737/api/services/headroom/logs?tail=20"
curl "http://127.0.0.1:2737/api/services/codebase/logs?tail=20"

# Parar todos
curl -X POST http://127.0.0.1:2737/api/services/stop-all | jq
```

**Validar:**
- [ ] Serviços iniciam corretamente
- [ ] Status reflete estado real
- [ ] Logs são capturados
- [ ] Stop funciona

---

## 🐛 Validação de Bugs Corrigidos

### Bug #1: Loop Infinito de Restart

```bash
# Simular falha de healthcheck
# (processo inicia mas healthcheck falha)

# Verificar que processo é morto
ps aux | grep Codebase

# Verificar logs
tail -f ~/.dwyt/dwyt.log | grep "healthcheck failed"
```

**Esperado:**
- Processo é morto após falha de healthcheck
- Não há loop infinito de restart
- Log mostra "killed" após healthcheck failure

### Bug #2: State Corrompido

```bash
# Simular falha de save (disco cheio simulado)
# Verificar que backup é criado
ls -la ~/.dwyt/state.json*

# Verificar logs
tail -f ~/.dwyt/dwyt.log | grep "failed to save state"
```

**Esperado:**
- Backup criado em `state.json.backup`
- Erro logado
- Sistema continua funcionando

### Bug #3: UI Cache Stale

```bash
# Abrir UI em navegador
# Trocar projeto via CLI
cd /tmp/test-project-2
dwyt .

# Verificar que UI atualiza automaticamente
```

**Esperado:**
- UI mostra projeto correto
- Dados do projeto anterior não aparecem
- Reload automático via SSE

### Bug #4: RTK Metrics Globais

```bash
# Projeto sem RTK
mkdir -p /tmp/no-rtk-project
cd /tmp/no-rtk-project
dwyt .

# Verificar métricas
curl "http://127.0.0.1:2737/api/tool-details?path=/tmp/no-rtk-project" | jq '.rtk'
```

**Esperado:**
- RTK metrics retorna null ou valores zerados
- Não mostra dados de outros projetos

### Bug #5: Headroom Múltiplas Instâncias

```bash
# Iniciar daemon 2x rapidamente
dwyt . &
sleep 0.5
dwyt . &

# Verificar processos
ps aux | grep "headroom proxy"

# Deve haver apenas 1 instância
```

**Esperado:**
- Apenas 1 instância do Headroom
- Segunda chamada detecta que já está rodando

### 6. UI Naming Validation

```bash
# Iniciar DWYT e abrir UI
dwyt .
xdg-open http://localhost:2737
```

**Validar na UI (Dashboard):**
- [ ] Card superior esquerdo mostra **"CODEBASE"** (não "CODE MAP")
- [ ] Card superior direito mostra **"RTK"** (não "TERMINAL OPTIMIZED")
- [ ] Card inferior esquerdo mostra **"HEADROOM"** (não "ACTIVE COMPRESSION")
- [ ] Card inferior direito mostra **"OBSIDIAN"** ✅
- [ ] Barra de projeto mostra "DWYT is protecting this project"
- [ ] Contador de arquivos usa "Obsidian files" (não "brain files")
- [ ] Totals banner mostra: RTK, Headroom, Obsidian, Codebase

**Validar em PT (trocar idioma):**
- [ ] Mesmos nomes em português
- [ ] "DWYT está protegendo este projeto"

---

### Benchmark

```bash
cd core
go test -bench=. ./internal/brain
go test -bench=. ./internal/state
```

### Load Testing

```bash
# Instalar hey
go install github.com/rakyll/hey@latest

# Test health endpoint
hey -n 1000 -c 10 http://127.0.0.1:2737/api/health

# Test status endpoint
hey -n 500 -c 5 http://127.0.0.1:2737/api/status

# Test brain search
hey -n 100 -c 2 "http://127.0.0.1:2737/api/brain/search?q=test"
```

**Esperado:**
- Health: >1000 req/s
- Status: >100 req/s
- Brain search: >50 req/s

### Memory Profiling

```bash
cd core
go test -memprofile=mem.prof ./internal/state
go tool pprof mem.prof
```

### CPU Profiling

```bash
cd core
go test -cpuprofile=cpu.prof ./internal/brain
go tool pprof cpu.prof
```

---

## 🔍 Debugging

### Logs

```bash
# Daemon log
tail -f ~/.dwyt/dwyt.log

# Service logs
tail -f ~/.dwyt/logs/headroom-stdout.log
tail -f ~/.dwyt/logs/headroom-stderr.log
tail -f ~/.dwyt/logs/codebase-stdout.log
tail -f ~/.dwyt/logs/codebase-stderr.log

# Grep por erros
grep ERROR ~/.dwyt/dwyt.log
grep WARN ~/.dwyt/dwyt.log
```

### State Inspection

```bash
# Runtime state
curl http://127.0.0.1:2737/api/state | jq

# Projects
curl http://127.0.0.1:2737/api/projects | jq

# Current project
curl http://127.0.0.1:2737/api/projects/current | jq

# Brain status
curl http://127.0.0.1:2737/api/brain/status | jq

# Tool details
curl http://127.0.0.1:2737/api/tool-details | jq
```

### Process Inspection

```bash
# Daemon PID
ps aux | grep "dwyt.*daemon"

# Service PIDs
ps aux | grep headroom
ps aux | grep Codebase

# Ports
netstat -tlnp | grep 2737
netstat -tlnp | grep 8787
netstat -tlnp | grep 9749

# File descriptors
lsof -p $(pgrep -f "dwyt.*daemon")
```

---

## ✅ Checklist de Validação Completa

### Funcionalidade Básica
- [ ] Build funciona
- [ ] Instalação funciona
- [ ] Daemon inicia
- [ ] UI abre
- [ ] Setup funciona

### Brain
- [ ] Save funciona
- [ ] Search funciona
- [ ] Summarize funciona
- [ ] Forget funciona
- [ ] Open in Obsidian funciona

### Projects
- [ ] Switch funciona
- [ ] Brain isolado por projeto
- [ ] State persiste
- [ ] Lista de projetos funciona

### Services
- [ ] Headroom inicia
- [ ] Codebase inicia
- [ ] RTK funciona
- [ ] Logs são capturados
- [ ] Stop funciona

### Bugs Corrigidos
- [ ] Sem loop infinito
- [ ] State não corrompe
- [ ] UI atualiza corretamente
- [ ] RTK metrics corretas
- [ ] Headroom única instância

### Performance
- [ ] Health endpoint rápido
- [ ] Status endpoint rápido
- [ ] Brain search rápido
- [ ] Sem memory leaks
- [ ] CPU usage normal

### Testes
- [ ] Testes unitários passam
- [ ] Testes E2E passam
- [ ] Race detector limpo
- [ ] Coverage >60%

---

## 🚨 Troubleshooting

### Daemon não inicia

```bash
# Verificar logs
tail -f ~/.dwyt/dwyt.log

# Verificar porta
netstat -tlnp | grep 2737

# Matar processos antigos
pkill -f "dwyt.*daemon"

# Tentar novamente
dwyt .
```

### UI não abre

```bash
# Verificar daemon
curl http://127.0.0.1:2737/api/health

# Verificar browser
xdg-open http://127.0.0.1:2737

# Verificar firewall
sudo ufw status
```

### Brain não salva

```bash
# Verificar permissões
ls -la ~/.dwyt/projects/

# Verificar espaço em disco
df -h

# Verificar logs
grep "Obsidian save" ~/.dwyt/dwyt.log
```

### Serviços não iniciam

```bash
# Verificar binários
ls -la ~/.dwyt/bin/

# Verificar logs
tail -f ~/.dwyt/logs/headroom-stderr.log
tail -f ~/.dwyt/logs/codebase-stderr.log

# Verificar portas
netstat -tlnp | grep 8787
netstat -tlnp | grep 9749
```

---

## 📞 Suporte

Se encontrar problemas:

1. **Verificar logs:** `~/.dwyt/dwyt.log`
2. **Executar testes:** `go test ./... -v`
3. **Executar E2E:** `./test-e2e.sh`
4. **Verificar state:** `curl http://127.0.0.1:2737/api/state | jq`
5. **Abrir issue:** GitHub com logs e steps to reproduce

---

**Última atualização:** 2026-05-04  
**Versão:** 3.1.0
