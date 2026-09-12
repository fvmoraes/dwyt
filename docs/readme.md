# DWYT documentation

DWYT is a context optimizer with three first-party MCPs, durable project memory,
and deterministic policies for retrieval, token budgets, lifecycle, and telemetry.
This index is the canonical starting point for current behavior.

## Start here

- [Project README](../readme.md) — installation, usage, dashboard, and supported platforms.
- [Architecture overview](architecture/overview.md) — ownership and component boundaries.
- [Runtime and lifecycle](architecture/runtime.md) — dashboard-first startup, reconciler, effective ports, and diagnostics.
- [Context optimization](architecture/context-optimization.md) — budgets, ROI, reuse, compression, cache, and provenance.

## Laws

The laws describe durable contracts; generated client instructions link to them rather
than copying them.

- [Optimizer Law](laws/optimizer-law.md)
- [Codebase Law](laws/codebase-law.md)
- [Obsidian Law](laws/obsidian-law.md)

## Metrics and evidence

- [Tokens saved and provenance](metrics/tokens-saved.md)
- [Benchmarks and measurement policy](metrics/benchmarks.md)

## Operations and integrations

- [Release process](operations/release-process.md)
- [Changelog](CHANGELOG.md)
- [Kiro Power](integrations/kiro-power.md)

## Platforms

- [Linux](linux/readme.md)
- [macOS](macos/readme.md)
- [Windows](windows/readme.md)

## Compatibility paths

The former numbered pages and `rules/rules.md` remain as short compatibility or
archive pages for older links. They are not independent sources of architecture or
agent policy; use the documents above for current guidance.
