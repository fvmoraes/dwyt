# DWYT Steering

## Priority:
1. **Obsidian FIRST**: check project vault before reading files.
   Search: GET http://localhost:2737/api/obsidian/search?q=<query>
   Save: POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"..."}
2. **Headroom**: auto-detected via env vars OPENAI_BASE_URL / ANTHROPIC_BASE_URL
3. **RTK**: prefix all shell commands with rtk
4. **Codebase MCP**: structural exploration only — use after Obsidian

Save important decisions to Obsidian after completion.
