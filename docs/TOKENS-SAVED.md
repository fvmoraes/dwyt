# Tokens Saved

DWYT exposes token savings in two categories: real metrics reported by tools, and transparent local estimates where native telemetry does not exist yet.

## Sources

| Tool | Source | Kind |
|---|---|---|
| RTK | `rtk gain` / `rtk gain --project` | real metric |
| Headroom | Headroom `/stats` | real metric |
| Codebase MCP | index metadata such as nodes and edges | local estimate |
| Obsidian MCP | vault markdown file count and total bytes | local estimate |

The dashboard global summary includes all four tools. Tool cards expose enough fields to make the calculation auditable:

- `without_dwyt_tokens`;
- `with_dwyt_tokens`;
- `tokens_saved`;
- `estimation_source`.

## Codebase Estimate

When a project has a Codebase index, DWYT estimates the manual cost of exploring the repository from graph metadata and compares it with a smaller structural-query cost:

```txt
tokens_saved = max(manual_tokens - mcp_tokens, 0)
```

Small or unindexed projects stay neutral instead of showing artificial savings. The estimate is labeled as local until Codebase MCP provides native telemetry.

## Obsidian Estimate

For Obsidian, DWYT estimates the manual context cost from vault markdown bytes:

```txt
manual_tokens ~= total_markdown_bytes / 4
tokens_saved = max(manual_tokens - mcp_overhead_tokens, 0)
```

Empty or newly-created vaults stay neutral. The estimate is refreshed with status data after vault actions such as save, search, summarize, open, and context capture.

## Global Summary

The backend calculates a global token summary from every tool detail with token fields. The frontend also has a defensive calculation path so the UI remains stable with older or partial API responses.

The global numbers are meant for operational visibility, not billing precision:

- RTK and Headroom remain authoritative where available.
- Codebase and Obsidian are conservative estimates.
- Unknown or inactive tools should not make the dashboard fail.
