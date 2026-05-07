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
