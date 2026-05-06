# DWYT — GitHub Copilot

## Priority:
1. **Obsidian FIRST**: check project vault before heavy file reads.
Search: GET http://localhost:2737/api/obsidian/search?q=<query>
Save: POST http://localhost:2737/api/obsidian/save
2. **Headroom**: compression auto-detected via OPENAI_BASE_URL / ANTHROPIC_BASE_URL
3. **RTK**: prefix shell commands with rtk
4. **Codebase MCP**: structural exploration only when needed

Save summaries after important changes via Obsidian API.
<!-- dwyt:headroom-proxy-start -->
**Headroom proxy is ACTIVE** on http://127.0.0.1:8787 — use OPENAI_BASE_URL and ANTHROPIC_BASE_URL env vars automatically.
<!-- dwyt:headroom-proxy-end -->
