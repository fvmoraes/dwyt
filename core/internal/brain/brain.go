package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
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

type ProjectBrain struct {
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

func NewProjectBrain(dwytHome, projectPath string) (*ProjectBrain, error) {
	id := hashPath(projectPath)
	baseDir := filepath.Join(dwytHome, "projects", id)
	brainDir := filepath.Join(baseDir, "brain")
	os.MkdirAll(brainDir, 0755)

	dirs := []string{"sessions", "decisions", "errors", "notes", ".obsidian"}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(brainDir, d), 0755)
	}

	pb := &ProjectBrain{
		ProjectID:   id,
		ProjectName: filepath.Base(projectPath),
		ProjectPath: projectPath,
		baseDir:     baseDir,
		brainDir:    brainDir,
	}

	contextFile := filepath.Join(brainDir, "context.md")
	if data, err := os.ReadFile(contextFile); err == nil {
		pb.Summary = string(data)
		pb.CreatedAt = time.Now()
		if info, err := os.Stat(contextFile); err == nil {
			pb.CreatedAt = info.ModTime()
		}
	} else {
		pb.CreatedAt = time.Now()
		pb.RebuildSummary()
	}

	ensureBrainJSON(baseDir, projectPath)
	return pb, nil
}

func (pb *ProjectBrain) SaveEntry(entryType, content string, tags []string) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	now := time.Now()
	id := fmt.Sprintf("%s_%s_%d", now.Format("2006-01-02_1504"), entryType, now.UnixNano()%10000)
	title := content
	if len(title) > 60 {
		title = title[:57] + "..."
	}

	dir := filepath.Join(pb.brainDir, entryType+"s")
	os.MkdirAll(dir, 0755)

	fileName := id + ".md"
	filePath := filepath.Join(dir, fileName)

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("brain save: %w", err)
	}
	defer f.Close()

	writeFrontmatter(f, entryType, tags, now)
	fmt.Fprintf(f, "# %s\n\n%s\n", title, content)

	pb.UpdatedAt = now
	return nil
}

func (pb *ProjectBrain) Search(query string) []BrainEntry {
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
			entryType := "note"
			relPath, _ := filepath.Rel(pb.brainDir, path)
			parts := strings.Split(relPath, string(filepath.Separator))
			if len(parts) >= 2 {
				entryType = strings.TrimSuffix(parts[0], "s")
			}
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
	if len(results) > 20 {
		results = results[:20]
	}
	return results
}

func (pb *ProjectBrain) RebuildSummary() string {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	var parts []string
	typeCount := map[string]int{}

	filepath.Walk(pb.brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" || filepath.Base(path) == "context.md" {
			return nil
		}
		entryType := "note"
		relPath, _ := filepath.Rel(pb.brainDir, path)
		parts2 := strings.Split(relPath, string(filepath.Separator))
		if len(parts2) >= 2 {
			entryType = strings.TrimSuffix(parts2[0], "s")
		}
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
	summary += "## Recent\n\n"
	recent := parts
	if len(recent) > 5 {
		recent = recent[len(recent)-5:]
	}
	for _, p := range recent {
		summary += fmt.Sprintf("- %s\n", p)
	}

	pb.Summary = summary
	contextFile := filepath.Join(pb.brainDir, "context.md")
	os.WriteFile(contextFile, []byte(summary), 0644)
	return summary
}

func (pb *ProjectBrain) Stats() map[string]interface{} {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	typeCount := map[string]int{}
	totalFiles := 0

	filepath.Walk(pb.brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" || filepath.Base(path) == "context.md" {
			return nil
		}
		totalFiles++
		entryType := "note"
		relPath, _ := filepath.Rel(pb.brainDir, path)
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) >= 2 {
			entryType = strings.TrimSuffix(parts[0], "s")
		}
		typeCount[entryType]++
		return nil
	})

	return map[string]interface{}{
		"project_id":     pb.ProjectID,
		"project_name":   pb.ProjectName,
		"project_path":   pb.ProjectPath,
		"total_files":    totalFiles,
		"files_by_type":  typeCount,
		"has_summary":    pb.Summary != "",
		"summary":        pb.Summary,
		"last_updated":   pb.UpdatedAt.Format(time.RFC3339),
		"ai_enabled":     pb.AIEnabled,
		"tools_enabled":  pb.ToolsEnabled,
		"brain_dir":      pb.brainDir,
	}
}

func (pb *ProjectBrain) SetConfig(aiEnabled, toolsEnabled []string) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.AIEnabled = aiEnabled
	pb.ToolsEnabled = toolsEnabled
}

func (pb *ProjectBrain) Forget() error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	entries, _ := os.ReadDir(pb.brainDir)
	for _, e := range entries {
		if e.Name() == ".obsidian" {
			continue
		}
		os.RemoveAll(filepath.Join(pb.brainDir, e.Name()))
	}
	os.MkdirAll(filepath.Join(pb.brainDir, "sessions"), 0755)
	os.MkdirAll(filepath.Join(pb.brainDir, "decisions"), 0755)
	os.MkdirAll(filepath.Join(pb.brainDir, "errors"), 0755)
	os.MkdirAll(filepath.Join(pb.brainDir, "notes"), 0755)
	pb.Summary = ""
	pb.UpdatedAt = time.Now()
	return nil
}

func (pb *ProjectBrain) OpenInObsidian() error {
	if !ObsidianInstalled() {
		return fmt.Errorf("obsidian is not installed")
	}

	vaultPath := pb.brainDir
	if runtime.GOOS == "windows" {
		vaultPath = strings.ReplaceAll(vaultPath, "\\", "/")
	}
	vaultURL := fmt.Sprintf("obsidian://open?path=%s", vaultPath)
	cmd := exec.Command("xdg-open", vaultURL)
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", vaultURL)
	} else if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "start", vaultURL)
	}
	return cmd.Start()
}

func (pb *ProjectBrain) OpenBrainDir() error {
	cmd := exec.Command("xdg-open", pb.brainDir)
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", pb.brainDir)
	} else if runtime.GOOS == "windows" {
		cmd = exec.Command("explorer", pb.brainDir)
	}
	return cmd.Start()
}

func (pb *ProjectBrain) GetBrainDir() string {
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

func AutoSaveSession(pb *ProjectBrain, tag string) error {
	content := fmt.Sprintf("Session %s at %s\n\nProject: %s\nPath: %s",
		tag, time.Now().Format(time.RFC3339), pb.ProjectName, pb.ProjectPath)
	return pb.SaveEntry("session", content, []string{"dwyt", "session", tag})
}

func AutoSaveDecision(pb *ProjectBrain, decision string) error {
	return pb.SaveEntry("decision", decision, []string{"dwyt", "decision"})
}

func AutoSaveError(pb *ProjectBrain, errStr, solution string) error {
	content := fmt.Sprintf("Error: %s\n\nSolution: %s", errStr, solution)
	return pb.SaveEntry("error", content, []string{"dwyt", "error"})
}

func AutoSaveCommand(pb *ProjectBrain, command string) error {
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
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

func extractContent(content string) string {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	fmCount := 0
	var body []string
	for _, line := range lines {
		if line == "---" {
			fmCount++
			if fmCount == 1 {
				inFrontmatter = true
				continue
			} else if inFrontmatter {
				inFrontmatter = false
				continue
			}
		}
		if !inFrontmatter {
			body = append(body, line)
		}
	}
	result := strings.TrimSpace(strings.Join(body, "\n"))
	if len(result) > 300 {
		result = result[:297] + "..."
	}
	return result
}

func hashPath(path string) string {
	abs, _ := filepath.Abs(path)
	abs = filepath.Clean(abs)
	h := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(h[:])[:12]
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
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	CreatedAt    time.Time `json:"created_at"`
	LastOpen     time.Time `json:"last_open"`
	ToolsEnabled []string  `json:"tools_enabled"`
	AIEnabled    []string  `json:"ai_enabled"`
	BrainFiles   int       `json:"brain_files"`
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
