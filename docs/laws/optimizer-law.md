# Optimizer Law

Mandatory invariants for `dwyt_optimizer` and every code path that decides how much
context an agent receives. These rules are enforced by the runtime, not by expanding
the client-facing instruction block.

## Invariants

**Law 1 — Plan before broad retrieval.** For anything beyond a trivial lookup,
establish `dwyt_context_plan` before wide retrieval.

**Law 2 — Budget before context.** The Optimizer owns the context budget. Never
retrieve first and budget afterwards.

**Law 3 — Suppliers supply, the Optimizer decides.** Codebase and Obsidian provide
candidates; they never decide how much of their answer belongs in the window.

**Law 4 — Reuse before retrieve.** Reuse session content by hash, reference, or
delta rather than resending valid bytes.

**Law 5 — Narrow before broad.** Prefer summary → symbol → relationship or range →
snippet → full file. Escalate one level only when the prior result is insufficient.

**Law 6 — Positive Token ROI.** Compression, expansion, and compaction require a
positive expected net gain after metadata and recovery cost; otherwise pass through.

**Law 7 — Preserve critical evidence.** Never compact away the only copy of errors,
failing assertions, security warnings, exact identifiers, user constraints, or pinned
context.

**Law 8 — Raw content remains recoverable.** Lossy presentation is acceptable;
destructive source loss is not. Keep a Raw Object Store handle.

**Law 9 — Stable prefix first.** Stable instructions and schemas stay early and
unchanged; volatile timestamps, IDs, and telemetry do not enter the cacheable prefix.

**Law 10 — Stop when sufficient.** Retrieval ends when evidence is sufficient to act
safely. Do not fetch additional context merely in case it becomes useful.

**Law 11 — Deterministic policy first.** Do not spend an extra LLM call deciding
what metadata and rules can decide deterministically.

**Law 12 — Compress once.** Apply one appropriate reduction; payloads already
reduced by DWYT are detected and passed through.

**Law 13 — User intent overrides brevity.** A requested document, diff, code file,
or exact payload is never silently truncated by output-saving rules.

**Law 14 — Honest telemetry.** Every number carries provenance: observed, estimated,
benchmark counterfactual, or unsupported. Missing data never becomes a flattering
zero or an unsubstantiated savings claim.

## Ownership boundaries

| Component | Owns | Must not own |
|---|---|---|
| `dwyt_optimizer` | Budget, Token ROI, reuse, GC, output contracts, cache guidance, routing, compaction policy, raw store | Project knowledge or code truth |
| `dwyt_codebase` | Symbols, modules, calls, dependencies, impact, snippets | Token-budget policy or history |
| `dwyt_obsidian` | Decisions, canonical knowledge, session memory, lifecycle | Code structure or budget decisions |
| RTK | Terminal output reduction | Policy authority |

A supplier supplies; the Optimizer decides. See the [Codebase Law](codebase-law.md)
and [Obsidian Law](obsidian-law.md) for the source-of-truth workflows.
