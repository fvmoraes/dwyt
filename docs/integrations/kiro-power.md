# Kiro Power

DWYT creates a local Kiro Power that packages project guidance, MCP configuration,
and steering files for consistent use of DWYT.

## Paths and activation

The canonical local Power is generated at:

```text
~/.dwyt/powers/dwyt-power
```

When possible DWYT links it at:

```text
~/.kiro/powers/dwyt-power
```

If automatic linking is unavailable, the dashboard and API return an activation hint
with the local path. Workspace MCP configuration is written primarily to
`.kiro/settings/mcp.json`; `.kiro/mcp.json` is updated only as a legacy-compatible
path. Existing user servers are merged and preserved.

## Generated structure

```text
~/.dwyt/powers/dwyt-power/
├── POWER.md
├── mcp.json
└── steering/
    ├── dwyt-context.md
    ├── codebase.md
    ├── obsidian.md
    ├── rtk.md
    └── headroom.md
```

`mcp.json` lists the three first-party MCP servers: `dwyt_optimizer`,
`dwyt_codebase`, and `dwyt_obsidian`. RTK is a CLI convention and Headroom is an
optional transport proxy, so both are expressed through steering rather than as MCP
servers.

## Required guidance

The Power explains the role of each capability in its applicable task stage:

- establish an Optimizer context plan before broad retrieval;
- use Codebase for structural code questions;
- retrieve and save durable task context through Obsidian;
- use RTK for shell output when a command is needed;
- use Headroom only for compatible clients, never for Codex ChatGPT/OAuth;
- preserve vaults and user-owned configuration outside DWYT-managed blocks.

This is stage-scoped collaboration, not a global priority order. For the durable
contracts, see the [documentation index](../readme.md).

## Status API

```text
GET  /api/kiro/power/status
POST /api/kiro/power/refresh
```

The status response includes installation state, symlink status, MCP binary
availability, activation hints, and actionable errors.
