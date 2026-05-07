# DWYT Steering

## Priority:
1. **Obsidian FIRST**: check project vault before reading files.
   Search: GET http://localhost:2737/api/obsidian/search?q=<query>
   Summarize: POST http://localhost:2737/api/obsidian/summarize
   Save: POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"..."}
   Save task/status: POST http://localhost:2737/api/obsidian/save {"type":"task","content":"..."}
   Save complete context at task end: POST http://localhost:2737/api/obsidian/context
   Required context fields: user_request, summary, files, decisions, actions, commands, errors, outcome, next_steps, context.
   Keep the vault rich with folders, links, templates, and instructions.
2. **Headroom**: auto-detected via env vars OPENAI_BASE_URL / ANTHROPIC_BASE_URL
3. **RTK**: prefix all shell commands with rtk
4. **Codebase MCP**: structural exploration only — use after Obsidian

Never finish a task without saving context to Obsidian.

<!-- dwyt:instructions:start -->
#dwyt

# DWYT Steering

## Priority:
1. **Obsidian FIRST**: check project vault before reading files.
   Search: GET http://localhost:2737/api/obsidian/search?q=<query>
   Summarize: POST http://localhost:2737/api/obsidian/summarize
   Save: POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"..."}
   Save task/status: POST http://localhost:2737/api/obsidian/save {"type":"task","content":"..."}
   Save context at task end: POST http://localhost:2737/api/obsidian/context {"client":"kiro","user_request":"...","summary":"...","files":["..."],"decisions":["..."],"actions":["..."],"commands":["..."],"errors":["..."],"outcome":"...","next_steps":["..."],"context":"..."}
   Keep the vault rich, interlinked, and organized with folders, links, templates, and instructions.
2. **Headroom**: auto-detected via env vars OPENAI_BASE_URL / ANTHROPIC_BASE_URL
3. **RTK**: prefix all shell commands with rtk
4. **Codebase MCP**: structural exploration only — use after Obsidian

Never finish a task without saving context to Obsidian.
<!-- dwyt:instructions:end -->
