# DWYT Plan Execution - 2026-05-06

## Scope

Executed the adjustment plan in `docs/plan` across backend, frontend, generated AI-client configs, Kiro Power, tests, and documentation.

## Delivered

- Canonical MCP names are `codebase` and `obsidian`; legacy registry keys are migrated automatically.
- Obsidian vaults now work for normal projects outside `~/.dwyt` and remain under `~/.dwyt/projects/<sha12>/obsidian/`.
- Status APIs now expose canonical `status` values while keeping legacy fields for compatibility.
- Codebase and Headroom status checks use health probes so externally running services are not shown as false offline.
- Project MCP config generation now merges existing JSON instead of leaving stale missing servers.
- RTK remains a CLI-only tool in the Dashboard.
- Kiro Power is generated at `~/.dwyt/powers/dwyt-power` and registered through `~/.kiro/powers/dwyt-power`.
- Dashboard includes Kiro Power status and refresh.
- Obsidian Linux installer discovers the latest AppImage from GitHub releases.
- Shared AI instruction files are no longer ignored by default; local configs with absolute paths remain ignored.

## Validation

- `go test ./...` - 33 tests passing.
- `go vet ./...` - no issues.
- `go build ./...` - success.
- `npm run lint` - success.
- `npm run build` - success.
- `core/test-e2e.sh` - success.
- Temporary-home API smoke test - success for `/api/health`, `/api/status`, `/api/obsidian/status`, `/api/mcp/registry`, `/api/kiro/power/status`, and `/api/kiro/power/refresh`.

## Notes

Commits and push were intentionally left for the user.
