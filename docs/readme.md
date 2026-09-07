# DWYT — Documentation index

Everything there is to know about DWYT, organized by what you are trying to do.

## Understand

| Document | What it covers |
|---|---|
| [how-it-works.md](how-it-works.md) | Architecture and internals: packages, startup flow, complete API reference, data layout, build and release pipeline |
| [architecture-v5.md](architecture-v5.md) | Component roles and ownership — Optimizer, Brain, Code Intelligence, Housekeeper, Memory Compiler — and the v5 rules they follow |
| [tokens-saved.md](tokens-saved.md) | Where every savings number comes from: real metrics vs local estimates, sessions and windows, honesty rules |

## Operate

| Document | What it covers |
|---|---|
| [../README.md](../README.md) | Install (Linux/macOS/Windows), usage, dashboard, setup, uninstall |
| [windows/readme.md](windows/readme.md) | Windows hub: installation, update, troubleshooting, PowerShell and Terminal notes |
| [release-process.md](release-process.md) | Automatic releases, semver conventions (scopes, `!`, `BREAKING CHANGE:`), changelog generation |
| [../docs/CHANGELOG.md](CHANGELOG.md) | Notable changes per release |

## Agent laws (mandatory workflows)

| Document | What it covers |
|---|---|
| [codebase-law.md](codebase-law.md) | Use the code graph before structural work; keep answers structural |
| [obsidian-law.md](obsidian-law.md) | Consult, update and close out the project vault for every task |

## Contribute

| Document | What it covers |
|---|---|
| [../core/TESTING.md](../core/TESTING.md) | How to run the Go test suite and validate changes |
| [rules/rules.md](rules/rules.md) | Repo conventions for agents working on DWYT itself |
| [architecture-v5.md](architecture-v5.md) §70–§73 | Trade-offs, acceptance criteria and the v5 definition of done |
