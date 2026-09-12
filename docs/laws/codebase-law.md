# Codebase Law

`dwyt_codebase` is the primary source for the current structure of a project. Use
it when a task requires symbols, dependencies, call paths, routes, data flow, or
impact—not as a substitute for project memory or context-budget policy.

## Mandatory workflow

1. **Confirm coverage.** Check whether the project is indexed and whether the
   relevant source was fully covered. Refresh the index only when the task needs it.
2. **Discover structurally.** Use `search_graph` to locate symbols, `trace_path` to
   understand callers, callees, or data flow, and `get_code_snippet` after an exact
   qualified name is known. Use `query_graph` for bounded aggregate questions.
3. **Escalate incrementally.** Prefer map → symbol → relationships or range →
   snippet → full file. A full file is the last structural answer, not the default.
4. **Edit with impact awareness.** Do not rename, remove, or duplicate critical
   code based on filenames alone when the graph can provide evidence.
5. **Validate and remember.** Run relevant checks and save durable decisions or
   handoff context through the Obsidian workflow.

## Appropriate exceptions

Direct file reads and shell search are appropriate for documentation, configuration,
comments, lockfiles, generated assets, literal strings, or a graph that is missing
or insufficient. For commands, use RTK when available; its terminal reduction does
not replace structural analysis.

## Working with other capabilities

There is no universal order of tools. The task stage determines the capability:

- The Optimizer establishes a budget before broad retrieval.
- Codebase answers current code-structure questions.
- Obsidian retrieves and records durable knowledge, decisions, and task handoff.
- RTK reduces command output when a shell operation is needed.
- Headroom is an optional compatible transport proxy, never a source of truth.

For ownership boundaries, see the [architecture overview](../architecture/overview.md)
and the [Obsidian Law](obsidian-law.md).
