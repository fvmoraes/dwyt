<!-- DWYT:START -->
#dwyt

# DWYT - Don't Waste Your Tokens

DWYT coordinates RTK, Codebase MCP, Obsidian MCP, and Headroom without overwriting user-managed files.

## Required Rules

Always use the DWYT Codebase MCP to understand the real repository structure before making changes.
Always use the DWYT Obsidian MCP to read and persist project context.
Always save relevant decisions, findings, fixes, and summaries into the project brain.
Before every final response, save a session context snapshot with the Obsidian MCP.
Never rely only on grep/glob when MCP tools are available.
Keep project context under `~/.dwyt`.
Never hardcode machine-specific absolute paths in shared markdown instructions.

## Priority Order

1. RTK
   - Prefix shell commands with `rtk`: `rtk git status`, `rtk go test ./...`, `rtk npm run build`.
   - In command chains, prefix each segment.

2. Codebase MCP
   - Before diagnosing, refactoring, or editing structural code, validate that the project is indexed.
   - Use `search_graph` to locate symbols, routes, handlers, components, modules, and relationships.
   - Use `trace_path` for calls, flows, dependencies, and impact.
   - Use `get_code_snippet` before applying changes.
   - Avoid grep/glob/find as the first strategy when MCP tools are available.

3. Obsidian MCP
   - Before relevant work, search notes and rebuild or read the summary:
     `GET http://localhost:2737/api/obsidian/search?q=<query>`
     `POST http://localhost:2737/api/obsidian/summarize`
   - During the work, save decisions, findings, and tasks:
     `POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"[[decisions]] ..."}`
     `POST http://localhost:2737/api/obsidian/save {"type":"task","content":"[[tasks]] ..."}`
   - At the end of every task/session, save complete context before the final answer.
     Prefer the MCP tool `obsidian_save_context`; in Codex it may appear as `mcp__obsidian__obsidian_save_context`.
     Set `client` to the current client: `codex`, `opencode`, `claude`, `cursor`, `kiro`, `copilot`, `windsurf`, or `continue`.
     This rule applies to Codex, OpenCode, Claude, Cursor, Kiro, Copilot, Windsurf, and Continue.
     If the MCP tool is unavailable, fall back to:
     `POST http://localhost:2737/api/obsidian/context`
     If saving fails, mention the failure in the final response.

4. Headroom
   - Use Headroom only when `OPENAI_BASE_URL` or `ANTHROPIC_BASE_URL` points to the compatible local proxy.
   - Never route Codex through Headroom when Codex is authenticated through ChatGPT/OAuth.
   - If Headroom is inactive or unavailable, use the standard endpoints.

## Codebase Law

When you need to understand, validate, diagnose, or change the real code structure, consult the DWYT Codebase MCP. The indexed graph is the primary source for files, symbols, dependencies, calls, paths, and impact. Do not create duplicate code, remove files, or move components without checking graph relationships and impact.

## Obsidian Law

The Obsidian vault at `~/.dwyt/projects/<id>/` is the official durable memory for the project. Keep notes with internal links such as `[[index]]`, `[[maps/project-map]]`, `[[instructions/obsidian-law]]`, and `[[instructions/codebase-law]]`. Never delete vaults, projects, notes, or history as an automatic repair step.

Minimum payload for saving context:

```json
{
  "client": "<client>",
  "user_request": "...",
  "summary": "...",
  "files": ["..."],
  "decisions": ["..."],
  "actions": ["..."],
  "commands": ["..."],
  "errors": ["..."],
  "outcome": "...",
  "next_steps": ["..."],
  "context": "..."
}
```

## User Files

Treat instruction files as safe append-only files: create the DWYT block if missing, update only the DWYT-managed block, and preserve all content outside that block.

## Validation

Before completing changes, run the relevant validation: Go tests, frontend build/lint when available, and manual checks for installed, inactive, and launch-on-demand states.
<!-- DWYT:END -->
