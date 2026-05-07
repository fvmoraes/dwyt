# DWYT Steering

<!-- dwyt:instructions:start -->
#dwyt

# DWYT — Don't Waste Your Tokens

## Priority

1. **RTK**: prefix shell commands with `rtk`.
2. **Codebase MCP**: use the code graph before diagnosing, refactoring, or editing real code structure.
3. **Obsidian MCP**: search/summarize project memory before relevant work, save decisions/tasks during work, and save context at task end.
4. **Headroom**: use only when compatible env vars point to the local proxy; Codex ChatGPT/OAuth must not use Headroom.

## Codebase Law

Validate/index the project when needed, then use `search_graph`, `trace_path`, and `get_code_snippet` for symbols, calls, dependencies, flows, and impact.

## Obsidian Law

Use the project vault as durable memory:

- `GET http://localhost:2737/api/obsidian/search?q=<query>`
- `POST http://localhost:2737/api/obsidian/summarize`
- `POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"[[decisions]] ..."}`
- `POST http://localhost:2737/api/obsidian/save {"type":"task","content":"[[tasks]] ..."}`
- `POST http://localhost:2737/api/obsidian/context`

Final context must include `client`, `user_request`, `summary`, `files`, `decisions`, `actions`, `commands`, `errors`, `outcome`, `next_steps`, and `context`.
<!-- dwyt:instructions:end -->
