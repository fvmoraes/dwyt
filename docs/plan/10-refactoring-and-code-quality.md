# 10 — Refatoração e Qualidade de Código

## Objetivo

Melhorar qualidade interna, organização, manutenção, performance e boas práticas sem alterar o comportamento atual.

---

## Regras de Refatoração

- Meta final: nenhum arquivo deve exceder **250 linhas**
- Essa meta pertence à fase de refatoração; não deve bloquear correções funcionais urgentes de MCP, status, Obsidian ou setup
- Funções devem ter **responsabilidade única**
- Sem `interface{}` — usar `any` ou tipos concretos
- Sem `any` desnecessário no TypeScript — usar tipos explícitos
- Sem manipulação direta de DOM no React
- Sem duplicação de lógica entre handlers

---

## Backend Go

### 10.1 — Dividir `server.go` se > 250 linhas

O arquivo `server.go` já foi parcialmente dividido. Os handlers já estão em arquivos separados
(`handlers_obsidian.go`, `handlers_codebase.go`, etc.). Verificar se `server.go` ainda excede
250 linhas e dividir apenas o que for necessário:

```
core/internal/server/
├── server.go              # setup do Gin, middleware, SPA, Start(), broadcastLoop()
├── handlers_obsidian.go   # /api/obsidian/*
├── handlers_codebase.go   # /api/codebase/* + /api/services/codebase/*
├── handlers_headroom.go   # /api/headroom/* + /api/services/headroom/*
├── handlers_mcp.go        # /api/mcp/*
├── handlers_project.go    # /api/project/* + /api/projects/*
├── handlers_setup.go      # /api/setup/* + /api/install/*
├── handlers_status.go     # /api/status + /api/health + /api/metrics
├── handlers_context.go    # /api/context + /api/tool-details
├── routes.go              # registro de todas as rotas (já existe)
└── types.go               # structs de request/response (já existe)
```

> **Nota:** Os arquivos `routes.go` e `types.go` já existem. Não recriar — apenas complementar.

### 10.2 — Padronizar respostas HTTP

Criar helper para respostas consistentes. Adicionar em `types.go` (já existe):

```go
// Adicionar em core/internal/server/types.go
type APIResponse struct {
    OK      bool        `json:"ok"`
    Data    any         `json:"data,omitempty"`
    Error   string      `json:"error,omitempty"`
    Message string      `json:"message,omitempty"`
}

func respondOK(c *gin.Context, data any) {
    c.JSON(http.StatusOK, APIResponse{OK: true, Data: data})
}

func respondError(c *gin.Context, status int, err error) {
    c.JSON(status, APIResponse{OK: false, Error: err.Error()})
}
```

> **Nota:** Usar `any` em vez de `interface{}` (Go 1.18+). O projeto usa Go 1.25.

### 10.3 — Padronizar tratamento de erros

```go
// Antes (inconsistente):
c.JSON(500, gin.H{"error": err.Error()})
c.JSON(200, gin.H{"status": "error", "message": err.Error()})

// Depois (consistente):
respondError(c, http.StatusInternalServerError, err)
```

### 10.4 — Remover duplicações em handlers

Identificar handlers que fazem a mesma coisa e extrair para funções compartilhadas.

### 10.5 — Melhorar segurança em paths

```go
// Validar paths antes de usar
func sanitizePath(p string) (string, error) {
    clean := filepath.Clean(p)
    if strings.Contains(clean, "..") {
        return "", fmt.Errorf("invalid path: %s", p)
    }
    return clean, nil
}
```

### 10.6 — Substituir `interface{}` por `any`

```bash
# Verificar ocorrências
grep -rn "interface{}" core/internal/
```

---

## Frontend TypeScript/React

### 10.7 — Eliminar `any` desnecessário em `api.ts`

```typescript
// Antes:
export async function getStatus(): Promise<any> { ... }

// Depois:
export interface StatusResponse {
  obsidian: ToolStatus
  codebase: ToolStatus
  headroom: ToolStatus
  rtk: ToolStatus
}
export async function getStatus(): Promise<StatusResponse> { ... }
```

### 10.8 — Extrair sub-componentes de `Dashboard.tsx`

Os sub-componentes já foram extraídos em v4.0.1. Verificar se os arquivos existem e se
`Dashboard.tsx` ainda excede 250 linhas:

```
core/web/src/components/
├── CardCodebase.tsx    # card do Codebase (verificar se existe)
├── CardRTK.tsx         # card do RTK (verificar se existe)
├── CardHeadroom.tsx    # card do Headroom (verificar se existe)
├── CardObsidian.tsx    # card do Obsidian (verificar se existe)
├── CardParts.tsx       # partes reutilizáveis (CardHeader, Row, Hr) — já existe
└── Button.tsx          # botão unificado — já existe
```

> **Nota:** O CHANGELOG v4.0.1 menciona que `CardHeader`, `Row`, `Hr`, `RepoRow` foram
> movidos para nível de módulo. Verificar se os arquivos de card individuais existem ou
> se ainda estão em `Dashboard.tsx`.

### 10.9 — Melhorar hooks de dados

```typescript
// Extrair lógica de polling para hook customizado
function useToolStatus(intervalMs = 5000) {
  const [status, setStatus] = useState<StatusResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const poll = async () => {
      try {
        const data = await getStatus()
        setStatus(data)
        setError(null)
      } catch (e) {
        setError(String(e))
      }
    }
    poll()
    const id = setInterval(poll, intervalMs)
    return () => clearInterval(id)
  }, [intervalMs])

  return { status, error }
}
```

### 10.10 — Melhorar estados de loading/erro/vazio

Cada operação assíncrona deve ter três estados:

```typescript
type AsyncState<T> = 
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'success'; data: T }
  | { status: 'error'; message: string }
```

---

## Verificação de Qualidade

```bash
# Backend
cd core
go build ./...
go vet ./...
go test ./... -race -v
staticcheck ./...  # se disponível

# Frontend
cd core/web
npm run lint
npm run build
npx tsc --noEmit  # verificar tipos sem build

# Verificar tamanho dos arquivos
find core -name "*.go" | xargs wc -l | sort -rn | head -20
find core/web/src -name "*.tsx" -o -name "*.ts" | xargs wc -l | sort -rn | head -20
# Nenhum deve exceder 250 linhas
```

---

## Critérios de Aceite

- [ ] Nenhum arquivo Go novo excede 250 linhas; arquivos legados acima disso têm plano de redução incremental
- [ ] Nenhum arquivo TypeScript/TSX novo excede 250 linhas; arquivos legados acima disso têm plano de redução incremental
- [ ] `go vet ./...` sem warnings
- [ ] `npm run lint` sem erros
- [ ] Sem `interface{}` no Go (usar `any`)
- [ ] Sem `any` desnecessário no TypeScript
- [ ] Respostas HTTP padronizadas
- [ ] Sem manipulação direta de DOM no React
- [ ] Handlers sem duplicação de lógica
