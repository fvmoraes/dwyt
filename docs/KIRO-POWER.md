# Kiro Power

DWYT creates a local Kiro Power that packages the project rules, MCP configuration, and steering files Kiro needs to use DWYT consistently.

The implementation follows Kiro's current Power shape: a Power directory has `POWER.md` and can include `mcp.json` and `steering/` files. Kiro workspace MCP configuration is written to `.kiro/settings/mcp.json`, with `.kiro/mcp.json` kept only as a legacy compatibility path.

References:

- Kiro Powers: <https://kiro.dev/docs/powers/>
- Creating Powers: <https://kiro.dev/docs/powers/create/>
- Kiro MCP configuration: <https://kiro.dev/docs/mcp/configuration/>

## Paths

DWYT generates the canonical local Power at:

```txt
~/.dwyt/powers/dwyt-power
```

When possible, DWYT links it into:

```txt
~/.kiro/powers/dwyt-power
```

If automatic linking cannot be guaranteed, the dashboard and API expose an activation hint:

```txt
Add power from local path: ~/.dwyt/powers/dwyt-power
```

## Generated Files

```txt
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

Only real MCP servers are placed in `mcp.json`: Codebase and Obsidian. RTK is a CLI convention, and Headroom is an API proxy/cache optimization, so both are expressed through steering instructions.

## POWER.md Frontmatter

```yaml
---
name: dwyt-power
displayName: DWYT Project Context
description: DWYT integration for Codebase MCP, Obsidian memory, RTK command compression and compatible Headroom usage.
keywords:
  - dwyt
  - codebase
  - obsidian
  - mcp
  - memory
  - project memory
  - token savings
  - repo analysis
  - arquitetura
  - refatoracao
  - debugging
  - documentacao
  - contexto do projeto
author: DWYT
---
```

## Workspace MCP

DWYT writes Kiro workspace MCP config to:

```txt
.kiro/settings/mcp.json
```

It also updates this legacy path for compatibility:

```txt
.kiro/mcp.json
```

Existing user MCP servers are merged and preserved. DWYT entries are idempotent and should not duplicate on repeated setup or repair.

## Required Guidance

The Power and steering files must reinforce:

- RTK first for shell commands;
- Codebase MCP for structural code understanding;
- Obsidian MCP for persistent memory and final context;
- Headroom only when compatible;
- no Headroom for Codex ChatGPT/OAuth;
- no automatic deletion of Obsidian vaults or project history.
