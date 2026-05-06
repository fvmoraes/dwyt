# 11 — Validação Final e Documentação

## Fase 10 — Testes, Validação e Documentação

**Objetivo:** Garantir que o produto funcione de ponta a ponta.

---

## Comandos de Validação Obrigatórios

```bash
# Backend
cd core
go build ./...
go vet ./...
go test ./...

# Frontend
cd core/web
npm run lint
npm run build
```

Todos devem passar sem erros antes de considerar qualquer fase concluída.

---

## Testes de API Completos

Execute na ordem abaixo após iniciar o daemon com `dwyt .`:

```bash
BASE="http://localhost:2737"

# Core
curl -s $BASE/api/health | jq .
curl -s $BASE/api/status | jq .
curl -s $BASE/api/context | jq .
curl -s $BASE/api/projects/current | jq .
curl -s $BASE/api/state | jq .

# Obsidian
curl -s $BASE/api/obsidian/status | jq .
curl -s -X POST $BASE/api/obsidian/save \
  -H "Content-Type: application/json" \
  -d '{"type":"decision","content":"Validação final"}' | jq .
curl -s "$BASE/api/obsidian/search?q=validação" | jq .
curl -s -X POST $BASE/api/obsidian/summarize | jq .
curl -s -X POST $BASE/api/obsidian/open-dir | jq .

# MCP
curl -s $BASE/api/mcp/registry | jq .
curl -s -X POST $BASE/api/mcp/configure \
  -H "Content-Type: application/json" \
  -d '{"name":"codebase"}' | jq .

# Codebase
curl -s -X POST $BASE/api/services/codebase/start | jq .
curl -s $BASE/api/services/codebase/status | jq .
curl -s -X POST $BASE/api/codebase/index \
  -H "Content-Type: application/json" \
  -d '{"path":"'$(pwd)'"}' | jq .
curl -s $BASE/api/codebase/index/status | jq .

# Headroom
curl -s -X POST $BASE/api/services/headroom/start | jq .
curl -s $BASE/api/services/headroom/status | jq .

# Kiro Power (arquivo 12)
curl -s $BASE/api/kiro/power/status | jq .
```

> **Nota:** O endpoint `/api/kiro/power/status` só existirá após implementar o arquivo 12.

---

## Checklist de Consistência de Status

```bash
# Executar e comparar manualmente
echo "=== /api/status ==="
curl -s $BASE/api/status | jq '.tools[] | select(.name=="obsidian" or .name=="codebase-memory-mcp")'

echo "=== /api/obsidian/status ==="
curl -s $BASE/api/obsidian/status | jq '{status: .status}'

echo "=== /api/services/codebase/status ==="
curl -s $BASE/api/services/codebase/status | jq '{running: .running, healthy: .healthy}'

echo "=== /api/mcp/registry ==="
curl -s $BASE/api/mcp/registry | jq '.mcpServers | to_entries[] | {name: .key, status: .value.status}'
```

Nenhum campo deve contradizer outro.

---

## Atualizar Documentação

### `docs/HOW-IT-WORKS.md`

Atualizar seções que mudaram:

- [ ] Rotas `/api/brain/*` → `/api/obsidian/*` (se ainda houver referências)
- [ ] Nomes de MCP: `codebase` e `obsidian`
- [ ] Fluxo do SetupWizard atualizado
- [ ] Estrutura de arquivos gerados por cliente
- [ ] Seção de testes com Playwright
- [ ] Adicionar seção do Kiro Power (`~/.dwyt/powers/dwyt-power/`)
- [ ] Atualizar estrutura `~/.dwyt/` para incluir `powers/`

### `docs/CHANGELOG.md`

Adicionar entrada no topo com:

- Versão e data
- Bugs corrigidos
- Features adicionadas
- Arquivos modificados
- Resultado da validação

### `docs/plan/PLAN.md`

Já contém nota de referência para os arquivos segmentados `00` a `12`. Manter.

---

## Documentação de Data (Formato Obrigatório)

Criar pasta `docs/<DDMMYYYY>/` usando a data da execução (ex.: em 6 de maio de 2026, `docs/06052026/`) com exatamente 3 arquivos:

```
docs/<DDMMYYYY>/
├── FIXES.md       # detalhes técnicos de cada correção
├── SUMMARY.md     # status final, métricas, commit sugerido
└── VALIDATION.md  # comandos de validação e resultados
```

---

## Critérios de Aceite Finais

### Produto

- [ ] `dwyt .` abre dashboard corretamente
- [ ] Projeto atual é detectado pelo path do comando
- [ ] Dados ficam em `~/.dwyt/`
- [ ] Vaults ficam em `~/.dwyt/projects/<id>/obsidian/`
- [ ] Vaults nunca são apagados por install/uninstall/reset
- [ ] Obsidian é carregado por projeto
- [ ] Codebase mostra nodes/edges reais
- [ ] Headroom inicia sem quebrar Codex OAuth
- [ ] RTK aparece como CLI
- [ ] Kiro Power gerado em `~/.dwyt/powers/dwyt-power/` quando Kiro habilitado

### MCP

- [ ] Registry usa `codebase` e `obsidian`
- [ ] Dashboard usa `codebase` e `obsidian`
- [ ] `.mcp.json` usa `codebase` e `obsidian`
- [ ] `.claude/mcp.json` correto
- [ ] `.kiro/mcp.json` correto
- [ ] `.vscode/mcp.json` correto
- [ ] OpenCode correto
- [ ] Configure MCP é granular por serviço (`codebase` ou `obsidian`)

### UI

- [ ] Cards não mostram status falso
- [ ] Botões têm loading/disabled/foco
- [ ] Mobile não quebra
- [ ] Open Vault e Open Dir são ações separadas
- [ ] RTK não tem Start/Stop
- [ ] Logs são acessíveis

### Testes

- [ ] `go build ./...` ✅
- [ ] `go vet ./...` ✅
- [ ] `go test ./...` ✅
- [ ] `npm run lint` ✅
- [ ] `npm run build` ✅
- [ ] E2E atualizado e passando
- [ ] Playwright passando (se instalado)
- [ ] Testes cobrem botões principais do Dashboard
- [ ] Testes do pacote `kiropow` passando (14 casos — arquivo 12)

### Documentação

- [ ] README atualizado
- [ ] HOW-IT-WORKS atualizado
- [ ] CHANGELOG atualizado
- [ ] Rotas antigas removidas
- [ ] Fluxo de setup documentado

---

## Prompt Direto para Execução

```
Você é o executor principal no projeto DWYT.

Objetivo: revisar e ajustar o produto com base no plano segmentado em docs/plan/,
priorizando confiabilidade, consistência de status, MCPs obrigatórios, Obsidian como
memória persistente por projeto, UX do Dashboard, SetupWizard e validação completa.

Regras obrigatórias:
1. Não apague vaults em ~/.dwyt/projects/<id>/obsidian/
2. Tudo que o DWYT gerencia deve ficar em ~/.dwyt/
3. MCPs obrigatórios: exatamente "codebase" e "obsidian" em todos os lugares
4. RTK é CLI, não daemon — sem Start/Stop no Dashboard
5. Dashboard não pode mostrar status contraditório
6. Obsidian nunca aparece active se ProjectObsidian for nil
7. Headroom wrap codex é falha não-fatal
8. Audite antes de reimplementar
9. Mudanças incrementais com validação após cada grupo
10. Atualize testes e documentação junto com o código

Ordem de execução:
1. Auditar (docs/plan/01-audit-and-diagnosis.md)
2. MCPs (docs/plan/02-mcp-standardization.md)
3. Status (docs/plan/03-status-contract.md)
4. Obsidian (docs/plan/04-obsidian-vault.md)
5. Codebase + Headroom (docs/plan/05-codebase-and-headroom.md)
6. RTK + Dashboard (docs/plan/06-rtk-and-dashboard-ux.md)
7. Setup + AI clients (docs/plan/07-setup-wizard-and-ai-clients.md)
8. Bugs (docs/plan/08-bug-hunting.md)
9. Testes browser CLI (docs/plan/09-testing-with-browser-cli.md)
10. Refatoração (docs/plan/10-refactoring-and-code-quality.md)
11. Validação final (docs/plan/11-validation-and-docs.md)

Validação final obrigatória:
- go build ./...
- go vet ./...
- go test ./...
- cd core/web && npm run lint
- cd core/web && npm run build
- bash core/test-e2e.sh
```
