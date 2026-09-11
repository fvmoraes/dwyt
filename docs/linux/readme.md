# DWYT on Linux

Everything below assumes the standard Linux installation — Ubuntu is the
primary validation target, but any glibc distribution works.

## Installation

One command (installs to `~/.local/bin`, configures PATH):

```bash
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
```

The script detects your platform, downloads the latest binary from GitHub
Releases, overwrites any previous `dwyt` binary, and guides you through the
next steps.

## Usage

```bash
cd ~/my-project
dwyt .
```

The UI opens at `http://localhost:2737` with your project pre-loaded.

## Startup behavior

The dashboard binds first; heavy work runs in the background. A slow
Codebase cold start shows as **Starting** on its card — never as offline —
and the service can recover to healthy without restarting DWYT. See
[Startup Lifecycle & Service Status](../02-startup-lifecycle.md).

- `DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS` — the CLI's wait budget on the
  dashboard itself (default 60 s)
- `DWYT_SERVICE_HEALTHCHECK_TIMEOUT_SECONDS` — the daemon's budget per
  managed service, Codebase and Headroom (default 60 s)

Both knobs are independent; raise one for a slow machine without widening
the other.

## Platform notes

- The daemon, API, SQLite, MCP servers, Headroom proxy and the
  cross-platform process manager run natively.
- On daemon startup timeout, DWYT terminates the daemon's dedicated process
  group, which also reaps managed descendants — no orphans, no zombies.
- RTK terminal compression ships native Linux binaries.

## Requirements

| Tool | Required for |
|------|-------------|
| curl or wget | Installer download |
| Git | Dependency installation |
| Python 3 | Headroom installation (auto-installed when missing) |

The `dwyt` binary itself has no dependencies — it is a static Go
executable with the React UI embedded.
