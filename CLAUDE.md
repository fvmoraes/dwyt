# DWYT — Engineering Conventions for AI Agents

This file is the source of truth for agents (Claude Code, Cursor, Copilot etc.)
working in this repo. Project-domain rules live in `docs/Rules/Rules.md`;
this file covers **how code is written and organized**.

## File size

- **Hard ceiling: 300 lines per file.** No exceptions for new files; existing
  oversized files (e.g. legacy `internal/brain/brain.go`) must not grow further
  and should be split when touched.
- **Soft target: 250 lines.** If you cross 250, ask whether the file has more
  than one clear responsibility — that's almost always the signal to split.
- Counts apply to source files (`.go`, `.ts`, `.py`, `.sh`, `.tsx`, etc.).
  Generated files, vendored code, and fixtures are exempt.

## Decomposition

- **Functions over inline blocks.** If a block inside a function reads as a
  named step ("validate input", "render header", "spawn daemon"), extract it
  into its own function with that name. The parent function should read like
  an outline of the steps it orchestrates.
- **One concept per file.** A file should be answerable by a single sentence
  ("installs Headroom", "detects the Obsidian binary"). When a file accretes a
  second concept, split before adding the third.
- **Helpers next to consumers, then promoted.** A helper used by one file
  stays in that file. The moment a second file needs it, move it to a shared
  location (`helpers.go`, `internal/<topic>/`, etc.) — never duplicate.
- **Same-package multi-file split is the default in Go.** Don't introduce a
  new package just to split a long file: `package install` across
  `install.go`, `headroom.go`, `obsidian.go` is preferred over creating
  `internal/installhelpers`.
- **Parent orchestrates, children execute.** A "mother" function/class wires
  inputs through specific helpers and surfaces failures; helpers do one thing
  and return errors, not log + exit.

## Reuse

- **Search before writing.** Before adding a path-detection block, fetch
  helper, retry loop, or HTTP client wrapper, grep the repo. The right place
  for it almost always already exists.
- **Single source of truth for cross-cutting data.** Lists like "where could
  Obsidian be installed" or "compatible Python versions" live in exactly one
  function. If two callers need it, they import — they don't redefine.
- **Lift on the second use, not the first.** Don't pre-abstract. Three
  similar lines is fine; two functions with copy-pasted bodies is the trigger.

## Bash specifics

- Bash files have the same 300-line ceiling. Extract repeated logic into
  functions defined at the top of the script.
- For installer scripts that ship via `curl | bash`, the orchestrator must
  stay self-contained but can source helpers if it bootstraps them itself.
  See `install.sh` + `install-lib/` for the working pattern: orchestrator
  resolves a sibling `install-lib/` (clone) or downloads the lib files into
  a tempdir (piped), then sources them. One concept per lib file
  (`output.sh`, `platform.sh`, `download.sh`, etc.).

## When refactoring an oversized file you have to touch

1. Identify the cohesive groups (by domain, by tool, by lifecycle phase).
2. Move each group into its own file in the same package.
3. Keep the original file as the "entry point": shared types, package doc,
   and small dispatcher functions. It should end up the smallest of the set.
4. Run the full build and tests for the package; don't ship a partial split.

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
