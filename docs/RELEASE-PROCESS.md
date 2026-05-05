# DWYT Release Process

This document describes the automated release process for DWYT.

---

## Overview

DWYT uses **automatic releases on every commit** to the `main` branch. The release process is fully automated via GitHub Actions and follows semantic versioning.

---

## How It Works

### 1. Commit to Main Branch

Every push to `main` that modifies:
- `core/**` (any code changes)
- `install.sh` (installation script)
- `.github/workflows/release.yml` (CI/CD changes)

Will trigger an automatic release.

### 2. Automatic Versioning

Version numbers are automatically calculated based on commit messages using **Semantic Versioning** (SemVer):

```
MAJOR.MINOR.PATCH
```

**Version Bump Rules:**

| Commit Prefix | Version Bump | Example |
|---------------|--------------|---------|
| `BREAKING CHANGE:` or `breaking:` | Major (x.0.0) | v1.0.0 → v2.0.0 |
| `feat:` or `feature:` | Minor (0.x.0) | v1.0.0 → v1.1.0 |
| `fix:` or `bugfix:` | Patch (0.0.x) | v1.0.0 → v1.0.1 |
| Any other | Patch (0.0.x) | v1.0.0 → v1.0.1 |

### 3. Changelog Generation

The changelog is automatically generated from commit messages and categorized:

- 🚨 **Breaking Changes** - `BREAKING CHANGE:` or `breaking:`
- ✨ **Features** - `feat:` or `feature:`
- 🐛 **Bug Fixes** - `fix:` or `bugfix:`
- 📚 **Documentation** - `docs:` or `doc:`
- 🔧 **Chores** - `chore:`, `build:`, `ci:`
- 📝 **Other Changes** - Everything else

### 4. Build and Release

The workflow:
1. Builds the React frontend
2. Embeds frontend into Go binary
3. Builds binaries for 5 platforms:
   - Linux (amd64, arm64)
   - macOS (amd64, arm64)
   - Windows (amd64)
4. Generates SHA256 checksums
5. Creates GitHub Release with:
   - Version tag
   - Categorized changelog
   - Binary archives
   - Checksums file
   - Installation instructions

---

## Commit Message Convention

To ensure proper versioning and changelog generation, follow these conventions:

### Format

```
<type>: <description>

[optional body]

[optional footer]
```

### Types

**Breaking Changes:**
```bash
git commit -m "breaking: remove deprecated API endpoints"
git commit -m "BREAKING CHANGE: change config file format"
```

**Features:**
```bash
git commit -m "feat: add support for multiple projects"
git commit -m "feature: implement Obsidian search API"
```

**Bug Fixes:**
```bash
git commit -m "fix: resolve race condition in ProcessManager"
git commit -m "bugfix: prevent zombie processes"
```

**Documentation:**
```bash
git commit -m "docs: update installation guide"
git commit -m "doc: add API reference"
```

**Chores:**
```bash
git commit -m "chore: update dependencies"
git commit -m "build: optimize frontend bundle"
git commit -m "ci: add automated tests"
```

**Other:**
```bash
git commit -m "refactor: simplify Obsidian save logic"
git commit -m "style: format code with gofmt"
git commit -m "test: add unit tests for state package"
```

### Examples

**Good commit messages:**
```bash
# Feature (minor bump)
git commit -m "feat: add RTK metrics per project"

# Bug fix (patch bump)
git commit -m "fix: resolve state corruption on save failure"

# Breaking change (major bump)
git commit -m "breaking: change API response format for /api/status"

# Multiple changes
git commit -m "feat: add project switching API

- Implement /api/project/switch endpoint
- Add Obsidian isolation per project
- Update frontend to handle project changes"
```

**Bad commit messages:**
```bash
# Too vague
git commit -m "update code"
git commit -m "fixes"

# No type prefix
git commit -m "added new feature"
git commit -m "bug fix"
```

---

## Release Workflow

### Automatic Release (Recommended)

1. Make your changes
2. Commit with proper message format
3. Push to `main` branch
4. GitHub Actions automatically:
   - Determines version bump
   - Generates changelog
   - Builds binaries
   - Creates release

```bash
# Example workflow
git checkout main
git pull origin main

# Make changes
vim core/internal/brain/brain.go  # Obsidian vault package

# Commit with proper format
git add .
git commit -m "fix: resolve lock released before write"

# Push to trigger release
git push origin main

# Wait ~5 minutes for release to be created
```

### Manual Release (Not Recommended)

If you need to create a manual release:

```bash
# Create and push tag
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3

# GitHub Actions will build and release
```

---

## Release Assets

Each release includes:

### Binaries

- `dwyt_linux_amd64.tar.gz` - Linux 64-bit
- `dwyt_linux_arm64.tar.gz` - Linux ARM64
- `dwyt_darwin_amd64.tar.gz` - macOS Intel
- `dwyt_darwin_arm64.tar.gz` - macOS Apple Silicon
- `dwyt_windows_amd64.zip` - Windows 64-bit

### Checksums

- `checksums.txt` - SHA256 checksums for all binaries

### Installation

Users can install via:

```bash
# Automatic installation (recommended)
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash

# Manual installation
# 1. Download binary for your platform
# 2. Verify checksum
# 3. Extract and move to PATH
```

---

## Version History

All releases are tracked in:
- **GitHub Releases** - https://github.com/fvmoraes/dwyt/releases
- **Git Tags** - `git tag -l`
- **CHANGELOG.md** - `docs/CHANGELOG.md`

---

## Troubleshooting

### Release Failed

Check GitHub Actions logs:
1. Go to https://github.com/fvmoraes/dwyt/actions
2. Click on the failed workflow
3. Review logs for errors

Common issues:
- **Frontend build failed** - Check `npm ci` or `npm run build` errors
- **GoReleaser failed** - Check Go build errors or .goreleaser.yaml syntax
- **Permission denied** - Ensure `GITHUB_TOKEN` has `contents: write` permission

### Wrong Version Generated

The version is calculated from commit messages since the last tag. To fix:

```bash
# Check current tags
git tag -l

# Check commits since last tag
git log v1.0.0..HEAD --oneline

# If needed, create manual tag
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3
```

### Changelog Missing Commits

Ensure commits follow the convention:
- Use proper prefixes (`feat:`, `fix:`, etc.)
- Commits must be on `main` branch
- Commits must be after the previous tag

---

## Best Practices

### 1. Atomic Commits

Make small, focused commits:
```bash
# Good
git commit -m "fix: resolve race condition in ProcessManager"
git commit -m "feat: add Obsidian search API"

# Bad
git commit -m "fix: multiple bugs and add features"
```

### 2. Descriptive Messages

Be specific about what changed:
```bash
# Good
git commit -m "fix: prevent zombie processes by checking /proc/<pid>/stat"

# Bad
git commit -m "fix: bug"
```

### 3. Test Before Pushing

Always test locally before pushing to `main`:
```bash
# Run tests
cd core
go test ./... -v

# Build locally
go build -o dwyt .

# Test binary
./dwyt version
```

### 4. Review Releases

After pushing, verify the release:
1. Check GitHub Actions completed successfully
2. Review generated changelog
3. Download and test a binary
4. Verify checksums

---

## CI/CD Configuration

### GitHub Actions Workflow

Location: `.github/workflows/release.yml`

Triggers on:
- Push to `main` branch
- Changes to `core/**`, `install.sh`, or workflow file

Permissions required:
- `contents: write` - Create releases and tags

### GoReleaser Configuration

Location: `core/.goreleaser.yaml`

Key settings:
- Builds for 5 platforms
- Generates tar.gz (Linux/macOS) and zip (Windows)
- Creates checksums.txt
- Publishes to GitHub Releases

---

## Future Improvements

Potential enhancements:
- [ ] Add pre-release support (alpha, beta, rc)
- [ ] Implement changelog validation
- [ ] Add release notes templates
- [ ] Integrate with package managers (Homebrew, apt, etc.)
- [ ] Add automated testing before release
- [ ] Implement rollback mechanism

---

## References

- **Semantic Versioning** - https://semver.org/
- **Conventional Commits** - https://www.conventionalcommits.org/
- **GoReleaser** - https://goreleaser.com/
- **GitHub Actions** - https://docs.github.com/en/actions

---

**Last Updated:** 2026-05-05 — v4.0.0  
**Maintained By:** DWYT Team
