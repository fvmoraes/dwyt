# GitHub Actions Workflows

This directory contains CI/CD workflows for DWYT.

---

## Workflows

### `release.yml` - Automated Release

**Trigger:** Push to `main` branch (when `core/**`, `install.sh`, or workflow file changes)

**What it does:**
1. Builds React frontend
2. Calculates version from commit messages (SemVer)
3. Generates categorized changelog
4. Creates and pushes version tag
5. Builds binaries for 5 platforms
6. Creates GitHub Release with binaries and changelog

**Version Calculation:**
- `breaking:` or `BREAKING CHANGE:` → Major bump (x.0.0)
- `feat:` or `feature:` → Minor bump (0.x.0)
- `fix:`, `bugfix:`, or any other → Patch bump (0.0.x)

**Changelog Categories:**
- 🚨 Breaking Changes
- ✨ Features
- 🐛 Bug Fixes
- 📚 Documentation
- 🔧 Chores
- 📝 Other Changes

**Platforms:**
- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64)

**Permissions Required:**
- `contents: write` - Create releases and tags

---

## Commit Message Convention

To ensure proper versioning and changelog generation:

```bash
# Features (minor bump)
git commit -m "feat: add new feature"

# Bug fixes (patch bump)
git commit -m "fix: resolve bug"

# Breaking changes (major bump)
git commit -m "breaking: remove deprecated API"

# Documentation (patch bump)
git commit -m "docs: update guide"

# Chores (patch bump)
git commit -m "chore: update dependencies"
```

See [../docs/RELEASE-PROCESS.md](../docs/RELEASE-PROCESS.md) for complete documentation.

---

## Secrets

No secrets are required. The workflow uses the default `GITHUB_TOKEN` which is automatically provided by GitHub Actions.

---

## Troubleshooting

### Release Failed

1. Check workflow logs: https://github.com/fvmoraes/dwyt/actions
2. Common issues:
   - Frontend build failed: Check npm errors
   - GoReleaser failed: Check Go build errors
   - Permission denied: Verify `contents: write` permission

### Wrong Version

Version is calculated from commits since last tag. To check:

```bash
# View current tags
git tag -l

# View commits since last tag
git log v1.0.0..HEAD --oneline

# Manually create tag if needed
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3
```

---

## Local Testing

Test the workflow locally using [act](https://github.com/nektos/act):

```bash
# Install act
brew install act  # macOS
# or
curl https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash

# Run workflow
act push -W .github/workflows/release.yml
```

---

## Future Improvements

- [ ] Add automated testing before release
- [ ] Implement pre-release support (alpha, beta, rc)
- [ ] Add release notes validation
- [ ] Integrate with package managers
- [ ] Add rollback mechanism

---

**Last Updated:** 2026-05-04
