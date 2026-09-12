# Benchmarks and measurement policy

DWYT uses deterministic benchmarks to protect behavior and measure bounded payload
costs. They are evidence about the harness or a local implementation, not automatic
claims about product performance or user savings.

## Deterministic harness

`dwyt bench` exercises scenarios across baseline and DWYT arms, including negative
compression and no-compression cases. Its output separates inputs, retrieval,
compression metadata, output, and provenance. It ends with `claim_allowed=false`:
a fixture cannot establish completion quality, provider cost, cache-hit rate, or
latency in real usage.

## Startup tax

Startup tax measures the JSON `result` payload used by `tools/list` plus the managed
instruction block. It is a startup cost, not tokens per completed task. Current
reference measurements in the acceptance scorecard report 14,869 schema bytes / 3,718
estimated tokens and 1,468 instruction bytes / 367 estimated tokens; the external
Codebase schema remains explicitly unknown and is not included as zero.

## Serial Go benchmarks

Run microbenchmarks serially with repeat samples and allocation reporting, for example:

```bash
rtk go test ./internal/mcp -run '^$' -bench '^BenchmarkMeasureStartupTax$' -benchmem -count=10 -cpu=1
```

Use a compatible paired before/after comparison and `benchstat` before calling a
change an improvement or regression. Repeated samples alone do not support a product
claim.

## Documentation-migration evidence

The Fase 0 baseline recorded a 1,187-line, 52,372-byte `01-how-it-works.md` and a
643-line, 18,180-byte `rules/rules.md`. After the migration, the compatibility page
is 15 lines / 777 bytes and the archive page is 23 lines / 1,163 bytes. The two
competing active documents no longer carry architecture or agent policy: focused
architecture pages and the three canonical laws are the only active sources. The
link crawler and policy checks run in `core/internal/integrate` and are wired to the
Ubuntu CI documentation job.

## Acceptance evidence

See the [scorecard](../../plan/Melhorias_v5.0.x/baseline/scorecard.md) and the
captured [Phase 9 benchmark output](../../plan/Melhorias_v5.0.x/baseline/bench-pos-fase9.txt).
Keep each number's provenance; do not turn counterfactual benchmark reductions into
observed product savings.
