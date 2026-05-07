# Codebase Law

The Codebase MCP knowledge graph is the primary source for the current structure of a project. Agents must use it whenever they need to understand, validate, diagnose, refactor, or change real code.

Codebase is structure, not memory. For decisions, task history, and handoff context, follow the [Obsidian Law](OBSIDIAN-LAW.md).

## Mandatory Workflow

1. **Validate the index**
   - Confirm the project is indexed before structural work.
   - If the index is missing or stale, use the dashboard or MCP index command to refresh it.

2. **Discover through the graph**
   - Use `search_graph` for symbols, modules, services, handlers, components, routes, variables, and relationships.
   - Use `trace_path` for callers, callees, dependencies, data flow, and impact.
   - Use `get_code_snippet` for exact source once the graph returns the qualified name.
   - Use `query_graph` for advanced multi-hop or aggregate questions.

3. **Edit with impact awareness**
   - Do not change critical code based only on filename guesses.
   - Do not create duplicate implementations without checking for existing symbols and patterns.
   - Do not remove, rename, or move files without tracing impact when the graph is available.

4. **Validate and remember**
   - Run the relevant tests and builds.
   - Save decisions and final context in Obsidian using links such as `[[instructions/codebase-law]]` and `[[decisions/index]]`.

## When Shell Search Is Acceptable

Use `rg`, `find`, or direct file reads when the task is about:

- string literals, messages, comments, or non-code files;
- docs, config files, lockfiles, shell scripts, or generated assets;
- cases where the Codebase MCP is unavailable or returns insufficient results.

Even then, prefer RTK for shell commands: `rtk grep`, `rtk find`, `rtk read`, and `rtk git status`.

## Relationship With Other DWYT Tools

DWYT tool priority for agents is:

1. **RTK** for shell commands and terminal output compression.
2. **Codebase MCP** for current code structure.
3. **Obsidian MCP** for memory, decisions, tasks, and handoff context.
4. **Headroom** for compatible API proxy/cache optimization.

Headroom is not a source of truth. Codex authenticated through ChatGPT/OAuth must not use Headroom.
