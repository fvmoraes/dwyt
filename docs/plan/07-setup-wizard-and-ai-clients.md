# 07 — SetupWizard e Geração de Arquivos para Clientes de IA

## Fase 8 — SetupWizard como Centro de Configuração

**Objetivo:** Fazer o setup configurar o produto de forma completa e previsível.

---

## Fluxo Esperado do Setup

```
Usuário abre /#/setup
  ├─ escolhe projeto
  ├─ escolhe tools: Codebase, RTK, Headroom, Obsidian
  ├─ escolhe IAs: Claude, Codex, Copilot, Kiro, Cursor, OpenCode
  └─ clica Install
       ├─ instala ferramentas em ~/.dwyt/
       ├─ cria/carrega vault do projeto
       ├─ gera configs das IAs no projeto
       ├─ atualiza .gitignore
       ├─ configura MCPs
       └─ faz indexação inicial quando aplicável
```

---

## Tarefas

### 8.1 — Obsidian obrigatório ou fortemente recomendado

Obsidian deve ser pré-selecionado e não pode ser desmarcado sem aviso.

### 8.2 — Instalar `dwyt-obsidian-mcp` com Obsidian

Quando Obsidian estiver selecionado, instalar o binário em `~/.dwyt/bin/dwyt-obsidian-mcp`.

### 8.3 — Detectar/instalar app Obsidian por plataforma

| Plataforma | Detecção | Instalação |
|------------|----------|------------|
| Linux | `which obsidian` ou `~/.local/bin/Obsidian.AppImage` | Download AppImage + symlink |
| macOS | `/Applications/Obsidian.app` | Mostrar link de download |
| Windows | `%LOCALAPPDATA%/obsidian/Obsidian.exe` | Mostrar link de download |

### 8.4 — Progresso real da instalação

O SetupWizard deve mostrar progresso real:

```
[1/5] Instalando RTK...        ✅
[2/5] Instalando Headroom...   ✅
[3/5] Instalando Codebase...   ✅
[4/5] Criando vault Obsidian... ✅
[5/5] Gerando configs de IA... ✅
```

Endpoint: `GET /api/install/status` com polling a cada 500ms.

### 8.5 — Não deixar instalação presa

Timeout de 5 minutos por etapa. Se uma etapa falhar, mostrar erro e permitir continuar.

### 8.6 — Abrir Dashboard com status real ao final

Após setup completo, redirecionar para `/#/dashboard` com status atualizado.

### 8.7 — Não apagar dados existentes

Se vault já existe, não recriar. Se config já existe, não sobrescrever sem confirmação.

---

## Fase 9 — Geração de Arquivos para Clientes de IA

**Objetivo:** Garantir que cada cliente receba apenas os arquivos necessários, no local correto.

---

## Mapeamento de Arquivos por Cliente

| Cliente | Arquivos | Local |
|---------|----------|-------|
| Claude Code | `CLAUDE.md`, `.claude/mcp.json` | raiz + `.claude/` |
| Codex | `AGENTS.md`, `.mcp.json` | raiz |
| GitHub Copilot | `.github/copilot-instructions.md` | `.github/` |
| Kiro | `.kiro/steering/dwyt.md`, `.kiro/mcp.json` | `.kiro/` |
| Cursor | `.cursor/rules/dwyt.mdc` | `.cursor/rules/` |
| OpenCode | `opencode.json`, `AGENTS.md`, `.mcp.json` | raiz |

---

## Tarefas

### 9.1 — Revisar `integrate.go`

Garantir templates atualizados com `/api/obsidian/*`, não `/api/brain/*`.

### 9.2 — Garantir MCPs `codebase` e `obsidian` em todos os templates

Nenhum template deve gerar `dwyt` (genérico), `dwyt-codebase`, `dwyt-obsidian` ou `obsidian-mcp` como chave de MCP. Os binários continuam sendo `codebase-memory-mcp` e `dwyt-obsidian-mcp`.

Arquivos existentes devem ser tratados como migração:

- Se o arquivo não existe, criar com o template atual.
- Se o arquivo existe e tem nomes legados ou falta `obsidian`, atualizar de forma controlada.
- Se o arquivo existe e já está correto, preservar conteúdo do usuário.

### 9.3 — Instruções obrigatórias em todos os arquivos gerados

```
1. Consultar Obsidian antes de operar
   GET http://localhost:2737/api/obsidian/search?q=<query>

2. Usar Headroom quando base URL estiver configurada
   (auto-detectado via OPENAI_BASE_URL / ANTHROPIC_BASE_URL)

3. Prefixar comandos shell com rtk quando útil
   rtk git status

4. Usar Codebase MCP apenas para exploração estrutural
```

### 9.4 — Atualizar `.gitignore`

Entradas mínimas a adicionar quando os arquivos são gerados:

```gitignore
# dwyt — generated files (do not commit)
CLAUDE.md
.cursorrules
.mcp.json
.claude/mcp.json
.kiro/mcp.json
.vscode/mcp.json
opencode.json
```

Entradas que **não** devem ir para `.gitignore` quando o objetivo for compartilhar instruções da equipe:

```
AGENTS.md
.kiro/steering/dwyt.md
.github/copilot-instructions.md
.cursor/rules/dwyt.mdc
```

> **Nota:** projetos já existentes podem ter `AGENTS.md` ignorado por versões anteriores do DWYT. A migração deve remover essa entrada apenas com cuidado, sem apagar o arquivo e sem sobrescrever decisões explícitas do usuário.

---

## Critérios de Aceite

- [ ] Setup instala ou detecta todas as ferramentas selecionadas
- [ ] Setup não apaga dados existentes
- [ ] Setup gera arquivos esperados para as IAs escolhidas
- [ ] Usuário termina o setup vendo cards coerentes
- [ ] Nenhum arquivo é criado em local errado
- [ ] Nenhum cliente recebe config incompleta
- [ ] `.gitignore` fica coerente com os arquivos gerados
- [ ] Templates usam `/api/obsidian/*` (não `/api/brain/*`)

---

## Verificação

```bash
# Após setup completo, verificar arquivos gerados
ls -la AGENTS.md CLAUDE.md .mcp.json opencode.json
ls -la .claude/mcp.json .kiro/mcp.json .vscode/mcp.json
ls -la .cursor/rules/dwyt.mdc
ls -la .github/copilot-instructions.md

# Verificar conteúdo dos MCPs
cat .mcp.json | jq '.mcpServers | keys'
# deve retornar: ["codebase", "obsidian"]

# Verificar .gitignore
grep -E "CLAUDE\.md|\.cursorrules|\.mcp\.json|\.claude/mcp\.json|\.kiro/mcp\.json|\.vscode/mcp\.json|opencode\.json" .gitignore

# Verificar rotas nos templates
grep -r "api/brain" AGENTS.md CLAUDE.md .kiro/steering/dwyt.md
# deve retornar vazio
```
