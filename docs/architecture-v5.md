# DWYT v5 Architecture

DWYT v5 turns the project into a **Context Optimizer**: three MCP servers with
strictly separated jobs, a persistent brain, and deterministic rules for budget,
retrieval, lifecycle, output, cache and cost.

This document is the answer to "which component owns what". If you are looking
for the HTTP surface, package map and startup flow, read
[HOW-IT-WORKS](how-it-works.md) instead.

---

## Component roles

Every DWYT component has exactly one job. Overlap is what made earlier versions
expensive: two components both deciding what context to load meant neither could
be trusted, and the agent paid for both answers.

| Component | Role | Owns |
|---|---|---|
| **`dwyt_optimizer`** (DWYT MCP) | **Optimizer** | Efficiency policy: context budget, Token ROI ranking, progressive retrieval, delta reuse, context GC, output contract, cache guidance, routing, raw object store |
| **`dwyt_obsidian`** (Obsidian MCP) | **Brain** | Durable project memory: canonical knowledge, decisions, tasks, context snapshots, HOT/WARM/COLD temperature, Search V2 |
| **`dwyt_codebase`** (Codebase MCP) | **Code Intelligence** | Structural retrieval: symbols, routes, call paths, dependencies, impact |
| **RTK** | **Terminal optimization tool** | Shell execution with compressed output. Not an MCP — a CLI |
| **Housekeeper** | **Memory lifecycle** | Session cap, TTLs, stale detection, dedup, promotion before expiry |
| **Memory Compiler** | **Knowledge consolidation** | Turning repeated session evidence into canonical notes |
| **Headroom** | **API proxy** | Optional transport-level compression. Never a source of truth |

Two rules follow from the table and are worth stating plainly:

- **The Optimizer decides, the other two supply.** `dwyt_codebase` and
  `dwyt_obsidian` answer questions; they never decide how much of their answer
  belongs in the window. That decision has one owner.
- **The Brain is not a cache.** Obsidian holds knowledge that should outlive the
  session. Operational data (logs, raw tool output, session history) expires;
  knowledge is promoted out of it first.

---

## Where state lives

Optimizer session state lives in the **daemon**, not in the MCP process.

That is deliberate. An MCP server is spawned per client, so state held there
would fragment: Kiro and Codex working the same task would each build their own
idea of what the model already has, and delta reuse would silently stop working
across clients. Keeping it in the daemon means one session per task, shared by
every AI client on the machine.

```
AI client (Kiro / Codex / Claude / Cursor / OpenCode / Copilot)
   │  stdio MCP
   ▼
dwyt_optimizer  ──HTTP──▶  daemon :2737  ──▶  session state, budget, delta store
dwyt_obsidian   ──HTTP──▶  daemon :2737  ──▶  vault (markdown on disk)
dwyt_codebase   ──HTTP──▶  :9749         ──▶  code knowledge graph
```

---

## The Optimizer

### Input optimization

The pipeline runs in a fixed order, and every step is a pure function of
metadata. No LLM call is spent deciding how to save tokens — that would be
self-defeating.

1. **Classify** the task (complexity, phase) deterministically.
2. **Budget** before retrieving. A trivial task does not get a 48k window.
3. **Rank** candidates by Token ROI: usefulness per *effective* token, where
   effective means cost-adjusted. A large block already in a cached prefix can be
   cheaper than a small one that would break it.
4. **Select** into the budget, honouring cache order so the prompt has a stable
   prefix.
5. **Reuse before retrieve.** Content the session already delivered is
   referenced by hash, not resent.
6. **GC** by lifecycle state, not by score: a resolved error is dropped before a
   still-relevant architecture note, however the numbers compare.

Pinned candidates are never dropped. Cutting context the caller declared
essential in order to hit a number is the one thing the optimizer will not do.

### Output optimization

Output has a per-phase visible-token target: classification gets ~120, a fix
~350, a review ~1500, a plan ~4000.

The **artifact exception** matters: when the requested output *is* the product —
a document, a spec, a generated file — there is no operational cap. Truncating
what the user asked for to save tokens is not optimization, it is failure.

### Tool output optimization

Large tool output is compacted deterministically to status, deduplicated
diagnostics, counts and a tail — never by an LLM. The full bytes go to the **Raw
Object Store** and every compaction carries a `raw_ref`.

Compaction never drops structured diagnostics to win tokens, even when the
compacted form is not shorter. Diagnostics are what the agent acts on.

### Cache intelligence

DWYT is **capability-advised**, never `enforced`. It shapes prompt order so a
provider's prefix cache has the best chance of a hit, and it reports observed
cache reads when a provider actually reports them.

It does not claim a cache hit it did not observe. DWYT does not control the HTTP
request a third-party client makes, so promising an enforced cache would be a
promise it cannot keep.

---

## The Brain

### Memory temperature

| Temperature | Meaning | Loaded |
|---|---|---|
| HOT | Project identity, constraints, active errors, current task state | Automatically |
| WARM | Architecture, decisions, conventions, module summaries | On relevance |
| COLD | Raw logs, session history, resolved and stale notes | Only by explicit reference |

Resolved and stale content is always COLD regardless of its kind. It stays
searchable; it is never loaded automatically.

### Search V2

The default search returns a small top-k (5) bounded by a token ceiling,
canonical knowledge first, with raw and stale notes excluded. The pre-v5 search
returned up to 30 notes ordered by modification time — which is how a vault full
of session snapshots ends up crowding out the decisions that matter.

### Snapshots

Snapshots are compact by default and identified by a **state hash** computed over
meaningful fields only. Timestamps and turn counters are excluded on purpose: a
snapshot should not be written merely because time passed.

A richer handoff is available when it is justified, but it is the exception.

---

## Memory lifecycle

The Housekeeper enforces the lifecycle. Its constraints, in the order they are
applied:

1. **Promote before deleting.** Durable knowledge is extracted into canonical
   notes first. Nothing expires while it still holds the only copy of something
   worth keeping.
2. **Cap sessions at 100.** Older sessions are consolidated, not silently lost.
3. **Apply TTLs by category.** Operational data expires; knowledge does not.
4. **Detect stale content by hash**, not by age. A note that still matches
   reality is not stale just because it is old.
5. **Dedup deterministically**, so the same run always produces the same result.

Two safety rules that are easy to get wrong:

- Notes without `dwyt_managed` **never** expire. DWYT does not garbage-collect
  what it did not create.
- An unknown note type is treated as `permanent`. Guessing wrong in the other
  direction destroys user data.

---

## Cost and telemetry

The north-star metric is **cost per successfully completed task**. It divides by
successes only. Dividing by all attempts would reward failing fast, which is the
opposite of what a token optimizer should optimize for.

Honesty rules baked into the telemetry layer:

- Unreported fields are **NULL**, never `0`. A missing measurement is not a zero
  measurement.
- Ratios with no data return **null**, never `0%`.
- **Estimated** and **observed** are separate columns and stay separate all the
  way to the dashboard.

OpenTelemetry export is implemented as direct **OTLP/HTTP JSON**, without
vendoring the OTel SDK. Pulling a large dependency tree into a binary whose whole
job is reducing overhead would be hard to justify; the wire format is stable and
small enough to emit directly.

---

## Benchmark

`dwyt bench` runs the deterministic benchmark (spec §69) over five scenarios and
four arms: `baseline`, `dwyt_v4`, `dwyt_v5_optimizer`, and
`dwyt_v5_optimizer_cache`.

It reports what it measured — input context, vault retrieval, tool output,
output budget and relative cost units — and explicitly declares what it did not:
completion rate, real cost, cache hit rate and latency all need a live agent loop
or a real provider response.

The report therefore ends with `claim_allowed: false`. No percentage from this
harness is a product claim, because a savings claim requires evidence that the
completion rate did not drop, and a fixture cannot produce that evidence.

```bash
dwyt bench           # table
dwyt bench --json    # full report
```

---

## Configuration

`dwyt config show|init|validate` manages `~/.dwyt/config/dwyt.json`.

`show` prints the **effective** configuration — defaults merged with the file —
because printing the raw file would hide the defaults filling every unset field,
which is exactly the question the command is asked to answer.

---

## MCP surface

### `dwyt_optimizer`

| Tool | Purpose |
|---|---|
| `dwyt_context_plan` | Budget + ranked retrieval plan for a task |
| `dwyt_context_status` | Current session state, budget and expansions |
| `dwyt_register_context` | Record what was actually delivered, enabling reuse |
| `dwyt_output_profile` | Visible-token contract for the current phase |
| `dwyt_cache_guidance` | Prompt assembly order and cache identity |
| `dwyt_compact_tool_output` | Compact a tool run, returning a `raw_ref` |
| `dwyt_get_raw` | Retrieve archived raw output by reference |
| `dwyt_report_usage` | Report observed provider usage (never estimated) |
| `dwyt_route` | Deterministic model routing by complexity and risk |
| `dwyt_housekeeper_status` / `dwyt_housekeeper_run` | Inspect and run memory lifecycle |
| `dwyt_memory_health` | Vault health: sizes, staleness, canonical coverage |

HTTP equivalents live under `/api/optimizer/*`, `/api/housekeeper/*` and
`/api/telemetry/*`.

### `dwyt_obsidian` and `dwyt_codebase`

See [Obsidian Law](obsidian-law.md) and [Codebase Law](codebase-law.md).

---

## Agent priority order

```
1. RTK              prefix shell commands with `rtk`
2. dwyt_codebase    structural code questions, before editing
3. dwyt_obsidian    memory: decisions, tasks, handoff context
4. dwyt_optimizer   budget, output contract, tool compaction, cache guidance
5. Headroom         optional transport compression, never a source of truth
```

Codex authenticated through ChatGPT/OAuth must not be routed through Headroom.
