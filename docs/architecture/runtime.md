# Runtime, lifecycle, and diagnostics

This document describes the runtime behavior implemented by DWYT. The Core makes the
dashboard available first; optional services, migrations, and integrations continue
in the background without deciding whether the daemon can bind its HTTP port.

## Dashboard-first startup

> The DWYT Core must not depend on readiness of optional services.

`server.New` performs only the work required to bind the dashboard: opening SQLite
state, resolving a registered project, attaching its vault, and registering managed
services. After the dashboard is serving, one serial background sequence performs:

```text
vault_reconciliation → mcp_config_sync → obsidian_mcp_validation →
housekeeper_start
```

Headroom lifecycle is deliberately outside that sequence. The ServiceReconciler
owns its start, health, adoption, and recovery decisions independently after the
dashboard binds, so no parallel probe bypasses that ownership.

A background failure is logged and isolated. It does not take the daemon down.
Housekeeper startup remains last so a deep pass cannot race a vault migration.

## Managed service lifecycle

The ProcessManager executes processes; the ServiceReconciler owns the decision to
observe, adopt, recover, and publish lifecycle state:

```text
observe → adopt-or-recover → publish state
```

- A healthy process already listening on its registered port is adopted rather than
  duplicated.
- A running process is observed and published, not restarted.
- A dead auto-start service recovers with bounded `1s → 3s → 10s` backoff and a
  per-service singleflight slot.
- The first reconcile pass waits for Codebase warmup, giving startup one owner.

States are `unknown`, `stopped`, `starting`, `healthy`, `degraded`, and `failed`.
`starting` is a grace period, while `degraded` requires two failed probes. Unknown
is never presented as offline.

## Status dimensions

A single health Boolean cannot accurately describe both a managed HTTP process and a
client-owned stdio MCP. The status payload keeps these facts separate:

| Field | Meaning |
|---|---|
| `status` | Whether the configured health endpoint answers now |
| `runtime_state` | Managed-service lifecycle phase |
| `mcp_activity` | Client traffic DWYT actually observed |

For example, an IDE stdio MCP may be active while a separately managed Codebase UI
is degraded. The dashboard reports both facts instead of forcing one color.

## Effective ports and platforms

| Component | Requested port | Fallback / publication |
|---|---:|---|
| Dashboard | 2737 | The Core binds this before optional work begins. |
| Codebase | 9749 | Tries 9750–9753 when occupied; the effective port is stored in runtime state. |
| Headroom | 8787 | Tries 8788–8791 when occupied; the effective port is written to runtime state, dashboard, and managed `env.sh` or `env.ps1`. |

The same lifecycle contract applies on Linux, macOS, and Windows. Platform-specific
installation and troubleshooting are in the [Linux](../linux/readme.md),
[macOS](../macos/readme.md), and [Windows](../windows/readme.md) guides.

## Diagnostics

- `GET /api/diagnostics/startup-tax` reports the measured `tools/list` payload and
  managed instruction-block overhead. Token counts are estimated from bytes and are
  labelled accordingly.
- `GET /api/diagnostics/net-savings?window=` derives gross avoided tokens minus
  measurable startup and instruction costs. No telemetry for a window is unsupported,
  not zero.
- `GET /api/telemetry/summary?window=` and `GET /api/telemetry/requests` expose
  aggregate and request-level provenance.

See [context optimization](context-optimization.md) for budgets, provenance, and
measurement policy; see [benchmarks](../metrics/benchmarks.md) before interpreting
any benchmark result as a product claim.
