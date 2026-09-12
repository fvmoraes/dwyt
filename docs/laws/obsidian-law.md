# Obsidian Law

`dwyt_obsidian` is DWYT's durable project Brain. Use it to recover relevant context,
preserve decisions, track work, and hand off state that should outlive the current
session. It does not replace current code structure, which belongs to Codebase.

## Mandatory workflow

1. **Before relevant work**, retrieve canonical project context and any focused
   decision, constraint, or current-task note needed to act safely.
2. **During work**, record meaningful decisions, investigation outcomes, and task
   state through the appropriate canonical or session entry.
3. **At the end**, save compact context containing the request, summary, affected
   files, decisions, actions, commands, errors, outcome, next steps, and reusable
   handoff context. Failure to save must be reported, not hidden.

## Retrieval and lifecycle

Search V2 is a bounded default read path: canonical knowledge first, small top-k,
and raw or stale notes excluded unless explicitly requested. Temperature determines
default loading:

| Temperature | Contents |
|---|---|
| HOT | Project identity, constraints, active errors, current task state |
| WARM | Architecture, decisions, conventions, module summaries |
| COLD | Raw logs, session history, resolved and stale notes |

The Housekeeper promotes reusable knowledge before retirement, caps managed
sessions, applies category TTLs, detects staleness by content hash, and deduplicates
deterministically. Notes that DWYT did not manage and unknown note types are never
assumed disposable.

## Vault identity and safety

Managed vaults live at `~/.dwyt/projects/<hash>_<project-name>/`. The hash is the
internal identifier; the suffix makes the vault understandable to people. Legacy
hash-only vaults are renamed only with reliable identity evidence. Installation,
repair, cleanup, reset, and uninstall preserve project vaults, notes, and history.

## Working with other capabilities

Obsidian supplies memory; it does not decide the context budget. The Optimizer owns
that policy, and Codebase owns current structural truth. See the [Codebase Law](codebase-law.md)
and [architecture overview](../architecture/overview.md) for those boundaries.
