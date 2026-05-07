# DWYT — GitHub Copilot

## Priority:
1. **Obsidian FIRST**: check project vault before acting.
Search: GET http://localhost:2737/api/obsidian/search?q=<query>
Summarize: POST http://localhost:2737/api/obsidian/summarize
Save decision: POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"..."}
Save task/status: POST http://localhost:2737/api/obsidian/save {"type":"task","content":"..."}
Save complete context at task end: POST http://localhost:2737/api/obsidian/context
Required context fields: user_request, summary, files, decisions, actions, commands, errors, outcome, next_steps, context.
Keep the vault rich with folders, links, templates, and instructions.
2. **Headroom**: compression auto-detected via OPENAI_BASE_URL / ANTHROPIC_BASE_URL
3. **RTK**: prefix shell commands with rtk
4. **Codebase MCP**: structural exploration only when needed

Never finish a task without saving context to Obsidian API.
<!-- dwyt:headroom-proxy-start -->
**Headroom proxy is ACTIVE** on http://127.0.0.1:8787 — use OPENAI_BASE_URL and ANTHROPIC_BASE_URL env vars automatically.
<!-- dwyt:headroom-proxy-end -->
