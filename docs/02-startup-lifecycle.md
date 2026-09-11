# Startup Lifecycle & Service Status

How DWYT boots, who owns each managed service, and how the dashboard
derives the state it shows. This documents behavior as implemented —
`core/internal/server/startup.go`, `core/internal/server/svcctl.go`,
`core/internal/status/status.go`.

## Dashboard-first startup (CORE AVAILABILITY LAW)

> The DWYT Core must not depend on the readiness of optional services.

`New()` performs only the work the Core needs to bind :2737:

- open SQLite state;
- load the v5 configuration;
- resolve the project (never adopting a ghost directory);
- attach the Obsidian vault for a registered project;
- register the managed services with the ProcessManager;
- start the Codebase warmup in the background.

Everything else runs as **ordered background tasks** after the dashboard is
already serving, preserving the relative order the synchronous version had:

```text
migrate_old_memory_dirs → brain_v5_migration → vault_migration →
vault_stats → obsidian_mcp_validation → mcp_config_sync →
headroom_probe → housekeeper_start (LAST)
```

Rules:

- a failing task is logged and skipped — it never fails the daemon;
- each task keeps the exact log/error semantics it had when synchronous;
- `housekeeper_start` stays last so its deep pass never races the vault
  migrations;
- tasks run sequentially on one goroutine: shared vault writes stay
  serialized by construction, with no new concurrency to audit.

## Service lifecycle: one owner, one watchdog

The ProcessManager (`core/internal/procman`) executes processes. The
ServiceReconciler (`core/internal/server/svcctl.go`) decides:

```text
observe → adopt-or-recover → publish state
```

- **Adopt**: if the registered port already answers health, the external
  instance is adopted and nothing is spawned (no duplicate processes
  competing for the same graph cache).
- **Never restart a running process**: the reconciler observes and
  publishes; it only spawns when the process is gone. This keeps API
  handlers and the watchdog from racing the same lifecycle.
- **Bounded recovery**: a dead auto-start service is restarted with a
  1s → 3s → 10s (capped) backoff and a per-service singleflight slot. No
  restart storms.
- **Warmup gate**: the first reconcile pass waits for the startup
  Codebase warmup attempt, so there is exactly one startup owner.

### Lifecycle states

```text
unknown → stopped → starting → healthy | degraded | failed
```

- `starting`: process spawned, health not answering yet — the grace
  period. Connection refused right after spawn is expected, not offline.
- `healthy` → `degraded`: only after two consecutive failed probes
  (hysteresis; one transient failure keeps the card green).
- `failed`: the process exited and recovery attempts are exhausted or
  gated.

States are published to runtime state (`state.json` `processes[].state`,
an additive field) and surfaced through `/api/status` as
`runtime_state` on each tool.

## Status model: more than online/offline

A single health boolean cannot represent a managed HTTP service, a
client-owned stdio MCP and an installed binary at once. The status
payload therefore carries independent dimensions:

| Field | Answers |
|---|---|
| `status` (probe) | Is the health endpoint answering right now? |
| `runtime_state` | What lifecycle phase is the DWYT-managed service in? |
| `mcp_activity` | Has DWYT observed client MCP traffic? |

Hard rules:

- **Unknown is never rendered as offline.** A card with no evidence shows
  Unknown.
- A Codebase UI process failing while an IDE stdio session works is
  `Service degraded` + `MCP active` — two facts, not one color.
- `mcp_activity` is filled only where DWYT actually observes traffic;
  fabricated activity is a telemetry lie, so it stays unknown.

## Diagnostics

- `GET /api/diagnostics/startup-tax` — measured tool-schema overhead per
  first-party MCP plus the managed instruction block. Provenance is always
  `estimated`; a test gate fails CI if the schemas grow without a
  documented before/after.
- `GET /api/diagnostics/net-savings?window=` — gross avoided tokens minus
  the measurable startup/instruction taxes. An empty window reports
  unknown, never zero.

## Anti-patterns this design rejects

- `time.Sleep` as a synchronization mechanism (startup or tests).
- Synchronous, optional work before the dashboard bind.
- Two code paths starting the same managed service.
- Unbounded retry loops against a dead binary.
- Collapsing unknown into offline, or estimated into observed.
