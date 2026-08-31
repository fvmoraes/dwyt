# Obsidian Law

The Obsidian vault is the official project memory in DWYT. Agents must use it to recover context, preserve decisions, track work, and hand off useful state to future agents.

Obsidian is memory, not code structure. For code symbols, dependencies, call paths, and impact analysis, follow the [Codebase Law](CODEBASE-LAW.md).

## Mandatory Workflow

1. **Before relevant work**, consult the vault.
   - Search existing notes: `GET /api/obsidian/search?q=<query>`
   - Rebuild or read the current summary: `POST /api/obsidian/summarize`
   - Look for decisions, open tasks, debug notes, and previous context.

2. **During work**, save meaningful state.
   - Decisions and ADRs: `POST /api/obsidian/save` with `type: "decision"`
   - Tasks, progress, and status: `POST /api/obsidian/save` with `type: "task"`
   - Investigation notes and failures: `POST /api/obsidian/save` with `type: "debug"`
   - General notes: `POST /api/obsidian/save` with `type: "note"`

3. **At the end of relevant work**, save complete context.
   - Endpoint: `POST /api/obsidian/context`
   - Required fields: `client`, `user_request`, `summary`, `files`, `decisions`, `actions`, `commands`, `errors`, `outcome`, `next_steps`, and `context`.

Never finish a relevant task without saving context to Obsidian. If the MCP or API is unavailable, do not block the task or recreate vaults; report the failure and retry saving context when the service is available.

## MCP Wiring

The Obsidian MCP is embedded in the main `dwyt` binary and is launched by
AI clients as `dwyt obsidian-mcp` (stdio). It talks to the dashboard at
`http://localhost:2737/api` via the `DWYT_API_URL` env var. No renamed
copy of the binary is required anymore — Windows installs that previously
failed because the installer could not write `%APPDATA%\dwyt\bin\dwyt-obsidian-mcp.exe`
now work out of the box. Older registries pointing at the legacy
`dwyt-obsidian-mcp` file are auto-rewritten on the next configure cycle.

## Vault Identity

Every DWYT-managed vault lives at `~/.dwyt/projects/<hash>_<project_name>/`
(for example `1597b5fc9bfb_dwyt`). The 12-character hash is the only internal
identifier — the project-name suffix exists purely so humans (and the
Obsidian vault picker) can tell vaults apart. Inside each vault, DWYT keeps
a metadata file at `.dwyt/vault.json` recording `version`, `project_hash`,
`project_name`, and `directory_name`; it is the durable source of truth for
"who owns this vault" and is what makes legacy-directory renames safe.

Legacy vaults named only with the hash are migrated automatically at startup
when DWYT can resolve the project name reliably (DB registry, then the
vault's own metadata). Vaults with no trustworthy association are never
renamed — they stay functional under the old name and are surfaced in the
dashboard's "Vault migration" card for the user to resolve manually. Notes,
plugins, `.obsidian/` settings, and history are always preserved across a
rename; a backup of Obsidian's `obsidian.json` is written before any
registry update.

## Vault Quality Standard

Vault files should be useful inside Obsidian itself:

- use clear headings and frontmatter;
- prefer internal links such as `[[instructions/obsidian-law]]`, `[[instructions/codebase-law]]`, `[[maps/project-map]]`, `[[decisions/index]]`, and `[[tasks/index]]`;
- keep decisions, tasks, debug notes, and context in their folders;
- explain why decisions were made, not only what changed;
- avoid loose, unlinked files when a map or index should reference them.

## Default Vault Structure

New DWYT vaults are seeded with:

```txt
obsidian/
├── index.md
├── context.md
├── instructions/
│   ├── obsidian-law.md
│   └── codebase-law.md
├── maps/
│   └── project-map.md
├── templates/
│   ├── decision-template.md
│   ├── task-template.md
│   └── session-context-template.md
├── decisions/
│   └── index.md
├── tasks/
│   └── index.md
├── debug/
│   └── index.md
├── context/
│   └── *.md
├── knowledge/
└── logs/
    ├── sessions/
    ├── errors/
    └── commands/
```

Legacy `decisions.md` and `tasks.md` may exist as compatibility pointers, but new entries are routed to `decisions/index.md` and `tasks/index.md`.

## Persistence Rule

Project vaults live under `~/.dwyt/projects/<id>/`. Install, repair, reinstall, clean, reset, and uninstall flows must preserve `~/.dwyt/projects/` and must never delete vaults, notes, project memories, or history automatically.

## Context Payload

Agents should save final context like this:

```json
{
  "client": "codex",
  "user_request": "...",
  "summary": "...",
  "files": ["..."],
  "decisions": ["..."],
  "actions": ["..."],
  "commands": ["..."],
  "errors": ["..."],
  "outcome": "...",
  "next_steps": ["..."],
  "context": "Use links such as [[decisions/index]] and [[instructions/codebase-law]]."
}
```
