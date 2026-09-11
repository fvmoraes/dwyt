# DWYT architecture overview

DWYT is a single-binary context optimizer. The embedded dashboard and HTTP API run
in the daemon; MCP clients connect over stdio and use the daemon or Codebase service
for their state. The design keeps policy, durable knowledge, and code truth separate
so one component does not silently make another component's decisions.

## First-party MCPs

These are the three first-party MCP servers documented by DWYT:

| MCP server | Responsibility | Does not own |
|---|---|---|
| `dwyt_optimizer` | Context budget, Token ROI, reuse, output contracts, cache guidance, routing, compaction, raw store | Project knowledge or code truth |
| `dwyt_obsidian` | Canonical knowledge, decisions, task handoff, session memory, lifecycle | Code structure or context-budget policy |
| `dwyt_codebase` | Symbols, dependencies, routes, call paths, impact, and source snippets | Memory history or context-budget policy |

RTK is a terminal CLI, not an MCP. Headroom is an optional transport proxy, not a
source of truth. The Housekeeper and Memory Compiler are daemon capabilities that
manage the Brain's lifecycle and consolidate durable knowledge.

## Ownership model

```text
AI client
  │ stdio
  ├── dwyt_optimizer ──HTTP──▶ DWYT daemon ──▶ session state and policy
  ├── dwyt_obsidian  ──HTTP──▶ DWYT daemon ──▶ project vault on disk
  └── dwyt_codebase  ──HTTP──▶ Codebase service ──▶ code knowledge graph
```

The Optimizer decides how much context a task may spend. Codebase and Obsidian
supply evidence inside that envelope. The Brain is durable memory, not a cache;
operational logs and sessions may expire only after reusable knowledge is promoted.

## State boundaries

- **Daemon session state** is shared by clients for one task, which makes reuse
  possible across IDEs and prevents per-client budget drift.
- **Vault state** lives under `~/.dwyt/projects/<hash>_<project-name>/`; unmanaged
  notes are protected from automatic deletion.
- **Code graph state** is current structural evidence and is retrieved on demand.
- **Transport state** remains optional: Headroom availability never defines whether
  project knowledge or code structure is true.

## Read the focused contracts

- [Runtime and lifecycle](runtime.md) covers startup, service ownership, status, and
  effective ports.
- [Context optimization](context-optimization.md) covers retrieval, budgets,
  compaction, output, cache, telemetry, and startup tax.
- [Optimizer Law](../laws/optimizer-law.md), [Codebase Law](../laws/codebase-law.md),
  and [Obsidian Law](../laws/obsidian-law.md) state the mandatory workflows.

There is no universal tool ordering. Each capability is used in the stage where its
evidence or operation is needed, with the Optimizer setting the retrieval envelope.
