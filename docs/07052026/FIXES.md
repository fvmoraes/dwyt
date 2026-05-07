# Fixes — 2026-05-07

## Scope

Implemented the rules from `docs/Rules/Rules.md` across status metrics, generated agent instructions, Kiro Power, Obsidian vault seeds, CLI wording, dashboard types, and documentation.

## Token Savings

- Added auditable token fields to tool details:
  - `without_dwyt_tokens`
  - `with_dwyt_tokens`
  - `tokens_saved`
  - `estimation_source`
- Codebase now estimates savings from index metadata when nodes/edges are available.
- Obsidian keeps its vault-size estimate and now participates in the global dashboard summary.
- RTK and Headroom remain real metrics from their native sources.

## Agent Laws

- Generated instruction files now use the priority order:
  1. RTK
  2. Codebase MCP
  3. Obsidian MCP
  4. Headroom
- The Codebase Law requires graph tools for structural code work.
- The Obsidian Law requires vault lookup before relevant work and context saving at task end.
- DWYT-managed blocks remain idempotent and preserve user content outside markers.

## Kiro

- Kiro Power frontmatter now matches the required `dwyt-power` metadata.
- Steering files reinforce RTK, Codebase, Obsidian, Headroom, and the Codex ChatGPT/OAuth Headroom exception.
- Status includes an activation hint when manual local-path activation is needed.
- Workspace MCP documentation and code use `.kiro/settings/mcp.json` as primary and `.kiro/mcp.json` as legacy compatibility.

## Obsidian Vaults

- New vaults are seeded with `instructions/`, `maps/`, `templates/`, `decisions/`, `tasks/`, `debug/`, `context/`, `knowledge/`, and `logs/`.
- Seeded notes include internal links to avoid loose files.
- Decisions and tasks append to folder indexes.
- Final context snapshots are saved under `context/`.
- Debug and error records are routed to `debug/`.

## Safety

- CLI reinstall/uninstall text now says vaults are preserved.
- Cleanup code continues to protect `~/.dwyt/projects/`.
- Documentation no longer describes uninstall as removing Obsidian vaults.
