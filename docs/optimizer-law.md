# Optimizer Law

Mandatory invariants for the `dwyt_optimizer` MCP and every code path that
decides how much context an agent receives. These rules are enforced by the
runtime (`core/internal/contextopt`, `core/internal/toolopt`,
`core/internal/outputopt`), not by prompt size. The client-facing Entry
Contract (see `internal/integrate/instructions.go`) stays small precisely
because these invariants live here and in the code.

## Invariants

**Law 1 — Plan before broad retrieval.** For anything beyond a trivial
lookup, a context plan (`dwyt_context_plan`) is established before wide
retrieval.

**Law 2 — Budget before context.** The Optimizer owns the context budget.
Never retrieve first and budget afterwards.

**Law 3 — Suppliers supply, the Optimizer decides.** Codebase and Obsidian
provide candidates; they never decide how much context the model receives.

**Law 4 — Reuse before retrieve.** If the session already holds valid
content (hash/reference/delta), reuse it instead of resending the same bytes.

**Law 5 — Narrow before broad.** Prefer:
summary → symbol → relationship/range → snippet → full file. Escalate one
level only when the previous level is insufficient.

**Law 6 — Positive Token ROI.** No mechanism is used merely because it
exists. Compression, expansion and compaction must have a positive expected
net gain (tokens − metadata − recovery cost); otherwise pass through.

**Law 7 — Preserve critical evidence.** Never compact away the only copy of
errors, failing assertions, security warnings, exact identifiers, user
constraints or pinned context.

**Law 8 — Raw content remains recoverable.** Lossy presentation is
acceptable; destructive loss of source is not. Use the Raw Object Store
(`dwyt_get_raw`) handles.

**Law 9 — Stable prefix first.** Stable instructions and schemas stay early
and unchanged; no timestamps, random IDs or volatile telemetry in the
cacheable prefix.

**Law 10 — Stop when sufficient.** Retrieval ends when evidence is
sufficient to act safely. No "just in case" fetching.

**Law 11 — Deterministic policy first.** No extra LLM call is spent deciding
how to save tokens when metadata and rules can decide deterministically.

**Law 12 — Compress once.** One appropriate reduction step; never a cascade
of generic compressors. Payloads already reduced by DWYT are detected and
passed through.

**Law 13 — User intent overrides brevity.** A requested artifact, document,
diff, code file or exact payload is never silently truncated by
output-saving rules.

**Law 14 — Honest telemetry.** Every number carries its provenance:
observed ≠ estimated ≠ unknown. Unknown stays unknown — never zero, never
offline, never a fabricated savings claim.

## Ownership boundaries

| Component | Owns | Must not own |
|---|---|---|
| `dwyt_optimizer` | budget, Token ROI, reuse, GC, output contracts, cache guidance, routing, compaction policy, raw store | project knowledge or code truth |
| `dwyt_codebase` | symbols, modules, calls, dependencies, impact, snippets | token-budget policy or history |
| `dwyt_obsidian` | decisions, canonical knowledge, session memory, lifecycle | code structure or budget decisions |
| RTK | terminal output reduction | any policy authority |

A supplier supplies. The Optimizer decides.

See also: [codebase-law.md](codebase-law.md) and
[obsidian-law.md](obsidian-law.md) for the retrieval and memory workflows
the Optimizer governs.
