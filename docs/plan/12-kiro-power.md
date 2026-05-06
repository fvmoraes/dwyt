# 12 — Kiro Power Local

## Objetivo

Gerar e manter automaticamente um **Kiro Power local** em `~/.dwyt/powers/dwyt-power` sempre que o usuário rodar `dwyt .` com Kiro habilitado na configuração.

O Power expõe as quatro frentes do DWYT ao Kiro por steering files persistentes e ativação automática por palavras-chave. Apenas `codebase` e `obsidian` são MCPs reais; RTK e Headroom entram como instruções operacionais/API, porque RTK é CLI e Headroom é proxy/serviço.

---

## Estrutura do Power

```
~/.dwyt/powers/dwyt-power/
├── POWER.md              # documentação e instruções do Power para o Kiro
├── mcp.json              # MCPs reais: codebase e obsidian
└── steering/
    ├── dwyt-context.md   # regras de contexto e prioridade
    ├── obsidian.md       # instruções de uso do vault
    ├── codebase.md       # instruções de uso do Codebase MCP
    ├── rtk.md            # instruções de uso do RTK
    └── headroom.md       # instruções de uso do Headroom
```

---

## Integração com o Kiro

O Kiro carrega Powers locais a partir de `~/.kiro/powers/`. O DWYT deve criar um symlink
nesse diretório apontando para o Power:

```
~/.kiro/powers/dwyt-power → ~/.dwyt/powers/dwyt-power
```

Se o diretório `~/.kiro/powers/` não existir, o DWYT deve criá-lo.

> **Nota sobre o schema do Kiro Power:** O Kiro espera que o `POWER.md` contenha uma seção
> `## Keywords` com palavras-chave separadas por vírgula. O `mcp.json` deve seguir o schema
> padrão de MCPs do Kiro (sem campo `"description"` no nível do servidor — esse campo não
> é parte do schema oficial). Os steering files devem ter frontmatter com `inclusion: auto`
> ou `inclusion: manual`.

---

## Arquivos a Implementar

### Novo pacote: `core/internal/kiropow/`

```
core/internal/kiropow/
├── kiropow.go       # lógica principal de geração e registro do Power
└── kiropow_test.go  # testes unitários
```

---

## Especificação de `kiropow.go`

### Tipos

```go
package kiropow

// PowerStatus descreve o estado atual do Kiro Power.
type PowerStatus struct {
    Installed   bool              `json:"installed"`
    PowerDir    string            `json:"power_dir"`
    KiroLink    string            `json:"kiro_link"`
    MCPs        map[string]bool   `json:"mcps"`        // nome → binário existe
    UpdatedAt   string            `json:"updated_at"`
    Errors      []string          `json:"errors,omitempty"`
}
```

### Função principal

```go
// EnsurePower cria ou atualiza o Kiro Power local.
// É idempotente: pode ser chamada múltiplas vezes sem efeitos colaterais.
// Retorna o status do Power após a operação.
func EnsurePower(dwytHome, dwytBin, projectPath string) (*PowerStatus, error)
```

### Funções auxiliares

```go
// IsKiroEnabled verifica se Kiro está habilitado na configuração do DWYT.
func IsKiroEnabled(setupConfig map[string]interface{}) bool

// ValidateMCPBinaries verifica quais binários de MCP existem em dwytBin.
// Retorna mapa nome → existe. Usar apenas as chaves "codebase" e "obsidian".
func ValidateMCPBinaries(dwytBin string) map[string]bool

// GeneratePowerMD gera o conteúdo do POWER.md com os MCPs disponíveis.
func GeneratePowerMD(dwytBin, projectPath string, mcps map[string]bool) string

// GenerateMCPJSON gera o conteúdo do mcp.json com os MCPs disponíveis.
// Inclui apenas os MCPs reais cujos binários existem.
func GenerateMCPJSON(dwytBin string, mcps map[string]bool) (string, error)

// GenerateSteeringFiles gera os arquivos de steering do Power.
func GenerateSteeringFiles(powerDir, projectPath string) error

// RegisterWithKiro cria o symlink ~/.kiro/powers/dwyt-power → powerDir.
// Se o symlink já existe e aponta para o mesmo destino, não faz nada.
func RegisterWithKiro(powerDir string) error

// NeedsUpdate verifica se o Power precisa ser atualizado
// (binários mudaram, paths mudaram, arquivos ausentes).
func NeedsUpdate(powerDir, dwytBin string) bool
```

---

## Conteúdo dos Arquivos Gerados

### `POWER.md`

```markdown
# DWYT Power

DWYT (Don't Waste Your Tokens) is a local orchestrator that reduces AI token
consumption by managing four tools: Obsidian (memory), Codebase (code graph),
RTK (terminal compression), and Headroom (API proxy).

## Keywords

dwyt, codebase, obsidian, rtk, headroom, mcp, memory, tokens, compression,
vault, knowledge, graph, proxy, context, economia de tokens, memória

## Tools

### Obsidian — Project Memory (ALWAYS FIRST)
Persistent markdown vault per project. Search before reading files.
- Search: GET http://localhost:2737/api/obsidian/search?q=<query>
- Save:   POST http://localhost:2737/api/obsidian/save
- Types:  decision, task, note, error, command, session

### Codebase — Code Knowledge Graph (ON DEMAND)
Structural exploration of the codebase. Use ONLY for architecture questions.
- MCP tools: search_graph, trace_call_path, get_code_snippet
- Start: POST http://localhost:2737/api/services/codebase/start

### RTK — Terminal Compression (ALWAYS)
Prefix ALL shell commands with rtk to reduce output 60-98%.
- Usage: rtk git status, rtk go test ./...

### Headroom — API Proxy (AUTOMATIC)
Compresses API calls ~34%. Auto-detected via env vars.
- Active when: OPENAI_BASE_URL or ANTHROPIC_BASE_URL point to 127.0.0.1:8787

## Priority Order
1. Obsidian FIRST — check vault before any file read
2. Headroom — auto via env vars
3. RTK — prefix all shell commands
4. Codebase — structural exploration only
```

### `mcp.json`

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

> **Nota:** Os nomes `codebase` e `obsidian` devem ser os mesmos usados no registry, Dashboard e arquivos de projeto (`.kiro/mcp.json`, `.mcp.json`, etc.). Apenas MCPs cujos binários existem em `~/.dwyt/bin/` são incluídos no arquivo gerado.
> RTK e Headroom não entram em `mcp.json`; eles ficam nos steering files `rtk.md` e `headroom.md`.

### `steering/dwyt-context.md`

```markdown
---
inclusion: auto
---

# DWYT Context Rules

## Priority Order (ALWAYS follow this order):

1. **Obsidian FIRST** — before reading any file, search the project vault:
   GET http://localhost:2737/api/obsidian/search?q=<your query>

2. **Headroom** — auto-detected via OPENAI_BASE_URL / ANTHROPIC_BASE_URL.
   If set, use them. No manual config needed.

3. **RTK** — prefix ALL shell commands with `rtk`:
   rtk git status, rtk go test ./..., rtk npm run build

4. **Codebase MCP** — ONLY for structural code exploration.
   Use search_graph, trace_call_path, get_code_snippet.

## After completing important work:
Save decisions to Obsidian:
POST http://localhost:2737/api/obsidian/save
{"type": "decision", "content": "..."}
```

### `steering/obsidian.md`

```markdown
---
inclusion: auto
---

# Obsidian — Project Memory

The project vault is at: ~/.dwyt/projects/<id>/obsidian/

## API
- Search: GET http://localhost:2737/api/obsidian/search?q=<query>
- Save:   POST http://localhost:2737/api/obsidian/save
- Status: GET http://localhost:2737/api/obsidian/status

## Entry Types
| Type      | Destination     |
|-----------|-----------------|
| decision  | decisions.md    |
| task      | tasks.md        |
| note      | knowledge/      |
| error     | logs/           |
| command   | logs/           |
| session   | logs/           |

## Rules
- ALWAYS search Obsidian before reading project files
- ALWAYS save important decisions after completing work
- NEVER delete vault files — they are persistent project memory
```

### `steering/codebase.md`

```markdown
---
inclusion: manual
---

# Codebase — Code Knowledge Graph

Use ONLY when you need to understand code structure.
Prefer Obsidian context first.

## MCP Tools
- search_graph: find nodes by name/type
- trace_call_path: trace function call chains
- get_code_snippet: get code at a specific location

## API
- Start: POST http://localhost:2737/api/services/codebase/start
- Index: POST http://localhost:2737/api/codebase/index
- Status: GET http://localhost:2737/api/services/codebase/status
```

### `steering/rtk.md`

```markdown
---
inclusion: auto
---

# RTK — Terminal Compression

RTK is a CLI tool. Prefix ALL shell commands with `rtk`.

## Usage
rtk git status
rtk go test ./...
rtk npm run build
rtk cargo test

## Why
Reduces terminal output 60-98% before it enters context.
Saves tokens on every shell command.

## Metrics
GET http://localhost:2737/api/rtk/gain
```

### `steering/headroom.md`

```markdown
---
inclusion: auto
---

# Headroom — API Proxy

Headroom compresses AI API calls ~34%. It is automatic.

## Detection
If OPENAI_BASE_URL or ANTHROPIC_BASE_URL point to 127.0.0.1:8787,
Headroom is active. No manual configuration needed.

## Status
GET http://localhost:2737/api/services/headroom/status
```

---

## Integração no Fluxo `dwyt .`

### Onde chamar `EnsurePower`

Em `core/cmd/dwyt/cli/root/root.go`, função `runDefault()`, após `waitForDaemon()` retornar `true`.
O daemon já está rodando nesse ponto, então a chamada é segura:

```go
// Após waitForDaemon() retornar true:
if kiropow.IsKiroEnabledFromDB(e.DwytHome) {
    if st, err := kiropow.EnsurePower(e.DwytHome, e.DwytBin, projectPath); err != nil {
        log.Warn("kiro power setup failed", log.Fields{"error": err.Error()})
        fmt.Printf("  ⚠  Kiro Power: %v\n", err)
    } else {
        fmt.Printf("  ✓  Kiro Power → %s\n", st.PowerDir)
        for name, ok := range st.MCPs {
            if ok {
                fmt.Printf("     MCP %-20s ✓\n", name)
            } else {
                fmt.Printf("     MCP %-20s ✗ (binary not found)\n", name)
            }
        }
    }
}
```

> **Nota:** A função `IsKiroEnabledFromDB` lê o SQLite diretamente. Não abrir uma segunda
> conexão com o banco se o daemon já está rodando — usar o endpoint da API em vez disso,
> ou passar a config como parâmetro. Alternativa mais simples: chamar via API após o daemon subir:
>
> ```go
> // Alternativa via API (evita dupla conexão SQLite):
> resp, _ := http.Get("http://localhost:2737/api/setup/load")
> // verificar se "kiro" está em cfg.Ias
> ```

### Onde chamar no daemon (server.go)

Em `server.New()`, após carregar a configuração do setup. Usar goroutine para não bloquear:

```go
// Se Kiro está habilitado, garantir Power atualizado
clients := strings.Join(cfg.Ias, ",")
if strings.Contains(clients, "kiro") {
    go func() {
        if _, err := kiropow.EnsurePower(dwytHome, dwytBin, project); err != nil {
            log.Warn("kiro power ensure failed", log.Fields{"error": err.Error()})
        }
    }()
}
```

---

## Endpoint de Status na API

Adicionar em `core/internal/server/handlers_mcp.go` (ou novo `handlers_kiro.go`).
Registrar a rota em `routes.go`:

```go
// Em routes.go, adicionar dentro do grupo /api:
api.GET("/kiro/power/status", ds.apiKiroPowerStatus)
api.POST("/kiro/power/refresh", ds.apiKiroPowerRefresh)
```

Resposta do `GET /api/kiro/power/status`:

```json
{
  "installed": true,
  "power_dir": "/home/user/.dwyt/powers/dwyt-power",
  "kiro_link": "/home/user/.kiro/powers/dwyt-power",
  "mcps": {
    "codebase": true,
    "obsidian": true
  },
  "updated_at": "2026-05-05T10:00:00Z",
  "errors": []
}
```

> **Nota:** O checklist do frontend menciona `POST /api/kiro/power/refresh`. Registrar
> essa rota também em `routes.go`.

---

## Regras de Segurança

### Proteção de dados persistentes

```go
// NUNCA deletar estes segmentos de path
var protectedPathSegments = []string{
    "/projects/",
    "/obsidian/",
    "/obsidian-vault/",
}

func isSafeToDelete(path string) bool {
    normalized := filepath.ToSlash(path)
    for _, seg := range protectedPathSegments {
        if strings.Contains(normalized, seg) {
            return false
        }
    }
    return true
}
```

> **Nota:** Usar segmentos com barras para evitar falsos positivos em nomes de projetos
> que contenham as palavras "projects" ou "obsidian".

### Idempotência

- Se `POWER.md` já existe e o conteúdo é idêntico → não reescrever
- Se symlink já existe e aponta para o destino correto → não recriar
- Se `mcp.json` já existe com os mesmos binários → não reescrever
- Comparar hash do conteúdo antes de sobrescrever qualquer arquivo

### Reversibilidade

- O Power pode ser removido sem afetar dados do projeto
- Remover o symlink `~/.kiro/powers/dwyt-power` desregistra o Power
- Os arquivos em `~/.dwyt/powers/dwyt-power/` são regeneráveis

---

## Testes Obrigatórios

Arquivo: `core/internal/kiropow/kiropow_test.go`

### Casos de teste

```go
// T01 — Primeira execução: Power criado do zero
func TestEnsurePower_FirstRun(t *testing.T)

// T02 — Execução repetida: idempotente, sem reescrita desnecessária
func TestEnsurePower_Idempotent(t *testing.T)

// T03 — Kiro habilitado: Power gerado
func TestEnsurePower_KiroEnabled(t *testing.T)

// T04 — Kiro não habilitado: Power não gerado
func TestEnsurePower_KiroDisabled(t *testing.T)

// T05 — MCP ausente: incluído no status como false, não causa erro fatal
func TestEnsurePower_MissingMCP(t *testing.T)

// T06 — Projeto novo: vault path correto no steering
func TestEnsurePower_NewProject(t *testing.T)

// T07 — Projeto já existente: arquivos existentes não sobrescritos se iguais
func TestEnsurePower_ExistingProject(t *testing.T)

// T08 — Proteção de dados: uninstall não remove ~/.dwyt/projects
func TestEnsurePower_VaultProtection(t *testing.T)

// T09 — Symlink Kiro: criado corretamente
func TestRegisterWithKiro_CreatesSymlink(t *testing.T)

// T10 — Symlink Kiro: não recriado se já correto
func TestRegisterWithKiro_Idempotent(t *testing.T)

// T11 — NeedsUpdate: detecta binário mudado
func TestNeedsUpdate_BinaryChanged(t *testing.T)

// T12 — NeedsUpdate: detecta arquivo ausente
func TestNeedsUpdate_MissingFile(t *testing.T)

// T13 — GenerateMCPJSON: apenas MCPs com binários existentes
func TestGenerateMCPJSON_OnlyExistingBinaries(t *testing.T)

// T14 — ValidateMCPBinaries: retorna mapa correto
func TestValidateMCPBinaries(t *testing.T)
```

---

## Checklist de Implementação

### Backend Go

- [ ] Criar `core/internal/kiropow/kiropow.go`
- [ ] Implementar `EnsurePower()`
- [ ] Implementar `ValidateMCPBinaries()`
- [ ] Implementar `GeneratePowerMD()`
- [ ] Implementar `GenerateMCPJSON()`
- [ ] Implementar `GenerateSteeringFiles()`
- [ ] Implementar `RegisterWithKiro()`
- [ ] Implementar `NeedsUpdate()`
- [ ] Criar `core/internal/kiropow/kiropow_test.go` com 14 testes
- [ ] Integrar chamada em `root.go` → `runDefault()`
- [ ] Integrar chamada em `server.go` → `New()`
- [ ] Adicionar endpoint `GET /api/kiro/power/status`
- [ ] Registrar rota em `routes.go`

### Frontend

- [ ] Adicionar card ou seção "Kiro Power" no Dashboard
- [ ] Exibir status: instalado/não instalado, MCPs ativos, erros
- [ ] Botão "Refresh Power" → chama `POST /api/kiro/power/refresh`
- [ ] Adicionar chaves i18n: `kiroPower`, `kiroPowerInstalled`, `kiroPowerMCPs`

### Segurança

- [ ] Verificar que `EnsurePower` nunca deleta `~/.dwyt/projects/`
- [ ] Verificar que `EnsurePower` nunca deleta `~/.dwyt/projects/*/obsidian/`
- [ ] Verificar idempotência em execuções repetidas
- [ ] Verificar que MCP ausente não causa erro fatal

---

## Verificação

```bash
# 1. Rodar dwyt . com Kiro habilitado
dwyt .

# 2. Verificar Power criado
ls -la ~/.dwyt/powers/dwyt-power/
cat ~/.dwyt/powers/dwyt-power/POWER.md
cat ~/.dwyt/powers/dwyt-power/mcp.json
ls -la ~/.dwyt/powers/dwyt-power/steering/

# 3. Verificar symlink no Kiro
ls -la ~/.kiro/powers/dwyt-power
readlink ~/.kiro/powers/dwyt-power
# deve apontar para ~/.dwyt/powers/dwyt-power

# 4. Verificar status via API
curl -s http://localhost:2737/api/kiro/power/status | jq .

# 5. Rodar novamente (idempotência)
dwyt .
# Nenhum arquivo deve ser reescrito se não houve mudança

# 6. Verificar proteção de dados
ls -la ~/.dwyt/projects/
# deve estar intacto

# 7. Rodar testes
cd core && go test ./internal/kiropow/... -v -race

# 8. Build completo
cd core && go build ./...
cd core/web && npm run lint && npm run build
```

---

## Notas de Implementação

### Detecção de Kiro habilitado

Preferir leitura via API quando o daemon já está rodando. Para uso em `root.go` (antes do daemon),
ler o SQLite diretamente — mas garantir que a conexão seja fechada imediatamente:

```go
// IsKiroEnabledFromDB lê o SQLite diretamente (usar apenas antes do daemon subir).
// Após o daemon subir, preferir GET /api/setup/load.
func IsKiroEnabledFromDB(dwytHome string) bool {
    store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
    if err != nil {
        return false
    }
    defer store.Close()
    raw, err := store.GetConfig("setup")
    if err != nil {
        return false
    }
    var cfg map[string]any
    if json.Unmarshal([]byte(raw), &cfg) != nil {
        return false
    }
    // Verificar campo "ias" (nome atual no Config struct)
    ias, _ := cfg["ias"].([]any)
    for _, ia := range ias {
        if s, ok := ia.(string); ok && s == "kiro" {
            return true
        }
    }
    // Fallback: verificar campo legado "clients"
    clients, _ := cfg["clients"].([]any)
    for _, c := range clients {
        if s, ok := c.(string); ok && s == "kiro" {
            return true
        }
    }
    return false
}
```

> **Nota:** O `Config` struct em `types.go` tem tanto `Ias` quanto `Clients` (campo legado).
> Verificar ambos para compatibilidade.

### Hash de conteúdo para idempotência

```go
func contentHash(content string) string {
    h := sha256.Sum256([]byte(content))
    return hex.EncodeToString(h[:8])
}

func writeIfChanged(path, content string) (bool, error) {
    if existing, err := os.ReadFile(path); err == nil {
        if contentHash(string(existing)) == contentHash(content) {
            return false, nil // sem mudança
        }
    }
    os.MkdirAll(filepath.Dir(path), 0755)
    return true, os.WriteFile(path, []byte(content), 0644)
}
```

### Symlink seguro

```go
func safeSymlink(target, link string) error {
    // Verificar se já existe e aponta para o destino correto
    if existing, err := os.Readlink(link); err == nil {
        if existing == target {
            return nil // já correto
        }
        os.Remove(link) // remover symlink desatualizado
    }
    os.MkdirAll(filepath.Dir(link), 0755)
    return os.Symlink(target, link)
}
```
