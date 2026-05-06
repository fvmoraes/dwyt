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

	dirs := []string{"knowledge", "logs", ".obsidian"}
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
- **context.md** — Full project summary
- **decisions.md** — Architecture and design decisions
- **tasks.md** — Active tasks and progress
- **knowledge/** — Knowledge base articles
- **logs/** — Session logs and command history
`,
		"decisions.md": `---
type: decisions
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [decisions, architecture]
---

# Decisions Log

## Recent Decisions
`,
		"tasks.md": `---
type: tasks
updated_at: ` + time.Now().Format(time.RFC3339) + `
tags: [tasks, progress]
---

# Tasks

## Active
`,
	}
	for name, content := range seeds {
		path := filepath.Join(brainDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
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
	case "error", "command", "session":
		return pb.saveToLogsLocked(entryType, content, tags, now)
	default:
		return pb.saveToKnowledgeLocked(entryType, content, tags, now)
	}
}

func (pb *ProjectObsidian) appendToDecisionsLogLocked(content string, now time.Time) error {
	path := filepath.Join(pb.brainDir, "decisions.md")
	entry := fmt.Sprintf("\n### %s\n\n%s\n\n*%s*\n\n---\n", now.Format("2006-01-02 15:04"), content, now.Format(time.RFC3339))
	return appendFile(path, entry)
}

func (pb *ProjectObsidian) appendToTasksLogLocked(content string, now time.Time) error {
	path := filepath.Join(pb.brainDir, "tasks.md")
	entry := fmt.Sprintf("\n- [ ] %s *(added %s)*\n", content, now.Format("2006-01-02 15:04"))
	return appendFile(path, entry)
}

func (pb *ProjectObsidian) saveToLogsLocked(entryType, content string, tags []string, now time.Time) error {
	dir := filepath.Join(pb.brainDir, "logs")
	os.MkdirAll(dir, 0755)
	id := fmt.Sprintf("%s_%s_%d", now.Format("2006-01-02_1504"), entryType, now.UnixNano()%10000)
	path := filepath.Join(dir, id+".md")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("obsidian save: %w", err)
	}
	defer f.Close()
	writeFrontmatter(f, entryType, tags, now)
	fmt.Fprintf(f, "# %s\n\n%s\n", entryType, content)
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
	fmt.Fprintf(f, "# %s\n\n%s\n", title, content)
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

	filepath.Walk(pb.brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" || filepath.Base(path) == "context.md" {
			return nil
		}
		totalFiles++
		entryType := detectType(pb.brainDir, path)
		typeCount[entryType]++
		return nil
	})

	return map[string]interface{}{
		"project_id":    pb.ProjectID,
		"project_name":  pb.ProjectName,
		"project_path":  pb.ProjectPath,
		"total_files":   totalFiles,
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
	vaultURI := "obsidian://open?path=" + url.PathEscape(pb.brainDir)
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", vaultURI)
	} else if runtime.GOOS == "windows" {
		cmd = exec.Command("start", vaultURI)
	} else {
		cmd = exec.Command("xdg-open", vaultURI)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("obsidian: failed to open vault via URI: %w", err)
	}
	return nil
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

func ObsidianInstalled() bool {
	if _, err := exec.LookPath("obsidian"); err == nil {
		return true
	}
	home, _ := os.UserHomeDir()
	locations := []string{
		filepath.Join(home, ".local", "bin", "obsidian"),
		"/usr/bin/obsidian",
		"/usr/local/bin/obsidian",
		"/opt/obsidian/obsidian",
		filepath.Join(home, "AppData", "Local", "obsidian", "obsidian.exe"),
		"/Applications/Obsidian.app/Contents/MacOS/Obsidian",
	}
	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return true
		}
	}
	return false
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
