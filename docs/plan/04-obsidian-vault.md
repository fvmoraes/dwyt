# 04 — Obsidian como Memória Obrigatória

## Fase 3 — Fortalecer Obsidian como Memória Persistente

**Objetivo:** Garantir que o Obsidian seja a memória persistente, isolada e confiável do projeto.

---

## Estrutura do Vault

```
~/.dwyt/projects/<sha256(projectPath)[:12]>/obsidian/
├── index.md        # índice do projeto com visão geral da estrutura
├── context.md      # resumo completo (auto-reconstruído de todos os arquivos)
├── decisions.md    # decisões de arquitetura (log append-only)
├── tasks.md        # tarefas ativas (checklist append-only)
├── knowledge/      # artigos da base de conhecimento (arquivos com timestamp)
└── logs/           # sessões, erros, comandos
```

### Formato dos Arquivos (Frontmatter YAML)

```markdown
---
tags: [dwyt, decision, architecture]
date: 2026-05-04T15:30:00Z
type: decision
---

# Título da decisão

Conteúdo...
```

---

## Tarefas

### 3.1 — Revisar `brain.NewProjectObsidian()`

Confirmar caminho:

```
~/.dwyt/projects/<sha256(projectPath)[:12]>/obsidian/
```

O `projectPath` pode estar em qualquer diretório do usuário. A validação de segurança deve garantir que o **vault gerado** fica dentro de `dwytHome/projects/<id>/obsidian`, não rejeitar o projeto por estar fora de `~/.dwyt/`.

Correção obrigatória:

```go
// Incorreto: validar projectPath contra dwytHome.
safePath(dwytHome, projectPath)

// Correto: resolver projectPath para hash/metadata e validar apenas baseDir/brainDir.
id := db.HashPath(projectPath)
brainDir := filepath.Join(dwytHome, "projects", id, "obsidian")
safePath(dwytHome, brainDir)
```

### 3.2 — Garantir criação inicial dos arquivos

Na primeira vez que um vault é criado:

```
index.md
context.md
decisions.md
tasks.md
knowledge/
logs/
```

### 3.3 — Garantir carregamento do `ProjectObsidian`

O vault deve ser carregado:

- No startup do daemon
- Ao rodar `dwyt .`
- Ao trocar projeto via API (`POST /api/project/switch`)
- Ao finalizar setup

### 3.4 — Garantir todos os endpoints

| Método | Rota | Propósito |
|--------|------|-----------|
| GET | `/api/obsidian/status` | Stats (contagem de arquivos, tipos, última atualização) |
| GET | `/api/obsidian/search?q=` | Busca full-text em todos os arquivos .md |
| POST | `/api/obsidian/save` | Salvar entrada `{"type":"decision","content":"..."}` |
| POST | `/api/obsidian/summarize` | Reconstruir context.md de todos os arquivos |
| POST | `/api/obsidian/open` | Abrir vault no app Obsidian (`obsidian://open?path=` ou URI equivalente suportada) |
| POST | `/api/obsidian/open-dir` | Abrir diretório do vault no file manager |
| POST | `/api/obsidian/install` | Baixar e instalar o app Obsidian (Linux: AppImage) |
| GET | `/api/obsidian/install-status` | Progresso da instalação do Obsidian |

### 3.5 — Separar ações de abertura

```
Open Vault = abrir app Obsidian (obsidian://open?path=... ou URI equivalente suportada)
Open Dir   = abrir diretório no file manager (xdg-open / open / explorer)
```

### 3.6 — Instalação do Obsidian no SetupWizard (não no Dashboard)

A instalação do app Obsidian deve ocorrer **exclusivamente no SetupWizard**.

**O botão "Install Obsidian" deve ser removido do Dashboard** (ver imagem de referência no arquivo 00, R10). O Dashboard é para operar ferramentas já instaladas.

No Dashboard, se o app Obsidian não estiver instalado, exibir apenas um aviso:

```
⚠ Obsidian app not installed — go to Setup to install
```

Com link para `/#/setup`, não um botão de instalação.

Detecção por plataforma (no SetupWizard):

| Plataforma | Local |
|------------|-------|
| Linux | `~/.local/bin/Obsidian.AppImage` + symlink `obsidian` |
| macOS | `/Applications/Obsidian.app` |
| Windows | `%LOCALAPPDATA%/obsidian/Obsidian.exe` |

### 3.7 — Proteção explícita contra deleção de vaults

Adicionar verificação em todos os comandos de limpeza:

```go
// NUNCA deletar vaults — verificar antes de qualquer os.RemoveAll
var protectedPathSegments = []string{
    "/projects/",
    "/obsidian/",
}

func isProtectedPath(path string) bool {
    for _, seg := range protectedPathSegments {
        if strings.Contains(filepath.ToSlash(path), seg) {
            return true
        }
    }
    return false
}
```

> **Nota:** A verificação `strings.Contains(path, "projects")` do rascunho original é frágil —
> um projeto chamado "my-projects-app" passaria no filtro. Usar segmentos de path com barras.

### 3.8 — Roteamento de SaveEntry por tipo

| Tipo | Destino |
|------|---------|
| `decision` | Append em `decisions.md` |
| `task` | Append em `tasks.md` |
| `error`, `command`, `session` | Novo arquivo em `logs/` |
| `note` | Novo arquivo em `knowledge/` |

---

## Critérios de Aceite

- [ ] `save`, `search`, `summarize`, `open` e `open-dir` funcionam com o projeto ativo
- [ ] Trocar de projeto troca também o vault carregado
- [ ] Uninstall/reinstall/clean não remove vaults
- [ ] Dashboard não mostra "vault active" se não houver vault carregado
- [ ] `Open Vault` e `Open Dir` são botões separados com ações distintas
- [ ] **Botão "Install Obsidian" não existe no Dashboard** — apenas no SetupWizard
- [ ] Dashboard exibe aviso com link para Setup quando app Obsidian não está instalado
- [ ] Projetos fora de `~/.dwyt/` carregam vault normalmente em `~/.dwyt/projects/<sha12>/obsidian/`

---

## Verificação

```bash
# Verificar vault criado
ls -la ~/.dwyt/projects/

# Testar save
curl -s -X POST http://localhost:2737/api/obsidian/save \
  -H "Content-Type: application/json" \
  -d '{"type":"decision","content":"Teste de save via API"}' | jq .

# Testar search
curl -s "http://localhost:2737/api/obsidian/search?q=teste" | jq .

# Testar status
curl -s http://localhost:2737/api/obsidian/status | jq .

# Verificar que vault não foi apagado após restart
dwyt stop && dwyt .
ls -la ~/.dwyt/projects/
```
