# Tokens saved and provenance

DWYT distinguishes tool counters, transparent estimates, deterministic benchmark
counterfactuals, and missing measurements. A dashboard number is useful only when
its source and limits remain visible.

## Tool-level sources

| Tool | Source | Provenance |
|---|---|---|
| RTK | `rtk gain` and `rtk gain --project` | Observed tool counter |
| Headroom | Headroom `/stats` | Observed tool counter when available |
| Codebase MCP | Index metadata such as nodes and edges | Estimated local model |
| Obsidian MCP | Vault markdown count and bytes | Estimated local model |

The global summary remains defensive when a tool is inactive, unindexed, empty, or
returns an older response. A missing value is displayed as unavailable, not as a
fabricated zero.

## Request telemetry provenance

Every request-level metric is tagged with one of these values:

| Value | Meaning |
|---|---|
| `observed` | Provider or runtime reported the measurement |
| `estimated` | Local calculation with documented inputs |
| `benchmark_counterfactual` | Fixture or benchmark-harness comparison |
| `unsupported` | DWYT cannot provide the measurement |

These values are not interchangeable. In particular, a deterministic benchmark does
not become provider telemetry, and `unsupported` never becomes zero.

## Gross and net savings

Gross avoided tokens summarize the available source metrics. Net savings subtracts
measurable overheads such as the startup schema, managed instruction block, and
external recovery metadata. `raw_ref` is already part of the sent payload estimate
and is not charged twice.

`net_estimated_tokens` is available only when every input is observed or estimated.
If the window contains benchmark-counterfactual or unsupported inputs, DWYT withholds
the net estimate rather than relabelling it as product savings.

## Sessions and windows

Lifetime counters cannot answer what happened in a single working period. Timestamped
metric, MCP-usage, and LLM-request ledgers power windowed views and sessions separated
by an inactivity gap. Observed provider throughput is kept separate from an explicitly
labelled local fallback.

## Evidence and claims

The deterministic harness, startup-tax baselines, and acceptance evidence are
explained in [benchmarks](benchmarks.md). The current acceptance record is the
[scorecard](../../plan/Melhorias_v5.0.x/baseline/scorecard.md). Neither a fixture
comparison nor a serial microbenchmark is a product savings claim without compatible
quality, cost, latency, and real-use evidence.
