# DWYT — Documentation index

Everything there is to know about DWYT, organized by what you are trying to
do. Platform guides live in their own folders: [linux/](linux/readme.md),
[macos/](macos/readme.md) and [windows/](windows/readme.md).

## Understand

| Document | What it covers |
|---|---|
| [01-how-it-works.md](01-how-it-works.md) | Architecture and internals: packages, startup flow, complete API reference, data layout, build and release pipeline |
| [02-startup-lifecycle.md](02-startup-lifecycle.md) | Dashboard-first boot, service reconciler, lifecycle states and honest status rules |
| [03-architecture-v5.md](03-architecture-v5.md) | Component roles and ownership — Optimizer, Brain, Code Intelligence, Housekeeper, Memory Compiler — and the v5 rules they follow |
| [07-tokens-saved.md](07-tokens-saved.md) | Where every savings number comes from: real metrics vs local estimates, sessions and windows, honesty rules |

## Operate

| Document | What it covers |
|---|---|
| [../readme.md](../readme.md) | Install (Linux/macOS/Windows), usage, dashboard, setup, uninstall |
| [09-release-process.md](09-release-process.md) | Automatic releases, semver conventions (scopes, `!`, `BREAKING CHANGE:`), changelog generation |
| [10-changelog.md](10-changelog.md) | Notable changes per release |

## Agent laws (mandatory workflows)

| Document | What it covers |
|---|---|
| [04-optimizer-law.md](04-optimizer-law.md) | Context budget, Token ROI, reuse, compression and output invariants — the rules the Optimizer enforces |
| [05-codebase-law.md](05-codebase-law.md) | Use the code graph before structural work; keep answers structural |
| [06-obsidian-law.md](06-obsidian-law.md) | Consult, update and close out the project vault for every task |

## Integrations

| Document | What it covers |
|---|---|
| [08-kiro-power.md](08-kiro-power.md) | Kiro Power paths, frontmatter, MCP behavior |

## Platforms

| Document | What it covers |
|---|---|
| [linux/readme.md](linux/readme.md) | Linux install, startup behavior, healthcheck budgets, process handling |
| [macos/readme.md](macos/readme.md) | macOS install (Intel/Apple Silicon), Gatekeeper notes, startup behavior |
| [windows/readme.md](windows/readme.md) | Windows hub: installation, update, troubleshooting, PowerShell and Terminal notes |

## Contribute

| Document | What it covers |
|---|---|
| [../core/testing.md](../core/testing.md) | How to run the Go test suite and validate changes |
| [rules/rules.md](rules/rules.md) | Repo conventions for agents working on DWYT itself |
| [03-architecture-v5.md](03-architecture-v5.md) §70–§73 | Trade-offs, acceptance criteria and the v5 definition of done |
