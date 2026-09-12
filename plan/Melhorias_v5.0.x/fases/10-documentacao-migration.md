# Fase 10 — Migração e Consolidação da Documentação

> Fontes: Fine-Tuning §13 (reestruturação), §35 (qualidade padrão), §3.3 (drift pre-v5) · evidência: `docs/` flat com how-it-works.md grande + rules/rules.md legado

## Goal

Documentação curta, navegável e fiel ao runtime: estrutura alvo do Fine-Tuning §13.1 adaptada às convenções existentes, `how-it-works.md` deixado de ser duplicata monolítica, `rules.md` migrado, links íntegros.

## Baseline (pre-migration)

```text
docs/
├── architecture-v5.md      # arquitetura atual (relativamente nova)
├── codebase-law.md
├── obsidian-law.md
├── optimizer-law.md        # criada na Fase 5
├── how-it-works.md         # monolítico grande (baseline Fase 0 mede)
├── rules/rules.md          # legado
├── readme.md, CHANGELOG.md, release-process.md, kiro-power.md, tokens-saved.md
└── windows/                # docs Windows (README, troubleshooting)
```

Fase 1 pode ter merged `fix/release-semver-v5` (38 arquivos de docs v5 accuracy + readme) — rebasear esta fase em cima do que existir.

## Problems being solved

- Duas "constituições" concorrentes (how-it-works vs architecture-v5) e um rules.md legado violam fonte única (Fine-Tuning §13.4/§13.5).
- Descrições de status/fluxo de startup ficarão desatualizadas após Fases 2-4 — atualizar aqui (docs descrevem o comportamento que passou a existir).

## Files/packages affected

```text
docs/…                              # moves/splits e páginas de compatibilidade
readme.md                           # índice público
core/internal/integrate/documentation_test.go  # contrato, links, âncoras e case-fold
core/internal/kiropow/{kiropow.go,kiropow_test.go} # steering por estágio, sem ordem global
.github/workflows/ci.yml            # job Ubuntu rápido para documentação
```

## Changes

1. **Estrutura alvo** (adaptada, sem move por estética — §13: cada move melhora descoberta ou remove duplicação):
   ```text
   docs/
   ├── readme.md            # índice + o que é DWYT (curto)
   ├── CHANGELOG.md
   ├── laws/{optimizer,codebase,obsidian}-law.md
   ├── architecture/overview.md          # ownership 3-MCP + RTK/Headroom + fluxo
   ├── architecture/runtime.md           # startup (Fase A/B), reconciler, procman, storage, plataformas
   ├── architecture/context-optimization.md  # budget, ROI, reuse, GC, compressão, raw store, output, cache, startup tax
   ├── metrics/tokens-saved.md + benchmarks.md
   ├── operations/release-process.md
   ├── integrations/kiro-power.md
   └── windows/               # permanece (convenção atual)
   ```
2. **how-it-works.md**: auditar conteúdo — material único → architecture/*; depois reduzir a página curta de navegação ou remover (§13.4). Nada de 40KB duplicado.
3. **rules/rules.md**: regras ainda válidas → leis/arquitetura/operations; histórico real → arquivo morto claro ou remoção (§13.5). Duas fontes de regras = proibido.
4. **Conteúdo das Fases 2-4**: startup dashboard-first, reconciler, modelo de status, portas efetivas, diagnósticos — documentar em `architecture/runtime.md` + troubleshooting (windows/ + nova seção linux/macos se útil).
5. **Testes de consistência** (Fine-Tuning §13.6, scripts Go simples):
   - exatamente 3 MCPs first-party documentados;
   - optimizer-law linkada do índice;
   - nenhum doc ativo com linguagem de ordem global de tools (`RTK → Codebase → Obsidian → …`);
   - links internos resolvem (crawler mínimo de `docs/` + README);
   - nenhum doc ativo afirma "duas leis principais";
   - sem blocos DWYT duplicados em arquivos gerados (reuso do teste da Fase 5).
6. **tokens-saved.md**: reescrever com provenance (observed/estimated/benchmark) e linkar scorecard.

## Non-goals

- Não documentar arquivo por arquivo do código (§13: documentar contratos, não catálogo).
- Não criar framework de docs; mover o mínimo.

## Backward-compatibility risks

- Links externos (issues/PRs apontam para docs antigos): adicionar stub redirect nos paths movidos de maior tráfego.

## Data-safety risks

- Nenhum.

## Tests

- Testes de consistência do item 5 rodam em CI (job rápido ubuntu).

## Benchmark/evidence

- Baseline Fase 0: `01-how-it-works.md` tinha 1.187 linhas / 52.372 bytes e
  `rules/rules.md` 643 linhas / 18.180 bytes.
- Pós-migração: a página de compatibilidade tem 15 linhas / 777 bytes e o arquivo
  arquivado de regras 23 linhas / 1.163 bytes; conteúdo canônico vive nos documentos
  focados em `architecture/`, `laws/`, `metrics/`, `operations/` e `integrations/`.
- `TestDocumentationLinksResolve` e os testes de contrato documental passaram
  localmente (`rtk go test ./internal/integrate -count=1`) e são executados pelo job
  Ubuntu de CI.

## Rollback path

A migração e seus redirects são isolados neste commit; revertê-lo restaura a árvore anterior e os caminhos de compatibilidade evitam quebra imediata de links.

## Definition of Done

- [x] Estrutura alvo implementada; índice no readme.md resolve tudo.
- [x] how-it-works não duplica arquitetura (split/remoção).
- [x] rules.md migrado/arquivado; uma única fonte de regras.
- [x] Testes de consistência verdes localmente e configurados no job rápido Ubuntu de CI.
- [x] Todos os links internos resolvem pelo crawler mínimo de `docs/` + `readme.md`.

## Status

`done — migração documental consolidada; validação local registrada e commit/push pendentes`