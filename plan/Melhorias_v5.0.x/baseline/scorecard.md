# Scorecard de aceitação — Fases 6, 8 e 9

Atualizado em 2026-09-11. Este scorecard separa evidência determinística de uso de produto: `benchmark_counterfactual`, estimativas de schema e testes funcionais não são telemetria observada de provider. `unsupported` nunca é tratado como zero.

| Fase | Cenário | Baseline tokens/task | Candidato | Qualidade | Latência | Decisão e evidência |
|---|---|---|---|---|---|---|
| 6 | Gate de compressão: `small_bypass` e `large_compress` | `unsupported`: o microbenchmark não mede uma tarefa completa nem tokens/provider | Gate de ROI; payload pequeno passa intacto, payload grande só comprime se preservar o veredito | Invariantes funcionais verificadas pelo benchmark; conclusão ao vivo `unsupported` | `unsupported` | Aceito como proteção funcional. Medição serial de 10 amostras em `toolopt-gate-working-tree-2026-09-11-linux-amd64.txt`; sem baseline pareado/`benchstat`, logo nenhum ganho de performance é alegado (`claim_allowed=false`). |
| 8 | Payload `tools/list` e bloco de instrução gerenciado | Não aplicável por tarefa: é custo de startup, não tokens/task | Medição/gate, sem otimização de schema | Shape, pureza e baseline congelado cobertos por testes | `unsupported` | Aceito como baseline de custo. A Fase 9 mediu o estado atual em 3.719 tokens estimados de schema e 367 de instrução; `dwyt_codebase` permanece `unknown`, portanto não existe total completo nem claim de redução. |
| 9 | Matriz A–K do `dwyt bench`, incluindo compressão negativa e ausência de compressão | 5.463.718 tokens de entrada por execução do harness (`benchmark_counterfactual`, não tokens/task de produto) | `dwyt_v5_optimizer`: 193.165 tokens de entrada, 11.620 de memória, 2.215 de tool output e 3.650 de output por execução do harness | Determinismo, matrix coverage, provenance e passthrough de J cobertos; completion ao vivo `unsupported` | `unsupported` | Infraestrutura de evidência aceita. Saída integral em `bench-pos-fase9.txt`; `claim_allowed=false`. As reduções só descrevem o harness e não são savings de produto até haver qualidade, custo e latência observados em execução real. |

## Medições seriais de referência

`BenchmarkMeasureStartupTax` foi executado serialmente com `-benchmem -count=10 -cpu=1` em Linux/amd64 (12th Gen Intel(R) Core(TM) i7-1255U). As dez amostras confirmaram os valores determinísticos de payload: 14.869 bytes / 3.718 tokens estimados de schema; com instrução, mais 1.468 bytes / 367 tokens estimados. A variação de `ns/op` não é uma comparação antes/depois e não foi interpretada como ganho ou regressão.

## Regra de aceitação

Uma linha só pode afirmar economia de produto quando houver comparação compatível, qualidade sem regressão e telemetria observada para custo/latência. Até lá, este scorecard preserva a decisão e a proveniência sem converter ausência em zero.
