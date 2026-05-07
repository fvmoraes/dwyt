# Validation — 2026-05-07

## Backend

Run from `core/`:

```bash
rtk go test ./...
```

Expected result:

- all Go packages pass;
- status handlers serialize the new token fields;
- `/api/health` includes the running daemon version;
- CLI startup restarts stale daemons when the running daemon version is missing or different;
- Obsidian vault tests confirm the new linked folder structure;
- Kiro Power tests confirm frontmatter and steering priorities;
- integration tests confirm DWYT blocks remain idempotent.

## Frontend

Run from `core/web/`:

```bash
rtk npm run build
rtk npm run lint
```

Expected result:

- TypeScript build passes;
- dashboard accepts token fields from backend;
- global summary includes RTK, Headroom, Codebase, and Obsidian;
- lint passes when the script exists.

## Manual Checks

1. Run `dwyt .` in a project.
2. Open the dashboard.
3. Confirm the Codebase card shows neutral savings before indexing and savings after indexing.
4. Confirm the Obsidian card shows savings only when vault content exists.
5. Confirm the global summary includes Codebase and Obsidian.
6. Confirm Kiro Power status shows a manual activation hint when the local Power is not linked.
7. Confirm `dwyt reinstall` and `dwyt uninstall` messages do not promise vault deletion.

## Safety Checks

- `~/.dwyt/projects/` must remain intact after reinstall/uninstall flows.
- Existing user MCP servers in `.kiro/settings/mcp.json` and `.kiro/mcp.json` must be preserved.
- DWYT instruction blocks must not duplicate.
- Content outside DWYT-managed blocks must not be modified by integration generation.
