# Commit Message Template

Use this template for commit messages to ensure proper versioning and changelog generation.

---

## Format

```
<type>: <short description>

[optional body]

[optional footer]
```

---

## Types

### Breaking Changes (Major Version Bump: x.0.0)
```
breaking: remove deprecated API endpoints
BREAKING CHANGE: change config file format
```

### Features (Minor Version Bump: 0.x.0)
```
feat: add support for multiple projects
feature: implement brain search API
```

### Bug Fixes (Patch Version Bump: 0.0.x)
```
fix: resolve race condition in ProcessManager
bugfix: prevent zombie processes
```

### Documentation (Patch Version Bump: 0.0.x)
```
docs: update installation guide
doc: add API reference
```

### Chores (Patch Version Bump: 0.0.x)
```
chore: update dependencies
build: optimize frontend bundle
ci: add automated tests
```

### Other (Patch Version Bump: 0.0.x)
```
refactor: simplify brain save logic
style: format code with gofmt
test: add unit tests for state package
perf: optimize database queries
```

---

## Examples

### Good Examples

**Simple bug fix:**
```
fix: resolve state corruption on save failure
```

**Feature with description:**
```
feat: add RTK metrics per project

- Check if .rtk/ directory exists
- Return nil if RTK not initialized
- Add validation in status package
```

**Breaking change:**
```
breaking: change API response format for /api/status

The status endpoint now returns a structured object instead of a flat array.

Migration guide:
- Old: GET /api/status returns [{name, status}]
- New: GET /api/status returns {services: {name: {status, pid, port}}}
```

**Multiple related changes:**
```
feat: implement project switching

- Add /api/project/switch endpoint
- Implement brain isolation per project
- Update frontend to handle project changes
- Add SSE event for project_switch
```

---

## Bad Examples

❌ Too vague:
```
update code
fixes
changes
```

❌ No type prefix:
```
added new feature
bug fix
updated documentation
```

❌ Multiple unrelated changes:
```
fix bugs and add features and update docs
```

---

## Setup Git Commit Template (Optional)

To use this template automatically:

```bash
# Set commit template
git config commit.template .github/COMMIT_TEMPLATE.md

# Now when you run 'git commit', this template will appear
git commit
```

---

## Quick Reference

| Type | Version Bump | Emoji | Use When |
|------|--------------|-------|----------|
| `breaking:` | Major (x.0.0) | 🚨 | Breaking changes |
| `feat:` | Minor (0.x.0) | ✨ | New features |
| `fix:` | Patch (0.0.x) | 🐛 | Bug fixes |
| `docs:` | Patch (0.0.x) | 📚 | Documentation |
| `chore:` | Patch (0.0.x) | 🔧 | Maintenance |
| `refactor:` | Patch (0.0.x) | ♻️ | Code refactoring |
| `test:` | Patch (0.0.x) | 🧪 | Tests |
| `perf:` | Patch (0.0.x) | ⚡ | Performance |
| `style:` | Patch (0.0.x) | 💄 | Code style |
| `build:` | Patch (0.0.x) | 🏗️ | Build system |
| `ci:` | Patch (0.0.x) | 👷 | CI/CD |

---

**See also:** [RELEASE-PROCESS.md](../docs/RELEASE-PROCESS.md)
