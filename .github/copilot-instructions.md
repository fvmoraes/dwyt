# DWYT — GitHub Copilot

## Priority

1. **RTK**: prefix shell commands with `rtk` when running terminal commands.
2. **Codebase MCP**: use `search_graph`, `trace_path` and `get_code_snippet` before analyzing or changing real code structure.
3. **Obsidian MCP**: recover project memory before relevant work and save useful context during and after the task.
4. **Headroom**: use only as compatible proxy/cache optimization when the AI client supports base URL configuration.

Headroom is not a source of truth, and Codex authenticated via ChatGPT/OAuth must not use Headroom.

<!-- dwyt:headroom-proxy-start -->
**Headroom proxy is ACTIVE** on http://127.0.0.1:8787 — use OPENAI_BASE_URL and ANTHROPIC_BASE_URL env vars automatically.
<!-- dwyt:headroom-proxy-end -->

<!-- dwyt:instructions:start -->
#dwyt

# DWYT — GitHub Copilot

## Priority Order

1. **RTK — terminal compression**
   - Prefix shell commands with `rtk`.
   - In command chains, prefix each command segment.
   - RTK optimizes terminal output; it does not replace Codebase or Obsidian.

2. **Codebase MCP — Lei do Codebase**
   - Use Codebase MCP whenever you need to understand, validate, diagnose or change real code structure.
   - Prefer `search_graph` for symbols, modules, services, handlers, components and relationships.
   - Prefer `trace_path` for calls, dependencies, data flow and impact.
   - Prefer `get_code_snippet` for reading exact source before proposing or editing code.
   - Avoid broad manual file search as the first strategy when Codebase MCP is available.

3. **Obsidian MCP — Lei do Obsidian**
   - The project vault is the persistent memory of the project.
   - Before relevant work, search existing notes and summarize the vault.
   - During work, save decisions, task status, debug notes and useful context.
   - At the end of relevant tasks, save a complete context snapshot.
   - Use internal links such as `[[instructions/obsidian-law]]`, `[[instructions/codebase-law]]`, `[[decisions/index]]` and `[[tasks/index]]` when recording context.

4. **Headroom — compatible optimization**
   - Use Headroom only when the client supports `OPENAI_BASE_URL` or `ANTHROPIC_BASE_URL`.
   - Do not use Headroom with Codex authenticated via ChatGPT/OAuth.
   - Treat Headroom as optimization only; never as memory, code structure or source of truth.

## Obsidian Context Payload

When saving final context, include:

- `client`;
- `user_request`;
- `summary`;
- `files`;
- `decisions`;
- `actions`;
- `commands`;
- `errors`;
- `outcome`;
- `next_steps`;
- `context`.

## Safety

- Preserve user content outside DWYT-managed blocks.
- Do not duplicate DWYT blocks.
- Do not delete Obsidian vaults, project memories, notes or history.
- Validate changes before concluding.
<!-- dwyt:instructions:end -->
