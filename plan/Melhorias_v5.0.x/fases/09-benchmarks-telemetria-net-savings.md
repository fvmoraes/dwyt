# Fase 9 — Benchmarks, Telemetria e Net Savings

> Fontes: Fine-Tuning §12 (benchmark e telemetria), §30-31 (filosofia de aceitação), §3.9 (honest savings) · evidência: `telemetry/` (RecordRequest/Summarize, SQLite), `benchmark/`, branch `dwyt-v5` já traz per-session savings/throughput observado (merge da Fase 1)

## Goal

Estender `dwyt bench` com cenários determinísticos (incluindo o obrigatório "compressão piora"), completar a telemetria com provenance (observed/estimated/unknown) e derivar **net savings** honesto para o dashboard.

## Current state

- Telemetria em SQLite (`telemetry.New(store.DB())`, `RecordRequest`, `Summarize`) persiste provenance por métrica e metadata de compressão por request, com migração aditiva.
- `benchmark/` cobre a matriz determinística A–K com fixtures em `testdata/`, incluindo o cenário negativo de passthrough e o caso sem compressão.
- Dashboard mantém savings legados separados e exibe net savings apenas no diagnostics/details, com cobertura e `unsupported` explícitos quando não há dados suficientes.
- Fase 8 fornece o startup tax estimado como insumo; catálogo externo `dwyt_codebase` permanece `unknown` e bloqueia um net total enganoso.

## Problems being solved

- Claims de economia sem cenários reproduzíveis (Fine-Tuning §28.5: sem benchmark-driven cheating).
- Metodologia de aceitação: não existe scorecard; "X% de redução" sem custo/latência/qualidade é inaceitável (§30).

## Files/packages affected

```text
core/internal/benchmark/…              # cenários + fixtures determinísticas
core/internal/telemetry/…              # campos faltantes + provenance
core/internal/server/handlers_metrics.go / handlers_optimizer.go  # agregações
core/web/src/…                         # seção de diagnóstico de net savings (details view)
```

## Changes

1. **Cenários do bench** (Fine-Tuning §12.2, fixtures em `testdata/`, sem rede/LLM real — só os caminhos determinísticos do DWYT):
   - pergunta trivial; lookup de símbolo; impact analysis cross-file;
   - debug com output de teste falho grande; output de teste ok grande; JSON grande; log grande;
   - recall histórico Obsidian; sessão longa com retrieval repetido;
   - **cenário negativo obrigatório**: payload em que comprimir piora → assert `passthrough`;
   - cenário sem necessidade de compressão.
2. **Métricas por cenário** (Fine-Tuning §12.3): input/output tokens (est. vs observado quando existir), retrieval tokens, compression metadata, raw_recovery_count, full_file_reads, mcp_calls, latency, task_passed, tokens_per_completed_task. Campos não disponíveis → `unknown`, nunca `0` (§12.4/§28.6).
3. **Provenance em toda métrica**: `observed | estimated | benchmark_counterfactual | unsupported` (§3.9). Telemetria: colunas já previstas no schema SQLite se ausentes (migração aditiva).
4. **Rigor de medição** (skill `golang-benchmark`): `-count=10 -benchmem` por cenário; comparação **exclusivamente** via `benchstat`; resultados com `~` (não-significativos) não são claims; medir **serialmente** (benchmark concorrente contamina ns/op); fixtures dimensionadas para medição separadas das de correção (`*_bench_test.go` separado de `*_test.go`); `b.Loop()` nos benchmarks novos; saída do bench colada nos commits/PRs que mudam performance.
5. **Net savings** (derivação, não novo subsistema):
   ```text
   net_est = gross_avoided − startup_schema_tax(Fase 8) − managed_instruction_tax − compression_metadata
   ```
   exibido no dashboard **atrás de details/diagnostics**, com rótulos `(est.)` e `—` para unknown (Fine-Tuning §8.2, §24 UI rules: unknown ≠ 0, sem fake precision).
6. **Scorecard por fase de aceitação** (Fine-Tuning §31): toda otimização das fases 6/8 registra linha no scorecard (`baseline/scorecard.md`): cenário, baseline tokens/task, candidato, qualidade, latência, decisão.

## Non-goals

- Não criar comando de benchmark paralelo (estender `dwyt bench`).
- Não inventar campos que providers não expõem.
- Não usar números do bench como claim de produto sem execução documentada.

## Backward-compatibility risks

- Migração SQLite aditiva (novas colunas) — padrão já usado pelo projeto; sem quebra.

## Data-safety risks

- Fixtures não devem conter paths/segredos reais; bench roda em temp dirs.

## Tests

```text
TestBenchmarkScenarioNegativeCompressionChoosesPassthrough   # obrigatório
TestBenchmarkScenariosDeterministic                          # 2 runs → mesma saída
TestTelemetryProvenanceFields                                # observed/estimated/unknown persistidos
TestNetSavingsDerivation                                     # gross − taxes, unknown propagado
TestMetricsAPIUnknownNotZero
```

## Benchmark/evidence

- `baseline/bench-pos-fase9.txt` contém a saída integral de `cd core && rtk go run . bench`: matriz A–K, provenance `benchmark_counterfactual`, cenário J em passthrough e `claim_allowed=false`.
- `BenchmarkMeasureStartupTax` foi executado serialmente com `-benchmem -count=10 -cpu=1`. Os payloads são estáveis em 14.869 bytes / 3.718 tokens estimados de schema e, quando aplicável, 1.468 bytes / 367 tokens estimados de instrução. Sem comparação pareada por `benchstat`, não há claim de ganho ou regressão de performance.
- `baseline/scorecard.md` registra as decisões das Fases 6, 8 e 9, com qualidade/latência ausentes explicitamente como `unsupported`.

## Rollback path

Cenários/derivações aditivas; reverter commits individuais. Migração de colunas: reverter código + coluna extra inofensiva (sem drop).

## Definition of Done

- [x] `dwyt bench` cobre os 11 cenários, é determinístico e inclui cenário negativo verde.
- [x] Toda métrica exposta carrega provenance; `unsupported` nunca é renderizado como zero.
- [x] Net savings calculável é visível em diagnostics, com rótulos e cobertura honestos.
- [x] Scorecard registra a avaliação ponta a ponta e não promove contrafactuais a savings de produto.

## Status

`done — evidências, scorecard e validação da Fase 9 registrados em 2026-09-11; o commit da fase contém os artefatos.`