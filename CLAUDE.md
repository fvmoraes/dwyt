# DWYT — Don't Waste Your Tokens

## Priority order (follow strictly):
1. **Obsidian FIRST** — before any file read or context rebuild:
   - Search: GET http://127.0.0.1:2737/api/brain/search?q=your+query
   - Save: POST http://127.0.0.1:2737/api/brain/save {"type":"decision","content":"..."}
   - Types: command, decision, action, error, note
   - Never mix vaults between projects — isolation is automatic.

2. **Headroom** — auto-detected via OPENAI_BASE_URL / ANTHROPIC_BASE_URL env vars.
   If set, use them. If not, use default API endpoints. No manual config needed.

3. **RTK** — always prefix shell commands with rtk. Reduces output 60-90%.

4. **Codebase MCP** — ONLY when you need structural code understanding.
   Prefer Obsidian context first. Use search_graph, trace_call_path, get_code_snippet.
<!-- dwyt:headroom-proxy-start -->
**Headroom proxy is ACTIVE** on http://127.0.0.1:8787 — use OPENAI_BASE_URL and ANTHROPIC_BASE_URL env vars automatically.
<!-- dwyt:headroom-proxy-end -->
