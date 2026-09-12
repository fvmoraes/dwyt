# Context optimization

DWYT optimizes context deterministically. It does not ask a second model whether it
should save tokens; it uses task metadata, budgets, lifecycle state, and observed
capabilities to make a reproducible decision.

## Retrieval and budgets

For work beyond a trivial lookup, establish a context plan before broad retrieval.
The plan assigns a budget, retrieval level, and visible-output target. Retrieval
narrows from project map to module, symbol, range, and full file only when the
smaller evidence is insufficient. Content already delivered in the session is reused
by reference or hash instead of being resent.

The Optimizer supplies the envelope; Codebase and Obsidian supply the evidence that
fits it. This avoids treating a large context window as permission to load a whole
repository or vault.

## Token ROI and compression

A reduction is used only when its expected net gain is positive:

```text
net gain = avoided tokens − compression metadata − recovery cost
```

Small payloads can pass through unchanged. A compressed result must preserve the
verdict and retain a recoverable raw payload. `raw_ref` is part of the sent payload
estimate; recovery metadata is charged only when it is external to that payload, so
cost is not counted twice.

## Raw store, output, and cache

Compacted tool output preserves a deterministic summary, diagnostics, counts, and a
raw object reference. The source remains available through `dwyt_get_raw`; a shorter
presentation is not permission to destroy evidence.

Output profiles cap operational narration by task phase, but requested artifacts are
not truncated. Prompt cache guidance keeps stable instructions and schemas early,
while reported cache reads remain observed facts rather than promises about a
third-party client.

## Memory lifecycle

The Housekeeper promotes durable knowledge before deleting operational material,
caps sessions, applies category TTLs, detects staleness by content hash, and dedups
deterministically. Errors, constraints, and pinned context are protected evidence:
the target budget may remain unmet rather than silently discarding them.

## Honest telemetry and net savings

Every reported measurement has one provenance value:

| Provenance | Meaning |
|---|---|
| `observed` | Reported by the provider or measured runtime source |
| `estimated` | Transparent local calculation with known inputs |
| `benchmark_counterfactual` | Deterministic harness comparison, not product usage |
| `unsupported` | The runtime cannot provide the value |

Missing data stays `unsupported`, not `0`. `net_estimated_tokens` is emitted only
when every input is observed or estimated. A window containing benchmark
counterfactual data cannot be promoted to an estimated product saving.

## Startup tax and benchmarks

Startup tax is a cost of the payload placed in `tools/list` and the managed
instruction block, not a task-completion metric. The deterministic harness and
serial Go benchmarks preserve this distinction; see [benchmarks](../metrics/benchmarks.md)
and the [Optimizer Law](../laws/optimizer-law.md) for the operational contract.
