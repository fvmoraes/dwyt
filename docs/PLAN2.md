# PLAN2 — Correções Pós-Auditoria (v4.0.2)

Segunda rodada de correções após validação do usuário sobre os itens resolvidos na v4.0.1.

---

## Achados Reportados pelo Usuário

1. **MCP status offline na UI** — Codebase MCP está online (conectável pelas UIs externas, Open Graph funciona), mas o dashboard DWYT mostra 🔴 offline.
2. **Gitignore incompleto** — `.cursorrules` e `CLAUDE.md` na raiz do projeto não foram para o `.gitignore` automaticamente.
3. **Obsidian sem instalação** — Não há botão para instalar o Obsidian na máquina do usuário. Nem para abrir o diretório do vault separadamente do app Obsidian.
4. **Headroom × Codex OAuth** — `headroom wrap codex` quebra quando o login é via ChatGPT (OAuth), só funciona com API key. Precisa ser não-fatal.
5. **Contexto do vault** — Confirmar que `~/.dwyt/projects/<id>/obsidian/` é sempre segmentado por projeto.

---

## Correções Aplicadas

### 1. MCP status — detecção em dois níveis

**Arquivo:** `core/internal/server/server.go:1678-1700`

O `apiMCPRegistry` só consultava o ProcMan. Se o codebase-memory-mcp foi iniciado externamente ou antes do ProcMan rastreá-lo, o status ficava "offline".

**Solução:** Fallback de health-probe direto na porta do serviço:

```
ProcMan.Status(name) → running+healthy?
  ├─ SIM → "online"
  └─ NÃO → porta > 0 && isPortOpen(port)?
              ├─ SIM → health.ProbeURL(healthURL)?
              │         ├─ SIM → "online"
              │         └─ NÃO → "port_open_no_health" (🟡)
              └─ NÃO → "offline" (🔴)
```

**UI:** Dashboard mostra 🟢 online, 🟡 "Starting...", 🔴 offline com base no campo `status` do registry.

---

### 2. Gitignore — entradas faltantes

**Arquivo:** `core/internal/integrate/integrate.go:25-46`

Adicionados ao mapa de clientes e ao bloco global:

| Entrada | Origem | Cliente |
|---------|--------|---------|
| `CLAUDE.md` | Raiz do projeto | Claude |
| `.cursorrules` | Raiz do projeto | Sempre |
| `.claude/mcp.json` | Pasta `.claude/` | Claude |
| `.vscode/mcp.json` | Pasta `.vscode/` | Sempre |

O mapa `cm` por cliente agora cobre todos os arquivos gerados pelo `integrate.Project()`.

---

### 3. Obsidian — instalação e abertura

**Arquivos:**
- `core/internal/install/install.go:141-244` — `InstallObsidianApp()`
- `core/internal/server/server.go:1432-1480` — handlers `apiObsidianOpenDir`, `apiObsidianInstall`, `apiObsidianInstallStatus`
- `core/web/src/pages/Dashboard.tsx` — 3 botões no card Obsidian
- `core/web/src/api.ts` — `openBrainDir()`, `installObsidian()`, `getObsidianInstallStatus()`
- `core/web/src/i18n.ts` — chaves `openVaultDir`, `installObsidian`

**Novos endpoints:**
| Método | Rota | Ação |
|--------|------|------|
| POST | `/api/obsidian/open-dir` | Abre diretório do vault no file manager |
| POST | `/api/obsidian/install` | Baixa e instala Obsidian (bg) |
| GET | `/api/obsidian/install-status` | Status da instalação |

**Install Obsidian por OS:**
- **Linux:** baixa AppImage do GitHub Releases → `~/.local/bin/Obsidian.AppImage` + symlink `obsidian`
- **macOS:** verifica `/Applications/Obsidian.app` (instalação manual)
- **Windows:** verifica `%LOCALAPPDATA%/obsidian/Obsidian.exe` (instalação manual)

**Botões no card:**
- **"Open Vault"** → abre no app Obsidian (`obsidian://open?path=`)
- **"Open Dir"** → abre diretório no file manager (`xdg-open`/`open`/`explorer`)

**Instalação do Obsidian** — ocorre SOMENTE na tela de Setup (SetupWizard), nunca no Dashboard. Durante o fluxo de instalação (`apiSetupInstall`), se a tool `obsidian` estiver selecionada e o binário do Obsidian não for encontrado, o instalador é disparado automaticamente como parte do pipeline de `apiSetupInstall` (junto com codebase, rtk, headroom). O endpoint `/api/obsidian/install` existe para re-instalação manual, mas o botão **não aparece no Dashboard** — apenas o `apiObsidianInstallStatus` pode ser consultado para diagnóstico.

---

### 4. Headroom wrap Codex — falha não-fatal

Já estava implementado: `server.go:1568-1577` captura erro do `headroom wrap` e loga como `WARN` sem crash. O `unwrap` no botão Stop reverte a injeção.

**Nenhuma alteração necessária** — o comportamento de fallback já existia.

---

### 5. Contexto do vault por projeto

Confirmado: `brain.NewProjectObsidian(dwytHome, projectPath)` em `brain.go:64` cria vaults em:
```
~/.dwyt/projects/<sha256(projectPath)[:12]>/obsidian/
```

Cada projeto tem seu próprio diretório isolado com `index.md`, `context.md`, `decisions.md`, `tasks.md`, `knowledge/`, `logs/`. O `apiProjectSwitch` recarrega o `ProjectObsidian` automaticamente.

**Nenhuma alteração necessária** — o isolamento já funcionava.

---

## Validação Executada

```
go build ./...     ✅
go vet ./...       ✅
go test ./...      ✅ (17 testes, 22 pacotes)
npm run lint       ✅ (0 erros, 0 warnings)
npm run build      ✅
```

---

## Impacto por Arquivo

| Arquivo | Mudança |
|---------|---------|
| `server.go` | MCP status fallback, 3 novos handlers obsidian |
| `install.go` | `InstallObsidianApp()` + installers por OS |
| `integrate.go` | Gitignore entries: CLAUDE.md, .cursorrules, .vscode/mcp.json |
| `Dashboard.tsx` | Open Dir, Install Obsidian, MCP status granular |
| `api.ts` | openBrainDir, installObsidian, getObsidianInstallStatus |
| `i18n.ts` | openVaultDir, installObsidian (EN + PT-BR) |
| `CHANGELOG.md` | Entrada v4.0.2 |
| `HOW-IT-WORKS.md` | Novos endpoints obsidian, MCP status detection |

---

## Resumo Final

| Item | Status |
|------|--------|
| MCP online na UI | ✅ Two-tier detection |
| Gitignore automático | ✅ 4 novas entradas |
| Install Obsidian | ✅ Linux AppImage + macOS/Windows detect |
| Open Dir / Open Vault | ✅ Botões separados |
| Headroom Codex seguro | ✅ Já era não-fatal |
| Vault por projeto | ✅ Já funcionava |
| Documentação | ✅ CHANGELOG + HOW-IT-WORKS |
