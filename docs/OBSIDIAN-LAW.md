# Obsidian Law

The Obsidian vault is the official memory of every DWYT project. Agents must treat it as the durable source of project context, decisions, task state, and handoff notes.

## Mandatory Agent Workflow

Every interaction must follow this order:

1. **Before acting**, consult Obsidian for context.
   - Search existing notes: `GET /api/obsidian/search?q=<query>`
   - Rebuild/read the current vault summary: `POST /api/obsidian/summarize`

2. **During work**, save important project state.
   - Technical decisions and ADRs: `POST /api/obsidian/save` with `type: "decision"`
   - Tasks, progress, and status: `POST /api/obsidian/save` with `type: "task"`
   - Errors, commands, sessions, and notes: use the matching entry type when useful.

3. **At the end of every task**, save complete context.
   - Endpoint: `POST /api/obsidian/context`
   - Required fields: `summary`, `user_request`, `files`, `decisions`, `actions`, `commands`, `errors`, `outcome`, `next_steps`, and `context`.

Never finish a task without saving context to Obsidian.

## Vault Quality Standard

The vault must be rich, interlinked, and organized enough for a future agent to continue without reconstructing history.

Use:

- notes with clear headings and frontmatter;
- folders for knowledge, logs, sessions, instructions, templates, and maps;
- internal links such as `[[decisions]]`, `[[tasks]]`, and `[[instructions/obsidian-law]]`;
- templates for decisions, tasks, and session context;
- enough detail to explain why decisions were made, not only what changed.

## Default Vault Structure

New DWYT vaults are seeded with:

```txt
obsidian/
├── index.md
├── context.md
├── decisions.md
├── tasks.md
├── instructions/
│   └── obsidian-law.md
├── maps/
│   └── project-map.md
├── templates/
│   ├── decision-template.md
│   ├── task-template.md
│   └── session-context-template.md
├── knowledge/
└── logs/
    ├── sessions/
    ├── errors/
    └── commands/
```

## Context Payload

Agents should save a complete context payload like this:

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
  "context": "Important details for future agents..."
}
```
