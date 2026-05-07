package brain

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/db"
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

func NewProjectObsidian(dwytHome, projectPath string) (*ProjectObsidian, error) {
	id := db.HashPath(projectPath)
	baseDir := filepath.Join(dwytHome, "projects", id)
	if err := safePath(dwytHome, baseDir); err != nil {
		return nil, err
	}

	// Migrate old "brain" folder to "obsidian" if it exists
	oldDir := filepath.Join(baseDir, "brain")
	newDir := filepath.Join(baseDir, "obsidian")
	if _, err := os.Stat(oldDir); err == nil {
		if _, err2 := os.Stat(newDir); os.IsNotExist(err2) {
			os.Rename(oldDir, newDir)
		}
	}

	brainDir := newDir
	os.MkdirAll(brainDir, 0755)

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
	}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(brainDir, d), 0755)
	}

	pb := &ProjectObsidian{
		ProjectID:   id,
		ProjectName: filepath.Base(projectPath),
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

	ensureBrainJSON(baseDir, projectPath)
	ensureSeedFiles(brainDir)
	return pb, nil
}

func ensureSeedFiles(brainDir string) {
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
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.MkdirAll(filepath.Dir(path), 0755)
			os.WriteFile(path, []byte(content), 0644)
		}
	}
}

func MigrateOldMemoryDirs(dwytHome string) error {
	projectsDir := filepath.Join(dwytHome, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		memoryDir := filepath.Join(projectsDir, entry.Name(), "memory")
		if info, err := os.Stat(memoryDir); err == nil && info.IsDir() {
			memoryFile := filepath.Join(memoryDir, "memory.json")
			if data, err := os.ReadFile(memoryFile); err == nil && len(data) > 2 {
				brainDir := filepath.Join(projectsDir, entry.Name(), "obsidian")
				os.MkdirAll(filepath.Join(brainDir, "knowledge"), 0755)
				os.MkdirAll(filepath.Join(brainDir, "logs"), 0755)
				ensureSeedFiles(brainDir)
				var pm struct {
					Entries []struct {
						Type    string `json:"type"`
						Content string `json:"content"`
					} `json:"entries"`
				}
				if err := json.Unmarshal(data, &pm); err == nil {
					for _, e := range pm.Entries {
						appendToMarkdown(brainDir, e.Type, e.Content)
					}
				}
			}
			os.RemoveAll(memoryDir)
		}
	}
	return nil
}

func appendToMarkdown(brainDir, entryType, content string) {
	var targetFile string
	switch entryType {
	case "decision":
		targetFile = filepath.Join(brainDir, "decisions.md")
	case "task":
		targetFile = filepath.Join(brainDir, "tasks.md")
	case "error", "command":
		targetFile = filepath.Join(brainDir, "logs", entryType+"-"+time.Now().Format("2006-01-02")+".md")
	default:
		targetFile = filepath.Join(brainDir, "knowledge", entryType+"-"+time.Now().Format("150405")+".md")
	}

	frontmatter := fmt.Sprintf(`---
type: %s
date: %s
migrated: true
---

`, entryType, time.Now().Format(time.RFC3339))

	existing := ""
	if data, err := os.ReadFile(targetFile); err == nil {
		existing = string(data)
	}
	entry := fmt.Sprintf("%s## %s\n\n%s\n\n---\n\n", frontmatter, entryType, content)
	os.WriteFile(targetFile, []byte(existing+entry), 0644)
}

func (pb *ProjectObsidian) SaveEntry(entryType, content string, tags []string) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

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

func (pb *ProjectObsidian) SaveContextSnapshot(snapshot ContextSnapshot) (string, error) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

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
	path := filepath.Join(dir, id+".md")

	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("obsidian context save: %w", err)
	}
	defer f.Close()

	writeContextFrontmatter(f, snapshot, pb, now)
	fmt.Fprintf(f, "# Conversation Context - %s\n\n", now.Format("2006-01-02 15:04"))
	writeVaultLinks(f)
	fmt.Fprintf(f, "Project: %s\n\nPath: %s\n\nClient: %s\n\n", pb.ProjectName, pb.ProjectPath, snapshot.Client)
	if snapshot.ConversationID != "" {
		fmt.Fprintf(f, "Conversation: %s\n\n", snapshot.ConversationID)
	}
	writeMarkdownSection(f, "User Request", snapshot.UserRequest)
	writeMarkdownSection(f, "Summary", snapshot.Summary)
	writeMarkdownSection(f, "Context", snapshot.Context)
	writeMarkdownSection(f, "Outcome", snapshot.Outcome)
	writeMarkdownList(f, "Files", snapshot.Files)
	writeMarkdownList(f, "Decisions", snapshot.Decisions)
	writeMarkdownList(f, "Actions", snapshot.Actions)
	writeMarkdownList(f, "Commands", snapshot.Commands)
	writeMarkdownList(f, "Errors", snapshot.Errors)
	writeMarkdownList(f, "Next Steps", snapshot.NextSteps)
	if len(snapshot.Metadata) > 0 {
		fmt.Fprintln(f, "## Metadata")
		fmt.Fprintln(f)
		keys := make([]string, 0, len(snapshot.Metadata))
		for k := range snapshot.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(f, "- %s: %s\n", k, snapshot.Metadata[k])
		}
		fmt.Fprintln(f)
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

func (pb *ProjectObsidian) saveToLogsLocked(entryType, content string, tags []string, now time.Time) error {
	dir := filepath.Join(pb.brainDir, "logs")
	switch entryType {
	case "error", "debug":
		dir = filepath.Join(pb.brainDir, "debug")
	case "command":
		dir = filepath.Join(pb.brainDir, "logs", "commands")
	case "session":
		dir = filepath.Join(pb.brainDir, "logs", "sessions")
	}
	os.MkdirAll(dir, 0755)
	id := fmt.Sprintf("%s_%s_%d", now.Format("2006-01-02_1504"), entryType, now.UnixNano()%10000)
	path := filepath.Join(dir, id+".md")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("obsidian save: %w", err)
	}
	defer f.Close()
	writeFrontmatter(f, entryType, tags, now)
	fmt.Fprintf(f, "# %s\n\n", entryType)
	writeVaultLinks(f)
	fmt.Fprintf(f, "%s\n", content)
	return nil
}

func (pb *ProjectObsidian) saveToKnowledgeLocked(entryType, content string, tags []string, now time.Time) error {
	dir := filepath.Join(pb.brainDir, "knowledge")
	os.MkdirAll(dir, 0755)
	id := fmt.Sprintf("%s_%s_%d", now.Format("2006-01-02_1504"), entryType, now.UnixNano()%10000)
	path := filepath.Join(dir, id+".md")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("obsidian save: %w", err)
	}
	defer f.Close()
	writeFrontmatter(f, entryType, tags, now)
	title := content
	if len(title) > 60 {
		title = title[:57] + "..."
	}
	fmt.Fprintf(f, "# %s\n\n", title)
	writeVaultLinks(f)
	fmt.Fprintf(f, "%s\n", content)
	return nil
}

func appendFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

func (pb *ProjectObsidian) Search(query string) []BrainEntry {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	var results []BrainEntry
	query = strings.ToLower(query)

	filepath.Walk(pb.brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
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

	var parts []string
	typeCount := map[string]int{}

	filepath.Walk(pb.brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" || filepath.Base(path) == "context.md" {
			return nil
		}
		entryType := detectType(pb.brainDir, path)
		typeCount[entryType]++

		data, _ := os.ReadFile(path)
		content := string(data)
		if title := extractTitle(content); title != "" {
			parts = append(parts, title)
		}
		return nil
	})

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
	os.WriteFile(contextFile, []byte(summary), 0644)
	return summary
}

func (pb *ProjectObsidian) Stats() map[string]interface{} {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	typeCount := map[string]int{}
	totalFiles := 0
	var totalBytes int64

	filepath.Walk(pb.brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" || filepath.Base(path) == "context.md" {
			return nil
		}
		totalFiles++
		totalBytes += info.Size()
		entryType := detectType(pb.brainDir, path)
		typeCount[entryType]++
		return nil
	})

	return map[string]interface{}{
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
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", vaultURI)
	} else if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "start", "", vaultURI)
	} else {
		cmd = exec.Command("xdg-open", vaultURI)
	}
	if err := cmd.Start(); err != nil {
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
		json.Unmarshal(data, &config)
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
	cmd := exec.Command("xdg-open", pb.brainDir)
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", pb.brainDir)
	} else if runtime.GOOS == "windows" {
		cmd = exec.Command("explorer", pb.brainDir)
	}
	return cmd.Start()
}

func (pb *ProjectObsidian) GetBrainDir() string {
	return pb.brainDir
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

func writeFrontmatter(f *os.File, entryType string, tags []string, date time.Time) {
	allTags := []string{"dwyt", entryType}
	allTags = append(allTags, tags...)
	fmt.Fprintf(f, "---\n")
	fmt.Fprintf(f, "tags: [%s]\n", strings.Join(allTags, ", "))
	fmt.Fprintf(f, "date: %s\n", date.Format(time.RFC3339))
	fmt.Fprintf(f, "type: %s\n", entryType)
	fmt.Fprintf(f, "---\n\n")
}

func writeContextFrontmatter(f *os.File, snapshot ContextSnapshot, pb *ProjectObsidian, date time.Time) {
	fmt.Fprintf(f, "---\n")
	fmt.Fprintf(f, "tags: [dwyt, context, session, conversation]\n")
	fmt.Fprintf(f, "date: %s\n", date.Format(time.RFC3339))
	fmt.Fprintf(f, "type: context\n")
	fmt.Fprintf(f, "client: %q\n", snapshot.Client)
	fmt.Fprintf(f, "project: %q\n", pb.ProjectName)
	fmt.Fprintf(f, "project_path: %q\n", pb.ProjectPath)
	if snapshot.ConversationID != "" {
		fmt.Fprintf(f, "conversation_id: %q\n", snapshot.ConversationID)
	}
	fmt.Fprintf(f, "---\n\n")
}

func writeVaultLinks(f *os.File) {
	fmt.Fprintln(f, "Links: [[index]] [[maps/project-map]] [[instructions/obsidian-law]] [[instructions/codebase-law]]")
	fmt.Fprintln(f)
}

func writeMarkdownSection(f *os.File, title, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	fmt.Fprintf(f, "## %s\n\n%s\n\n", title, content)
}

func writeMarkdownList(f *os.File, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(f, "## %s\n\n", title)
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		fmt.Fprintf(f, "- %s\n", strings.ReplaceAll(item, "\n", "\n  "))
	}
	fmt.Fprintln(f)
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

func ensureBrainJSON(baseDir, projectPath string) {
	projFile := filepath.Join(baseDir, "project.json")
	meta := ProjectMeta{
		Name:      filepath.Base(projectPath),
		Path:      projectPath,
		CreatedAt: time.Now(),
		LastOpen:  time.Now(),
	}
	if data, err := os.ReadFile(projFile); err == nil {
		json.Unmarshal(data, &meta)
		meta.LastOpen = time.Now()
		meta.Path = projectPath
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	os.WriteFile(projFile, data, 0644)
}
