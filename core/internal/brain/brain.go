package brain

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/platform"
)

type BrainEntry struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	Title     string    `json:"title,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	FilePath  string    `json:"file_path,omitempty"`
}

type ContextSnapshot struct {
	Client         string            `json:"client,omitempty"`
	ConversationID string            `json:"conversation_id,omitempty"`
	UserRequest    string            `json:"user_request,omitempty"`
	Summary        string            `json:"summary,omitempty"`
	Context        string            `json:"context,omitempty"`
	Outcome        string            `json:"outcome,omitempty"`
	Files          []string          `json:"files,omitempty"`
	Decisions      []string          `json:"decisions,omitempty"`
	Actions        []string          `json:"actions,omitempty"`
	Commands       []string          `json:"commands,omitempty"`
	Errors         []string          `json:"errors,omitempty"`
	NextSteps      []string          `json:"next_steps,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type ProjectObsidian struct {
	ProjectID    string       `json:"project_id"`
	ProjectName  string       `json:"project_name"`
	ProjectPath  string       `json:"project_path"`
	Summary      string       `json:"summary"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	AIEnabled    []string     `json:"ai_enabled"`
	ToolsEnabled []string     `json:"tools_enabled"`
	mu           sync.RWMutex `json:"-"`
	baseDir      string       `json:"-"`
	brainDir     string       `json:"-"`
	statsMu      sync.Mutex   `json:"-"`
	statsCache   map[string]interface{}
	statsAt      time.Time `json:"-"`
}

type BrainManager struct {
	Current *ProjectObsidian
}

// safePath ensures the resolved path stays within dwytHome boundary.
// Returns error if path escapes the allowed scope.
func safePath(dwytHome, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("obsidian: unsafe path resolution: %w", err)
	}
	dwytAbs, err := filepath.Abs(dwytHome)
	if err != nil {
		return fmt.Errorf("obsidian: unsafe home resolution: %w", err)
	}
	if !strings.HasPrefix(abs+string(os.PathSeparator), dwytAbs+string(os.PathSeparator)) && abs != dwytAbs {
		return fmt.Errorf("obsidian: path escapes dwyt home boundary: %s", abs)
	}
	return nil
}

// HasVaultDir reports whether a vault directory already exists for the
// project, in either the canonical "<hash>_<name>" or the legacy "<hash>"
// layout. It is read-only: read-side helpers (status, diagnostics) must never
// create a vault as a side effect of being called.
func HasVaultDir(dwytHome, projectPath string) bool {
	if strings.TrimSpace(projectPath) == "" {
		return false
	}
	id := db.HashPath(projectPath)
	projectsDir := filepath.Join(dwytHome, "projects")
	candidates := []string{id}
	if named := VaultDirectoryName(id, filepath.Base(projectPath)); named != id {
		candidates = append([]string{named}, candidates...)
	}
	for _, name := range candidates {
		if info, err := os.Stat(filepath.Join(projectsDir, name)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func NewProjectObsidian(dwytHome, projectPath string) (*ProjectObsidian, error) {
	// Note: the project path itself is NOT required to exist here — vault
	// naming only hashes the path string, and internal flows rely on that.
	// Callers that take user input (project switch, daemon startup) are
	// responsible for validating that the directory is real, so a stale path
	// never phantom-projects a vault into existence.
	id := db.HashPath(projectPath)
	projectName := filepath.Base(projectPath)

	// Resolve to the canonical "hash_name" layout. Older installs may still
	// have a "<hash>"-only directory — adoptLegacyVaultLayout migrates it in
	// place (or coexists with it when migration is unsafe).
	baseDir, err := adoptLegacyVaultLayout(dwytHome, id, projectName)
	if err != nil {
		return nil, err
	}
	if err := safePath(dwytHome, baseDir); err != nil {
		return nil, err
	}

	// Flatten any "brain"/"obsidian" subdirectory from even older layouts so
	// the vault root itself is the Obsidian vault.
	if err := migrateLegacyVaultLayout(baseDir); err != nil {
		return nil, err
	}

	brainDir := baseDir
	if err := os.MkdirAll(brainDir, 0755); err != nil {
		return nil, fmt.Errorf("obsidian: create vault directory: %w", err)
	}

	dirs := []string{
		"knowledge",
		"logs",
		filepath.Join("logs", "sessions"),
		filepath.Join("logs", "errors"),
		filepath.Join("logs", "commands"),
		"templates",
		"instructions",
		"maps",
		"decisions",
		"tasks",
		"debug",
		"context",
		".obsidian",
		".dwyt",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(brainDir, d), 0755); err != nil {
			return nil, fmt.Errorf("obsidian: create vault subdirectory %q: %w", d, err)
		}
	}

	pb := &ProjectObsidian{
		ProjectID:   id,
		ProjectName: projectName,
		ProjectPath: projectPath,
		UpdatedAt:   time.Now(),
		baseDir:     baseDir,
		brainDir:    brainDir,
	}

	contextFile := filepath.Join(brainDir, "context.md")
	if data, err := os.ReadFile(contextFile); err == nil {
		pb.Summary = string(data)
		pb.CreatedAt = time.Now()
		if info, err := os.Stat(contextFile); err == nil {
			pb.CreatedAt = info.ModTime()
			pb.UpdatedAt = info.ModTime()
		}
	} else {
		pb.CreatedAt = time.Now()
		pb.UpdatedAt = pb.CreatedAt
		pb.RebuildSummary()
	}

	if err := ensureBrainJSON(baseDir, projectPath); err != nil {
		return nil, err
	}
	// vault.json is the durable identity of the vault (project_hash +
	// project_name + directory_name). It is what allows a future migration to
	// recover the project name when the registry or runtime state is gone.
	if err := WriteVaultMeta(brainDir, VaultMeta{
		Version:     VaultMetaVersion,
		ProjectHash: id,
		ProjectName: projectName,
	}); err != nil {
		log.Warn("vault: failed to write metadata", log.Fields{"dir": brainDir, "error": err.Error()})
	}
	if err := ensureSeedFiles(brainDir); err != nil {
		return nil, err
	}
	return pb, nil
}

// adoptLegacyVaultLayout returns the canonical "<hash>_<name>" vault path for
// a project, migrating an existing "<hash>"-only directory in place when
// safe. It is idempotent: calling it repeatedly yields the same answer and
// never deletes content. It is conservative: when the target directory
// already exists with different content (a collision from a previous rename
// attempt or a foreign directory with the same name), the legacy directory
// is preserved untouched so the user can resolve the conflict manually.
func adoptLegacyVaultLayout(dwytHome, projectHash, projectName string) (string, error) {
	if projectHash == "" {
		return "", fmt.Errorf("vault: empty project hash")
	}
	projectsDir := filepath.Join(dwytHome, "projects")
	legacyDir := filepath.Join(projectsDir, projectHash)
	canonicalDir := filepath.Join(projectsDir, ResolveVaultDirName(projectsDir, projectHash, projectName))

	// Nothing to migrate: the canonical directory already exists and the
	// hash-only directory does not. This is the steady state.
	if samePath(legacyDir, canonicalDir) {
		return canonicalDir, nil
	}

	legacyInfo, legacyErr := os.Stat(legacyDir)

	// No legacy directory → just use the canonical path. A different stat
	// failure is not evidence that no legacy data exists, so do not migrate
	// based on an incomplete filesystem view.
	if legacyErr != nil {
		if os.IsNotExist(legacyErr) {
			return canonicalDir, nil
		}
		return "", fmt.Errorf("vault: inspect legacy path %s: %w", legacyDir, legacyErr)
	}
	if !legacyInfo.IsDir() {
		return canonicalDir, nil
	}

	canonicalInfo, canonicalErr := os.Stat(canonicalDir)

	// Canonical path exists but is NOT a directory (a stray file with an
	// unlucky name). Renaming onto it would fail on every platform, and
	// failing the whole vault load over it would be worse than limping on
	// the legacy path — so keep the legacy directory and let the user
	// clean up the stray file.
	if canonicalErr == nil && !canonicalInfo.IsDir() {
		log.Warn("vault: canonical path is a file; keeping legacy layout",
			log.Fields{"canonical": canonicalDir, "legacy": legacyDir})
		return legacyDir, nil
	}

	// Canonical directory already exists. Decide which one wins.
	switch {
	case os.IsNotExist(canonicalErr):
		// Canonical doesn't exist yet → rename legacy into place.
		if err := os.MkdirAll(projectsDir, 0755); err != nil {
			return "", fmt.Errorf("vault: prepare projects dir: %w", err)
		}
		if err := os.Rename(legacyDir, canonicalDir); err != nil {
			return "", fmt.Errorf("vault: rename legacy %s -> %s: %w", legacyDir, canonicalDir, err)
		}
		log.Info("vault: migrated legacy layout", log.Fields{"from": legacyDir, "to": canonicalDir})
		// Keep the Obsidian app pointed at the renamed directory. A failure
		// here is non-fatal: the vault on disk is already correct and the
		// user can re-open it once from Obsidian.
		if err := UpdateObsidianVaultPath(legacyDir, canonicalDir); err != nil {
			log.Warn("vault: obsidian registry update failed", log.Fields{"error": err.Error()})
		}
		return canonicalDir, nil
	case canonicalErr != nil:
		return "", fmt.Errorf("vault: inspect canonical path %s: %w", canonicalDir, canonicalErr)
	default:
		// Both exist. Keep the canonical (newer) directory untouched and
		// log a warning so the user can decide how to merge.
		log.Warn("vault: legacy and canonical directories both exist; keeping canonical",
			log.Fields{"legacy": legacyDir, "canonical": canonicalDir})
		return canonicalDir, nil
	}
}

// samePath returns true when two file paths refer to the same location on
// disk. It uses lexical comparison after filepath.Clean because the inputs
// are always absolute (built from dwytHome) and we don't expect symlinks.
func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func ensureSeedFiles(brainDir string) error {
	seeds := map[string]string{
		"index.md": `---
type: index
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [project, index]
---

# Project Index

Welcome to the Project Brain. This vault contains the knowledge base for your project.

## Structure
- [[context]] — Full project summary
- [[decisions/index|Decisions]] — Architecture and design decisions
- [[tasks/index|Tasks]] — Active tasks and progress
- [[debug/index|Debug]] — Errors, investigations, and root-cause notes
- [[context/index|Context]] — Complete task/session handoffs
- [[instructions/obsidian-law|Obsidian Law]] — Mandatory agent memory rules
- [[instructions/codebase-law|Codebase Law]] — Mandatory code graph rules
- [[maps/project-map|Project Map]] — Navigation hub for future agents
- **knowledge/** — Knowledge base articles
- **logs/commands/** — Command records
- **templates/** — Reusable note templates

## Agent Rule

For shell commands use RTK. For code structure use [[instructions/codebase-law|Codebase Law]]. For memory use [[instructions/obsidian-law|Obsidian Law]]. Before losing context, save a linked handoff in [[context/index]].
`,
		"decisions.md": `---
type: decisions
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [decisions, architecture]
---

# Decisions Log

This legacy root note points to [[decisions/index]].
`,
		"tasks.md": `---
type: tasks
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [tasks, progress]
---

# Tasks

This legacy root note points to [[tasks/index]].
`,
		filepath.Join("instructions", "obsidian-law.md"): `---
type: instruction
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [dwyt, obsidian, agents, memory]
---

# Obsidian Law

The Obsidian vault is the official project memory. It works together with [[instructions/codebase-law|Codebase Law]] and [[maps/project-map|Project Map]].

## Mandatory workflow

1. Before relevant work, search existing notes and rebuild/read the vault summary.
2. During work, save important decisions as ` + "`decision`" + ` entries and task/status updates as ` + "`task`" + ` entries.
3. At the end of every task, save complete context in [[context/index]] with request, summary, files, decisions, actions, commands, errors, outcome, next steps, and future-agent context.

## Vault quality

Keep the vault rich, linked, and organized. Prefer internal links, folders, templates, clear headings, and enough context for a future agent to continue without reconstructing history. Never delete vault files during install, uninstall, reinstall, clean, repair, or reset flows.
`,
		filepath.Join("instructions", "codebase-law.md"): `---
type: instruction
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [dwyt, codebase, agents, graph]
---

# Codebase Law

Use the Codebase MCP whenever you need to understand, validate, diagnose, refactor, or alter the real code structure.

## Mandatory workflow

1. Validate whether the project is indexed.
2. Use search_graph to find symbols, routes, components, modules, handlers, and relationships.
3. Use trace_path for callers, dependencies, data flow, and impact.
4. Use get_code_snippet before proposing or applying code edits.
5. Save important findings and follow-up context to [[context/index]] and [[decisions/index]] through [[instructions/obsidian-law|Obsidian Law]].

The graph is the primary source for files, symbols, dependencies, calls, paths, and impact. Avoid manual grep/glob as the first strategy when Codebase MCP is available.
`,
		filepath.Join("decisions", "index.md"): `---
type: decisions
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [dwyt, decisions, architecture]
---

# Decisions

Links: [[index]] [[maps/project-map]] [[instructions/obsidian-law]] [[instructions/codebase-law]]

## Recent Decisions
`,
		filepath.Join("tasks", "index.md"): `---
type: tasks
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [dwyt, tasks, progress]
---

# Tasks

Links: [[index]] [[maps/project-map]] [[instructions/obsidian-law]] [[instructions/codebase-law]]

## Active
`,
		filepath.Join("debug", "index.md"): `---
type: debug
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [dwyt, debug]
---

# Debug

Links: [[index]] [[maps/project-map]] [[instructions/obsidian-law]] [[instructions/codebase-law]]

## Investigations
`,
		filepath.Join("context", "index.md"): `---
type: context
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [dwyt, context, handoff]
---

# Context

Links: [[index]] [[maps/project-map]] [[instructions/obsidian-law]] [[instructions/codebase-law]]

Complete task/session handoffs are saved in this folder.
`,
		filepath.Join("maps", "project-map.md"): `---
type: map
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [dwyt, map, navigation]
---

# Project Map

- [[index|Project Index]]
- [[context|Current Summary]]
- [[decisions/index|Decision Log]]
- [[tasks/index|Task Log]]
- [[debug/index|Debug Log]]
- [[context/index|Context Handoffs]]
- [[instructions/obsidian-law|Obsidian Law]]
- [[instructions/codebase-law|Codebase Law]]
- [[templates/decision-template|Decision Template]]
- [[templates/task-template|Task Template]]
- [[templates/session-context-template|Session Context Template]]
`,
		filepath.Join("templates", "decision-template.md"): `---
type: template
tags: [template, decision]
---

# Decision - {{title}}

Links: [[decisions/index]] [[maps/project-map]] [[instructions/codebase-law]] [[instructions/obsidian-law]]

## Context

## Decision

## Consequences

## Links
`,
		filepath.Join("templates", "task-template.md"): `---
type: template
tags: [template, task]
---

# Task - {{title}}

Links: [[tasks/index]] [[maps/project-map]] [[instructions/codebase-law]] [[instructions/obsidian-law]]

## Status

## Goal

## Actions

## Blockers

## Next Steps
`,
		filepath.Join("templates", "session-context-template.md"): `---
type: template
tags: [template, session, context]
---

# Session Context - {{date}}

Links: [[context/index]] [[maps/project-map]] [[instructions/codebase-law]] [[instructions/obsidian-law]]

## User Request

## Summary

## Files

## Decisions

## Actions

## Commands

## Errors

## Outcome

## Next Steps

## Context For Future Agents
`,
	}
	for name, content := range seeds {
		path := filepath.Join(brainDir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("obsidian: stat seed file %q: %w", name, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("obsidian: create seed directory for %q: %w", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("obsidian: write seed file %q: %w", name, err)
		}
	}
	return nil
}

func MigrateOldMemoryDirs(dwytHome string) error {
	return MigrateOldMemoryDirsContext(context.Background(), dwytHome)
}

type legacyMemoryPayload struct {
	Entries []legacyMemoryEntry `json:"entries"`
}

type legacyMemoryEntry struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

const (
	legacyMemoryPlanVersion = 1
	legacyMemoryPlanSource  = "memory/memory.json"
)

type legacyMemoryMigrationPlan struct {
	Version    int                           `json:"version"`
	Source     string                        `json:"source"`
	SourceHash string                        `json:"source_hash"`
	Targets    []legacyMemoryMigrationTarget `json:"targets"`
	Completed  bool                          `json:"completed"`
}

type legacyMemoryMigrationTarget struct {
	Path         string `json:"path"`
	BeforeSHA256 string `json:"before_sha256"`
	After        string `json:"after"`
}

func MigrateOldMemoryDirsContext(ctx context.Context, dwytHome string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	projectsDir := filepath.Join(dwytHome, "projects")
	entries, err := os.ReadDir(projectsDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy memory projects directory %s: %w", projectsDir, err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() {
			continue
		}
		baseDir := filepath.Join(projectsDir, entry.Name())
		plan, hasPlan, err := readLegacyMemoryPlan(baseDir)
		if err != nil {
			return err
		}
		memoryDir := filepath.Join(baseDir, "memory")
		memoryInfo, err := os.Stat(memoryDir)
		if os.IsNotExist(err) {
			if hasPlan && !plan.Completed {
				if err := resumeLegacyMemoryPlan(ctx, baseDir, plan); err != nil {
					return err
				}
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("stat legacy memory directory %s: %w", memoryDir, err)
		}
		if !memoryInfo.IsDir() {
			continue
		}

		memoryFile := filepath.Join(memoryDir, "memory.json")
		data, err := os.ReadFile(memoryFile)
		if os.IsNotExist(err) {
			if !hasPlan {
				if err := removeEmptyLegacyMemoryDir(memoryDir); err != nil {
					return err
				}
				continue
			}
			if !plan.Completed {
				if err := resumeLegacyMemoryPlan(ctx, baseDir, plan); err != nil {
					return err
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := removeEmptyLegacyMemoryDir(memoryDir); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("read legacy memory file %s: %w", memoryFile, err)
		}

		var payload legacyMemoryPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			return fmt.Errorf("decode legacy memory file %s: %w", memoryFile, err)
		}
		if err := migrateLegacyVaultLayout(baseDir); err != nil {
			return fmt.Errorf("migrate legacy vault layout %s: %w", baseDir, err)
		}
		if err := os.MkdirAll(filepath.Join(baseDir, "knowledge"), 0755); err != nil {
			return fmt.Errorf("create migrated knowledge directory %s: %w", baseDir, err)
		}
		if err := os.MkdirAll(filepath.Join(baseDir, "logs"), 0755); err != nil {
			return fmt.Errorf("create migrated logs directory %s: %w", baseDir, err)
		}
		if err := ensureSeedFiles(baseDir); err != nil {
			return fmt.Errorf("seed migrated vault %s: %w", baseDir, err)
		}

		sourceHash := legacyMemoryContentHash(data)
		if hasPlan {
			if plan.Source != legacyMemoryPlanSource || plan.SourceHash != sourceHash {
				return fmt.Errorf("legacy memory plan for %s does not match its source", baseDir)
			}
			// Preserve the legacy source as a verified backup after completion.
			// It also prevents a concurrent writer from losing a replacement
			// memory.json during cleanup.
			if plan.Completed {
				continue
			}
		} else {
			partiallyImported, err := legacyMemoryAppearsPartiallyImported(ctx, baseDir, payload)
			if err != nil {
				return err
			}
			if partiallyImported {
				return fmt.Errorf("legacy memory import for %s appears partially applied; preserving source for manual reconciliation", baseDir)
			}
			plan, err = newLegacyMemoryPlan(ctx, baseDir, sourceHash, payload)
			if err != nil {
				return err
			}
			if err := writeLegacyMemoryPlan(baseDir, plan); err != nil {
				return err
			}
		}
		if err := resumeLegacyMemoryPlan(ctx, baseDir, plan); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// The completed plan is the durable import record. Retain memory.json
		// as a source backup rather than racing a legacy writer during cleanup.
	}
	return nil
}

func legacyMemoryPlanPath(baseDir string) string {
	return filepath.Join(baseDir, ".dwyt", "legacy-memory-plan.json")
}

// removeEmptyLegacyMemoryDir only removes a proven-empty residue. It never
// recursively deletes a directory because an interrupted cleanup or a legacy
// writer may have added data after the import plan was recorded.
func removeEmptyLegacyMemoryDir(memoryDir string) error {
	entries, err := os.ReadDir(memoryDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read residual legacy memory directory %s: %w", memoryDir, err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("legacy memory source missing with residual files in %s", memoryDir)
	}
	if err := os.Remove(memoryDir); err != nil {
		return fmt.Errorf("remove empty migrated memory directory %s: %w", memoryDir, err)
	}
	return nil
}

func readLegacyMemoryPlan(baseDir string) (*legacyMemoryMigrationPlan, bool, error) {
	data, err := os.ReadFile(legacyMemoryPlanPath(baseDir))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read legacy memory plan: %w", err)
	}
	var plan legacyMemoryMigrationPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, false, fmt.Errorf("decode legacy memory plan: %w", err)
	}
	if plan.Version != legacyMemoryPlanVersion || plan.Source != legacyMemoryPlanSource {
		return nil, false, fmt.Errorf("unsupported legacy memory plan in %s", baseDir)
	}
	for _, target := range plan.Targets {
		if _, err := legacyMemoryPlanTargetPath(baseDir, target.Path); err != nil {
			return nil, false, err
		}
	}
	return &plan, true, nil
}

func writeLegacyMemoryPlan(baseDir string, plan *legacyMemoryMigrationPlan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encode legacy memory plan: %w", err)
	}
	if err := WriteFileAtomic(legacyMemoryPlanPath(baseDir), append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write legacy memory plan: %w", err)
	}
	return nil
}

func newLegacyMemoryPlan(ctx context.Context, baseDir, sourceHash string, payload legacyMemoryPayload) (*legacyMemoryMigrationPlan, error) {
	now := time.Now()
	entriesByTarget := make(map[string]*strings.Builder)
	var targetOrder []string
	for _, legacy := range payload.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entryType := safeEntryName(legacy.Type)
		relativeTarget := legacyMemoryTargetRelative(entryType, now)
		builder := entriesByTarget[relativeTarget]
		if builder == nil {
			builder = &strings.Builder{}
			entriesByTarget[relativeTarget] = builder
			targetOrder = append(targetOrder, relativeTarget)
		}
		builder.WriteString(legacyMarkdownEntry(entryType, legacy.Content, now))
	}

	plan := &legacyMemoryMigrationPlan{
		Version:    legacyMemoryPlanVersion,
		Source:     legacyMemoryPlanSource,
		SourceHash: sourceHash,
		Targets:    make([]legacyMemoryMigrationTarget, 0, len(targetOrder)),
	}
	for _, relativeTarget := range targetOrder {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		targetPath, err := legacyMemoryPlanTargetPath(baseDir, relativeTarget)
		if err != nil {
			return nil, err
		}
		existing, err := readLegacyMemoryTarget(targetPath)
		if err != nil {
			return nil, err
		}
		after := string(existing) + entriesByTarget[relativeTarget].String()
		plan.Targets = append(plan.Targets, legacyMemoryMigrationTarget{
			Path:         relativeTarget,
			BeforeSHA256: legacyMemoryContentHash(existing),
			After:        after,
		})
	}
	return plan, nil
}

func legacyMemoryTargetRelative(entryType string, now time.Time) string {
	switch entryType {
	case "decision":
		return "decisions.md"
	case "task":
		return "tasks.md"
	case "error", "command":
		return filepath.ToSlash(filepath.Join("logs", entryType+"-"+now.Format("2006-01-02")+".md"))
	default:
		return filepath.ToSlash(filepath.Join("knowledge", entryType+"-"+now.Format("150405")+".md"))
	}
}

func legacyMemoryPlanTargetPath(baseDir, relativeTarget string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(relativeTarget))
	if filepath.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid legacy memory plan target %q", relativeTarget)
	}
	return filepath.Join(baseDir, cleaned), nil
}

func readLegacyMemoryTarget(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy memory target %q: %w", path, err)
	}
	return data, nil
}

func resumeLegacyMemoryPlan(ctx context.Context, baseDir string, plan *legacyMemoryMigrationPlan) error {
	for _, target := range plan.Targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		targetPath, err := legacyMemoryPlanTargetPath(baseDir, target.Path)
		if err != nil {
			return err
		}
		current, err := readLegacyMemoryTarget(targetPath)
		if err != nil {
			return err
		}
		if string(current) == target.After {
			continue
		}
		if legacyMemoryContentHash(current) != target.BeforeSHA256 {
			return fmt.Errorf("legacy memory target %q changed while migration was incomplete", target.Path)
		}
		if err := WriteFileAtomic(targetPath, []byte(target.After), 0644); err != nil {
			return fmt.Errorf("write legacy memory target %q: %w", target.Path, err)
		}
	}
	if !plan.Completed {
		plan.Completed = true
		if err := writeLegacyMemoryPlan(baseDir, plan); err != nil {
			return err
		}
	}
	return nil
}

func legacyMemoryContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func legacyMemoryAppearsPartiallyImported(ctx context.Context, baseDir string, payload legacyMemoryPayload) (bool, error) {
	var candidates []string
	for _, path := range []string{"decisions.md", "tasks.md"} {
		candidates = append(candidates, filepath.Join(baseDir, path))
	}
	for _, dir := range []string{"logs", "knowledge"} {
		entries, err := os.ReadDir(filepath.Join(baseDir, dir))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("read legacy memory output directory %q: %w", dir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				candidates = append(candidates, filepath.Join(baseDir, dir, entry.Name()))
			}
		}
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		data, err := readLegacyMemoryTarget(candidate)
		if err != nil {
			return false, err
		}
		content := string(data)
		if !strings.Contains(content, "migrated: true\n") {
			continue
		}
		for _, legacy := range payload.Entries {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			entryType := safeEntryName(legacy.Type)
			signature := fmt.Sprintf("## %s\n\n%s\n\n---\n\n", entryType, legacy.Content)
			if strings.Contains(content, signature) {
				return true, nil
			}
		}
	}
	return false, nil
}

// migrateLegacyVaultLayout flattens older project layouts where the vault
// content lived in a "brain" or "obsidian" subdirectory. After this
// runs, baseDir itself is the Obsidian vault and its basename (the
// project SHA) is the vault name shown in Obsidian.
//
// The function is idempotent: if no legacy directory is present it is a
// no-op. If a legacy directory exists alongside files already at baseDir,
// only files that don't collide are moved up; the legacy folder is then
// removed (or kept if it's still non-empty after the move).
func migrateLegacyVaultLayout(baseDir string) error {
	for _, legacy := range []string{"obsidian", "brain"} {
		oldDir := filepath.Join(baseDir, legacy)
		info, err := os.Stat(oldDir)
		if err != nil || !info.IsDir() {
			continue
		}
		if err := flattenInto(oldDir, baseDir); err != nil {
			return fmt.Errorf("migrate legacy vault %q: %w", legacy, err)
		}
		// Best-effort cleanup. If anything remained (collision), leave
		// it in place rather than risk losing user content.
		_ = os.Remove(oldDir)
	}
	return nil
}

func flattenInto(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if _, err := os.Stat(to); err == nil {
			// Don't overwrite. Skip the file so the user can resolve
			// it manually; it stays inside the legacy directory.
			continue
		}
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("move %s -> %s: %w", from, to, err)
		}
	}
	return nil
}

// safeEntryName neutralizes a caller-provided entry type before it can reach a
// filesystem path. Entry types arrive from HTTP and MCP clients, and a value
// like "../../tmp/x" would otherwise escape the vault through the generated
// file name. Only [A-Za-z0-9._-] survive; everything else collapses to "-",
// leading dots and dashes are stripped (no hidden files, no ".." segments), and
// the result is capped to a sane file-name length.
func safeEntryName(entryType string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(entryType) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.TrimLeft(b.String(), "-.")
	if len(name) > 40 {
		name = name[:40]
	}
	if name == "" {
		return "note"
	}
	return name
}

func appendToMarkdown(brainDir, entryType, content string) error {
	entryType = safeEntryName(entryType)
	now := time.Now()
	var targetFile string
	switch entryType {
	case "decision":
		targetFile = filepath.Join(brainDir, "decisions.md")
	case "task":
		targetFile = filepath.Join(brainDir, "tasks.md")
	case "error", "command":
		targetFile = filepath.Join(brainDir, "logs", entryType+"-"+now.Format("2006-01-02")+".md")
	default:
		targetFile = filepath.Join(brainDir, "knowledge", entryType+"-"+now.Format("150405")+".md")
	}
	return appendMarkdownEntry(targetFile, legacyMarkdownEntry(entryType, content, now))
}

func legacyMarkdownEntry(entryType, content string, now time.Time) string {
	frontmatter := fmt.Sprintf(`---
type: %s
date: %s
migrated: true
---

`, entryType, now.Format(time.RFC3339))
	return fmt.Sprintf("%s## %s\n\n%s\n\n---\n\n", frontmatter, entryType, content)
}

func appendMarkdownEntry(targetFile, entry string) error {
	existing, err := readLegacyMemoryTarget(targetFile)
	if err != nil {
		return err
	}
	if err := atomicWriteFile(targetFile, append(existing, []byte(entry)...), 0644); err != nil {
		return fmt.Errorf("write migrated note %q: %w", targetFile, err)
	}
	return nil
}

func (pb *ProjectObsidian) SaveEntry(entryType, content string, tags []string) error {
	// The entry type is client-controlled and becomes part of generated file
	// names downstream; sanitize it before anything branches on it.
	entryType = safeEntryName(entryType)
	pb.mu.Lock()
	defer pb.mu.Unlock()
	defer pb.invalidateStats()

	now := time.Now()
	pb.UpdatedAt = now

	switch entryType {
	case "decision":
		return pb.appendToDecisionsLogLocked(content, now)
	case "task":
		return pb.appendToTasksLogLocked(content, now)
	case "error", "debug", "command", "session":
		return pb.saveToLogsLocked(entryType, content, tags, now)
	default:
		return pb.saveToKnowledgeLocked(entryType, content, tags, now)
	}
}

// SaveContextSnapshot writes the full "rich handoff" note.
//
// In v5 this is no longer the default end-of-task save — SaveCompactSnapshot is
// (spec §19). It remains available for the case the spec explicitly reserves it
// for: a complex handoff where the commands, actions and prose genuinely need to
// survive. Callers reach it through `POST /api/obsidian/context?rich=true`.
func (pb *ProjectObsidian) SaveContextSnapshot(snapshot ContextSnapshot) (path string, retErr error) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	defer pb.invalidateStats()

	if strings.TrimSpace(snapshot.Client) == "" {
		snapshot.Client = "dwyt"
	}
	if strings.TrimSpace(snapshot.Summary) == "" && strings.TrimSpace(snapshot.Context) == "" && strings.TrimSpace(snapshot.UserRequest) == "" {
		snapshot.Summary = "DWYT context snapshot"
	}

	now := time.Now()
	pb.UpdatedAt = now

	dir := filepath.Join(pb.brainDir, "context")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("obsidian context save: %w", err)
	}
	id := fmt.Sprintf("%s_context_%d", now.Format("2006-01-02_150405"), now.UnixNano()%10000)
	path = filepath.Join(dir, id+".md")

	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("obsidian context save: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); retErr == nil && closeErr != nil {
			path = ""
			retErr = fmt.Errorf("obsidian context save close: %w", closeErr)
		}
	}()

	if err := writeContextFrontmatter(f, snapshot, pb, now); err != nil {
		return "", fmt.Errorf("obsidian context save frontmatter: %w", err)
	}
	if err := writeFileString(f, fmt.Sprintf("# Conversation Context - %s\n\n", now.Format("2006-01-02 15:04"))); err != nil {
		return "", fmt.Errorf("obsidian context save heading: %w", err)
	}
	if err := writeVaultLinks(f); err != nil {
		return "", fmt.Errorf("obsidian context save links: %w", err)
	}
	if err := writeFileString(f, fmt.Sprintf("Project: %s\n\nPath: %s\n\nClient: %s\n\n", pb.ProjectName, pb.ProjectPath, snapshot.Client)); err != nil {
		return "", fmt.Errorf("obsidian context save project details: %w", err)
	}
	if snapshot.ConversationID != "" {
		if err := writeFileString(f, fmt.Sprintf("Conversation: %s\n\n", snapshot.ConversationID)); err != nil {
			return "", fmt.Errorf("obsidian context save conversation: %w", err)
		}
	}
	for _, section := range []struct {
		title   string
		content string
	}{
		{title: "User Request", content: snapshot.UserRequest},
		{title: "Summary", content: snapshot.Summary},
		{title: "Context", content: snapshot.Context},
		{title: "Outcome", content: snapshot.Outcome},
	} {
		if err := writeMarkdownSection(f, section.title, section.content); err != nil {
			return "", fmt.Errorf("obsidian context save %s: %w", strings.ToLower(strings.ReplaceAll(section.title, " ", "-")), err)
		}
	}
	for _, list := range []struct {
		title string
		items []string
	}{
		{title: "Files", items: snapshot.Files},
		{title: "Decisions", items: snapshot.Decisions},
		{title: "Actions", items: snapshot.Actions},
		{title: "Commands", items: snapshot.Commands},
		{title: "Errors", items: snapshot.Errors},
		{title: "Next Steps", items: snapshot.NextSteps},
	} {
		if err := writeMarkdownList(f, list.title, list.items); err != nil {
			return "", fmt.Errorf("obsidian context save %s: %w", strings.ToLower(strings.ReplaceAll(list.title, " ", "-")), err)
		}
	}
	if len(snapshot.Metadata) > 0 {
		if err := writeFileString(f, "## Metadata\n\n"); err != nil {
			return "", fmt.Errorf("obsidian context save metadata heading: %w", err)
		}
		keys := make([]string, 0, len(snapshot.Metadata))
		for k := range snapshot.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := writeFileString(f, fmt.Sprintf("- %s: %s\n", k, snapshot.Metadata[k])); err != nil {
				return "", fmt.Errorf("obsidian context save metadata value: %w", err)
			}
		}
		if err := writeFileString(f, "\n"); err != nil {
			return "", fmt.Errorf("obsidian context save metadata spacing: %w", err)
		}
	}

	return path, nil
}

func (pb *ProjectObsidian) appendToDecisionsLogLocked(content string, now time.Time) error {
	path := filepath.Join(pb.brainDir, "decisions", "index.md")
	entry := fmt.Sprintf("\n### %s\n\nLinks: [[decisions/index]] [[maps/project-map]] [[instructions/codebase-law]] [[instructions/obsidian-law]]\n\n%s\n\n*%s*\n\n---\n", now.Format("2006-01-02 15:04"), content, now.Format(time.RFC3339))
	return appendFile(path, entry)
}

func (pb *ProjectObsidian) appendToTasksLogLocked(content string, now time.Time) error {
	path := filepath.Join(pb.brainDir, "tasks", "index.md")
	entry := fmt.Sprintf("\n- [ ] %s *(added %s)* — [[tasks/index]] [[maps/project-map]]\n", content, now.Format("2006-01-02 15:04"))
	return appendFile(path, entry)
}

func (pb *ProjectObsidian) saveToLogsLocked(entryType, content string, tags []string, now time.Time) (retErr error) {
	dir := filepath.Join(pb.brainDir, "logs")
	switch entryType {
	case "error", "debug":
		dir = filepath.Join(pb.brainDir, "debug")
	case "command":
		dir = filepath.Join(pb.brainDir, "logs", "commands")
	case "session":
		dir = filepath.Join(pb.brainDir, "logs", "sessions")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("obsidian save: create log directory: %w", err)
	}
	id := fmt.Sprintf("%s_%s_%d", now.Format("2006-01-02_1504"), entryType, now.UnixNano()%10000)
	path := filepath.Join(dir, id+".md")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("obsidian save: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("obsidian save: close log entry: %w", closeErr)
		}
	}()
	if err := writeFrontmatter(f, entryType, tags, now); err != nil {
		return fmt.Errorf("obsidian save: write log frontmatter: %w", err)
	}
	if err := writeFileString(f, fmt.Sprintf("# %s\n\n", entryType)); err != nil {
		return fmt.Errorf("obsidian save: write log heading: %w", err)
	}
	if err := writeVaultLinks(f); err != nil {
		return fmt.Errorf("obsidian save: write log links: %w", err)
	}
	if err := writeFileString(f, content+"\n"); err != nil {
		return fmt.Errorf("obsidian save: write log content: %w", err)
	}
	return nil
}

func (pb *ProjectObsidian) saveToKnowledgeLocked(entryType, content string, tags []string, now time.Time) (retErr error) {
	dir := filepath.Join(pb.brainDir, "knowledge")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("obsidian save: create knowledge directory: %w", err)
	}
	id := fmt.Sprintf("%s_%s_%d", now.Format("2006-01-02_1504"), entryType, now.UnixNano()%10000)
	path := filepath.Join(dir, id+".md")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("obsidian save: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("obsidian save: close knowledge entry: %w", closeErr)
		}
	}()
	if err := writeFrontmatter(f, entryType, tags, now); err != nil {
		return fmt.Errorf("obsidian save: write knowledge frontmatter: %w", err)
	}
	title := content
	if len(title) > 60 {
		title = title[:57] + "..."
	}
	if err := writeFileString(f, fmt.Sprintf("# %s\n\n", title)); err != nil {
		return fmt.Errorf("obsidian save: write knowledge heading: %w", err)
	}
	if err := writeVaultLinks(f); err != nil {
		return fmt.Errorf("obsidian save: write knowledge links: %w", err)
	}
	if err := writeFileString(f, content+"\n"); err != nil {
		return fmt.Errorf("obsidian save: write knowledge content: %w", err)
	}
	return nil
}

func appendFile(path, content string) (retErr error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); retErr == nil && closeErr != nil {
			retErr = closeErr
		}
	}()
	_, err = f.WriteString(content)
	return err
}

// Search is the pre-v5 broad substring search: every matching note, ordered by
// modification time, capped at 30.
//
// SearchV2 is the v5 default for agent-facing retrieval (spec §27) because this
// shape is the wrong one for a token budget — the newest 30 notes are mostly
// session history and the caller cannot ask for the two decisions it needed.
// Search stays for local tooling and diagnostics that genuinely want "every
// note containing this string".
func (pb *ProjectObsidian) Search(query string) []BrainEntry {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	var results []BrainEntry
	query = strings.ToLower(query)

	walkErr := filepath.Walk(pb.brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walk vault path %s: %w", path, err)
		}
		if info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read vault note %s: %w", path, err)
		}
		content := string(data)
		if strings.Contains(strings.ToLower(content), query) {
			entryType := detectType(pb.brainDir, path)
			results = append(results, BrainEntry{
				ID:        info.Name(),
				Type:      entryType,
				Content:   extractContent(content),
				Title:     extractTitle(content),
				CreatedAt: info.ModTime(),
				FilePath:  path,
			})
		}
		return nil
	})
	if walkErr != nil {
		log.Warn("obsidian search incomplete", log.Fields{"path": pb.brainDir, "error": walkErr.Error()})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})
	if len(results) > 30 {
		results = results[:30]
	}
	return results
}

func detectType(brainDir, path string) string {
	rel, _ := filepath.Rel(brainDir, path)
	base := filepath.Base(path)
	switch {
	case base == "decisions.md":
		return "decision"
	case base == "tasks.md":
		return "task"
	case strings.HasPrefix(rel, "decisions/"):
		return "decision"
	case strings.HasPrefix(rel, "tasks/"):
		return "task"
	case strings.HasPrefix(rel, "debug/"):
		return "debug"
	case strings.HasPrefix(rel, "context/"):
		return "context"
	case strings.HasPrefix(rel, "logs/"):
		return "log"
	case strings.HasPrefix(rel, "knowledge/"):
		return "knowledge"
	default:
		return "note"
	}
}

func (pb *ProjectObsidian) RebuildSummary() string {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	defer pb.invalidateStats()

	var parts []string
	typeCount := map[string]int{}

	if err := filepath.Walk(pb.brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walk vault path %s: %w", path, err)
		}
		if info.IsDir() || filepath.Ext(path) != ".md" || filepath.Base(path) == "context.md" {
			return nil
		}
		entryType := detectType(pb.brainDir, path)
		typeCount[entryType]++

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read vault note %s: %w", path, err)
		}
		content := string(data)
		if title := extractTitle(content); title != "" {
			parts = append(parts, title)
		}
		return nil
	}); err != nil {
		log.Warn("obsidian summary rebuild incomplete", log.Fields{"path": pb.brainDir, "error": err.Error()})
		return pb.Summary
	}

	summary := fmt.Sprintf("# %s — Project Brain\n\n", pb.ProjectName)
	summary += fmt.Sprintf("**Last updated:** %s\n\n", time.Now().Format(time.RFC3339))
	summary += fmt.Sprintf("## Summary\n\n%d entries: %s\n\n", totalCount(typeCount), formatTypeCount(typeCount))
	summary += "## Recent Activity\n\n"
	recent := parts
	if len(recent) > 8 {
		recent = recent[len(recent)-8:]
	}
	for _, p := range recent {
		summary += fmt.Sprintf("- %s\n", p)
	}

	pb.Summary = summary
	pb.UpdatedAt = time.Now()
	contextFile := filepath.Join(pb.brainDir, "context.md")
	if err := atomicWriteFile(contextFile, []byte(summary), 0644); err != nil {
		log.Warn("obsidian: failed to persist summary", log.Fields{"path": contextFile, "error": err.Error()})
	}
	return summary
}

func (pb *ProjectObsidian) Stats() map[string]interface{} {
	stats, _ := pb.StatsContext(context.Background())
	return stats
}

func (pb *ProjectObsidian) StatsContext(ctx context.Context) (map[string]interface{}, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Short TTL cache: the dashboard polls every few seconds and several
	// handlers call Stats() within a single poll. Walking the vault each time
	// is wasteful, so reuse a recent result. Writes invalidate via invalidateStats.
	pb.statsMu.Lock()
	if pb.statsCache != nil && time.Since(pb.statsAt) < 2*time.Second {
		cached := pb.statsCache
		pb.statsMu.Unlock()
		return cached, nil
	}
	pb.statsMu.Unlock()

	pb.mu.RLock()
	defer pb.mu.RUnlock()

	typeCount := map[string]int{}
	totalFiles := 0
	var totalBytes int64

	walkErr := filepath.Walk(pb.brainDir, func(path string, info os.FileInfo, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("walk vault path %s: %w", path, err)
		}
		if info.IsDir() || filepath.Ext(path) != ".md" || filepath.Base(path) == "context.md" {
			return nil
		}
		totalFiles++
		totalBytes += info.Size()
		entryType := detectType(pb.brainDir, path)
		typeCount[entryType]++
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	result := map[string]interface{}{
		"project_id":    pb.ProjectID,
		"project_name":  pb.ProjectName,
		"project_path":  pb.ProjectPath,
		"total_files":   totalFiles,
		"total_bytes":   totalBytes,
		"files_by_type": typeCount,
		"has_summary":   pb.Summary != "",
		"summary":       pb.Summary,
		"last_updated":  pb.UpdatedAt.Format(time.RFC3339),
		"ai_enabled":    pb.AIEnabled,
		"tools_enabled": pb.ToolsEnabled,
		"obsidian_dir":  pb.brainDir,
	}

	pb.statsMu.Lock()
	pb.statsCache = result
	pb.statsAt = time.Now()
	pb.statsMu.Unlock()
	return result, nil
}

// invalidateStats drops the cached Stats() result so the next read reflects a
// just-written note immediately.
func (pb *ProjectObsidian) invalidateStats() {
	pb.statsMu.Lock()
	pb.statsCache = nil
	pb.statsMu.Unlock()
}

func (pb *ProjectObsidian) SetConfig(aiEnabled, toolsEnabled []string) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.AIEnabled = aiEnabled
	pb.ToolsEnabled = toolsEnabled
}

func (pb *ProjectObsidian) OpenInObsidian() error {
	if err := pb.RegisterObsidianVault(); err != nil {
		return err
	}
	openPath := filepath.Join(pb.brainDir, "index.md")
	vaultURI := "obsidian://open?path=" + url.QueryEscape(openPath)
	if err := platform.OpenURL(vaultURI); err != nil {
		return fmt.Errorf("obsidian: failed to open vault via URI: %w", err)
	}
	return nil
}

func (pb *ProjectObsidian) RegisterObsidianVault() error {
	configPath, err := obsidianConfigPath()
	if err != nil {
		return err
	}

	config := map[string]interface{}{}
	if data, err := os.ReadFile(configPath); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("obsidian: decode vault registry: %w", err)
		}
	}

	vaults, _ := config["vaults"].(map[string]interface{})
	if vaults == nil {
		vaults = map[string]interface{}{}
	}

	entry := map[string]interface{}{
		"path": pb.brainDir,
		"ts":   time.Now().UnixMilli(),
		"open": true,
	}
	found := false
	for id, raw := range vaults {
		v, ok := raw.(map[string]interface{})
		if !ok || v["path"] != pb.brainDir {
			continue
		}
		vaults[id] = entry
		found = true
	}
	if !found {
		vaults[pb.ProjectID] = entry
	}
	config["vaults"] = vaults

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("obsidian: failed to encode vault registry: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("obsidian: failed to create config dir: %w", err)
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("obsidian: failed to register vault: %w", err)
	}
	return nil
}

func obsidianConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("obsidian: cannot locate home dir: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "obsidian", "obsidian.json"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "obsidian", "obsidian.json"), nil
	default:
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		return filepath.Join(configHome, "obsidian", "obsidian.json"), nil
	}
}

func (pb *ProjectObsidian) OpenBrainDir() error {
	return platform.OpenPath(pb.brainDir)
}

func (pb *ProjectObsidian) GetBrainDir() string {
	return pb.brainDir
}

// CountVaultFiles returns the number of markdown notes in a project's existing
// vault WITHOUT creating, seeding, or migrating it. The bool is false when no
// vault directory exists yet. This is the read-only path used by list/context
// endpoints, so a GET never mutates the filesystem.
//
// It accepts both layouts — "<hash>_<name>" (canonical) and "<hash>" (legacy,
// still present while a migration is pending) — so list endpoints don't
// report "no vault" the moment a user upgrades before the migration pass
// runs.
func CountVaultFiles(dwytHome, projectPath string) (int, bool) {
	hash := db.HashPath(projectPath)
	projectName := filepath.Base(projectPath)
	candidates := []string{
		filepath.Join(dwytHome, "projects", VaultDirectoryName(hash, projectName)),
		filepath.Join(dwytHome, "projects", hash),
	}
	for _, baseDir := range candidates {
		if err := safePath(dwytHome, baseDir); err != nil {
			continue
		}
		info, err := os.Stat(baseDir)
		if err != nil || !info.IsDir() {
			continue
		}
		count := 0
		if err := filepath.Walk(baseDir, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return fmt.Errorf("walk vault path %s: %w", path, err)
			}
			if fi.IsDir() || filepath.Ext(path) != ".md" || filepath.Base(path) == "context.md" {
				return nil
			}
			count++
			return nil
		}); err != nil {
			log.Warn("vault file count incomplete", log.Fields{"path": baseDir, "error": err.Error()})
			continue
		}
		return count, true
	}
	return 0, false
}

func AutoSaveSession(pb *ProjectObsidian, tag string) error {
	content := fmt.Sprintf("Session %s at %s\n\nProject: %s\nPath: %s",
		tag, time.Now().Format(time.RFC3339), pb.ProjectName, pb.ProjectPath)
	return pb.SaveEntry("session", content, []string{"dwyt", "session", tag})
}

func AutoSaveDecision(pb *ProjectObsidian, decision string) error {
	return pb.SaveEntry("decision", decision, []string{"dwyt", "decision"})
}

func AutoSaveError(pb *ProjectObsidian, errStr, solution string) error {
	content := fmt.Sprintf("Error: %s\n\nSolution: %s", errStr, solution)
	return pb.SaveEntry("error", content, []string{"dwyt", "error"})
}

func AutoSaveCommand(pb *ProjectObsidian, command string) error {
	if len(command) > 500 {
		command = command[:497] + "..."
	}
	content := fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), command)
	return pb.SaveEntry("note", content, []string{"dwyt", "command"})
}

// writeFrontmatter emits the note header, including the v5 lifecycle block
// (spec §22, §25) so the housekeeper can classify retention, detect staleness
// and apply usage-aware retention without reading the body.
//
// The legacy `date` field is kept alongside `created_at` because existing
// Obsidian views and user queries reference it; dropping it would break vaults
// silently.
func writeFrontmatter(f *os.File, entryType string, tags []string, date time.Time) error {
	allTags := []string{"dwyt", entryType}
	allTags = append(allTags, tags...)
	for _, content := range []string{
		"---\n",
		fmt.Sprintf("tags: [%s]\n", strings.Join(allTags, ", ")),
		fmt.Sprintf("date: %s\n", date.Format(time.RFC3339)),
		NewLifecycle(entryType, date).Render(),
		"---\n\n",
	} {
		if err := writeFileString(f, content); err != nil {
			return err
		}
	}
	return nil
}

func writeContextFrontmatter(f *os.File, snapshot ContextSnapshot, pb *ProjectObsidian, date time.Time) error {
	parts := []string{
		"---\n",
		"tags: [dwyt, context, session, conversation]\n",
		fmt.Sprintf("date: %s\n", date.Format(time.RFC3339)),
		NewLifecycle("context", date).Render(),
		fmt.Sprintf("client: %q\n", snapshot.Client),
		fmt.Sprintf("project: %q\n", pb.ProjectName),
		fmt.Sprintf("project_path: %q\n", pb.ProjectPath),
	}
	if snapshot.ConversationID != "" {
		parts = append(parts, fmt.Sprintf("conversation_id: %q\n", snapshot.ConversationID))
	}
	parts = append(parts, "---\n\n")
	for _, content := range parts {
		if err := writeFileString(f, content); err != nil {
			return err
		}
	}
	return nil
}

func writeVaultLinks(f *os.File) error {
	return writeFileString(f, "Links: [[index]] [[maps/project-map]] [[instructions/obsidian-law]] [[instructions/codebase-law]]\n\n")
}

func writeMarkdownSection(f *os.File, title, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return writeFileString(f, fmt.Sprintf("## %s\n\n%s\n\n", title, content))
}

func writeMarkdownList(f *os.File, title string, items []string) error {
	if len(items) == 0 {
		return nil
	}
	if err := writeFileString(f, fmt.Sprintf("## %s\n\n", title)); err != nil {
		return err
	}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if err := writeFileString(f, fmt.Sprintf("- %s\n", strings.ReplaceAll(item, "\n", "\n  "))); err != nil {
			return err
		}
	}
	return writeFileString(f, "\n")
}

func writeFileString(f *os.File, content string) error {
	_, err := f.WriteString(content)
	return err
}

func extractTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

func extractContent(content string) string {
	inFM := false
	fmCount := 0
	var body []string
	for _, line := range strings.Split(content, "\n") {
		if line == "---" {
			fmCount++
			if fmCount == 1 {
				inFM = true
				continue
			} else if inFM {
				inFM = false
				continue
			}
		}
		if !inFM {
			body = append(body, line)
		}
	}
	result := strings.TrimSpace(strings.Join(body, "\n"))
	if len(result) > 300 {
		result = result[:297] + "..."
	}
	return result
}

func totalCount(tc map[string]int) int {
	total := 0
	for _, c := range tc {
		total += c
	}
	return total
}

func formatTypeCount(tc map[string]int) string {
	var parts []string
	for t, c := range tc {
		parts = append(parts, fmt.Sprintf("%d %s", c, t))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

type ProjectMeta struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	CreatedAt     time.Time `json:"created_at"`
	LastOpen      time.Time `json:"last_open"`
	ToolsEnabled  []string  `json:"tools_enabled"`
	AIEnabled     []string  `json:"ai_enabled"`
	ObsidianFiles int       `json:"obsidian_files"`
}

func ensureBrainJSON(baseDir, projectPath string) error {
	projFile := filepath.Join(baseDir, "project.json")
	meta := ProjectMeta{
		Name:      filepath.Base(projectPath),
		Path:      projectPath,
		CreatedAt: time.Now(),
		LastOpen:  time.Now(),
	}
	if data, err := os.ReadFile(projFile); err == nil {
		if err := json.Unmarshal(data, &meta); err != nil {
			return fmt.Errorf("obsidian: decode project metadata %q: %w", projFile, err)
		}
		meta.LastOpen = time.Now()
		meta.Path = projectPath
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("obsidian: read project metadata %q: %w", projFile, err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("obsidian: encode project metadata %q: %w", projFile, err)
	}
	if err := os.WriteFile(projFile, data, 0644); err != nil {
		return fmt.Errorf("obsidian: write project metadata %q: %w", projFile, err)
	}
	return nil
}
