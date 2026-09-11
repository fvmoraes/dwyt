# DWYT on macOS

DWYT supports both Intel (amd64) and Apple Silicon (arm64) Macs.

## Installation

One command (installs to `~/.local/bin`, configures PATH):

```bash
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
```

The script detects your platform and architecture, downloads the matching
binary from GitHub Releases, and guides you through the next steps.

## Usage

```bash
cd ~/my-project
dwyt .
```

The UI opens at `http://localhost:2737` with your project pre-loaded.

## Startup behavior

Same as Linux: the dashboard binds first and services warm up in the
background. A slow cold start shows as **Starting**, never offline. See
[Startup Lifecycle & Service Status](../02-startup-lifecycle.md).

- `DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS` — CLI wait budget on the
  dashboard (default 60 s)
- `DWYT_SERVICE_HEALTHCHECK_TIMEOUT_SECONDS` — daemon budget per managed
  service (default 60 s)

## Platform notes

- Release artifacts are built separately for `darwin-amd64` and
  `darwin-arm64`; the installer picks the matching one.
- After installation, macOS Gatekeeper may quarantine downloaded binaries;
  if `dwyt` fails to launch, clear the quarantine attribute:
  `xattr -d com.apple.quarantine ~/.local/bin/dwyt`.
- On daemon startup timeout, DWYT terminates the daemon's dedicated process
  group and reaps managed descendants.
- RTK terminal compression ships native macOS binaries.

## Requirements

| Tool | Required for |
|------|-------------|
| curl | Installer download |
| Git | Dependency installation |
| Python 3 | Headroom installation (auto-installed when missing) |

The `dwyt` binary itself has no dependencies — it is a static Go
executable with the React UI embedded.
