# DWYT — Commit & Release Guide

Every push to `main` that touches `core/**`, `install.sh`, or `.github/workflows/release.yml`
triggers an **automatic release**. The commit message controls the version bump and the changelog.

---

## Commit Format

```
<type>: <short description>

[optional body]

[optional footer]
```

## Types & Version Bumps

| Type | Bump | When to use |
|------|------|-------------|
| `breaking:` / `BREAKING CHANGE:` | Major (x.0.0) | 🚨 Incompatible API change |
| `feat:` / `feature:` | Minor (0.x.0) | ✨ New feature |
| `fix:` / `bugfix:` | Patch (0.0.x) | 🐛 Bug fix |
| `docs:` / `doc:` | Patch | 📚 Documentation only |
| `chore:` / `build:` / `ci:` | Patch | 🔧 Maintenance |
| `refactor:` | Patch | ♻️ Code restructure, no behavior change |
| `test:` | Patch | 🧪 Tests |
| `perf:` | Patch | ⚡ Performance |
| `style:` | Patch | 💄 Formatting |

## Examples

```bash
# Patch — bug fix
git commit -m "fix: prevent zombie processes in ProcessManager"

# Minor — new feature
git commit -m "feat: add Obsidian search API"

# Major — breaking change
git commit -m "breaking: change /api/status response format"

# With body
git commit -m "feat: add project switching

- POST /api/project/switch endpoint
- Obsidian vault isolation per project
- SSE event broadcast on switch"
```

## Bad Examples

```bash
# ❌ Too vague
git commit -m "fixes"
git commit -m "update code"

# ❌ No type prefix
git commit -m "added new feature"
git commit -m "bug fix"

# ❌ Multiple unrelated changes
git commit -m "fix bugs and add features and update docs"
```

---

## Release Pipeline

On every qualifying push, GitHub Actions:

1. Builds the React frontend (`npm ci && npm run build`)
2. Commits the frontend build artifacts
3. Calculates the new version from commit messages (SemVer)
4. Generates a categorized changelog
5. Creates and pushes the version tag
6. Runs GoReleaser — builds for 5 platforms:
   - Linux amd64 / arm64
   - macOS amd64 / arm64
   - Windows amd64
7. Publishes GitHub Release with binaries, changelog, and `checksums.txt`

**Workflow file:** `.github/workflows/release.yml`  
**GoReleaser config:** `core/.goreleaser.yaml`  
**Permissions needed:** `contents: write` (provided by default `GITHUB_TOKEN`)

---

## Troubleshooting

**Release failed?**
```bash
# Check Actions logs
open https://github.com/fvmoraes/dwyt/actions

# Check current tags
git tag -l

# Check commits since last tag
git log $(git describe --tags --abbrev=0)..HEAD --oneline

# Create tag manually if needed
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3
```

**Test workflow locally:**
```bash
# Install act (https://github.com/nektos/act)
brew install act   # macOS
act push -W .github/workflows/release.yml
```

---

## Setup Git Commit Template

```bash
git config commit.template .github/COMMIT_TEMPLATE.md
```

---

**Full release docs:** `docs/RELEASE-PROCESS.md`
