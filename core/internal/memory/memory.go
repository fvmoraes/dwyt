package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryEntry represents a single memory item for a project.
type MemoryEntry struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MemorySnapshot stores a versioned snapshot of project memory.
type MemorySnapshot struct {
	ID        string        `json:"id"`
	Tag       string        `json:"tag"`
	Summary   string        `json:"summary"`
	Entries   []MemoryEntry `json:"entries"`
	CreatedAt time.Time     `json:"created_at"`
	ProjectID string        `json:"project_id"`
}

// ProjectMemory holds all memory data for a single project.
type ProjectMemory struct {
	ProjectID    string         `json:"project_id"`
	ProjectName  string         `json:"project_name"`
	ProjectPath  string         `json:"project_path"`
	Summary      string         `json:"summary"`
	Entries      []MemoryEntry  `json:"entries"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	AIEnabled    []string       `json:"ai_enabled"`
	ToolsEnabled []string          `json:"tools_enabled"`
	Snapshots    []MemorySnapshot `json:"snapshots,omitempty"`
	mu           sync.RWMutex     `json:"-"`
	baseDir      string         `json:"-"`
}

// NewProjectMemory creates or loads the memory for a project.
func NewProjectMemory(dwytHome, projectPath string) (*ProjectMemory, error) {
	id := hashPath(projectPath)
	baseDir := filepath.Join(dwytHome, "projects", id, "memory")
	os.MkdirAll(baseDir, 0755)

	// Also ensure project-level metadata file
	EnsureProjectJSON(dwytHome, projectPath)

	pm := &ProjectMemory{
		ProjectID:   id,
		ProjectName: filepath.Base(projectPath),
		ProjectPath: projectPath,
		baseDir:     baseDir,
	}

	memFile := filepath.Join(baseDir, "memory.json")
	loaded := false
	if data, err := os.ReadFile(memFile); err == nil {
		if err := json.Unmarshal(data, pm); err != nil {
			return pm, fmt.Errorf("memory load: %w", err)
		}
		loaded = true
	}

	if pm.Entries == nil {
		pm.Entries = make([]MemoryEntry, 0)
	}
	if !loaded {
		pm.CreatedAt = time.Now()
	}
	pm.UpdatedAt = time.Now()
	if len(pm.Entries) > 0 {
		_, _ = pm.SaveSnapshot("session-start")
	}
	return pm, nil
}

// Save persists the project memory to disk.
func (pm *ProjectMemory) Save() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(pm, "", "  ")
	if err != nil {
		return fmt.Errorf("memory save: %w", err)
	}

	memFile := filepath.Join(pm.baseDir, "memory.json")
	os.MkdirAll(filepath.Dir(memFile), 0755)
	return os.WriteFile(memFile, data, 0644)
}

// AddEntry adds a new memory entry and saves.
func (pm *ProjectMemory) AddEntry(entryType, content string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	entry := MemoryEntry{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:      entryType,
		Content:   content,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	pm.Entries = append(pm.Entries, entry)
	pm.UpdatedAt = time.Now()

	memFile := filepath.Join(pm.baseDir, "memory.json")
	data, err := json.MarshalIndent(pm, "", "  ")
	if err != nil {
		return fmt.Errorf("memory save: %w", err)
	}
	os.MkdirAll(filepath.Dir(memFile), 0755)
	return os.WriteFile(memFile, data, 0644)
}

// Search queries entries for the given text.
func (pm *ProjectMemory) Search(query string) []MemoryEntry {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	query = strings.ToLower(query)
	var results []MemoryEntry
	for _, e := range pm.Entries {
		if strings.Contains(strings.ToLower(e.Content), query) ||
			strings.Contains(strings.ToLower(e.Summary), query) ||
			strings.Contains(strings.ToLower(e.Type), query) {
			results = append(results, e)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})
	if len(results) > 20 {
		results = results[:20]
	}
	return results
}

// RebuildSummary rebuilds the summary based on stored entries.
func (pm *ProjectMemory) RebuildSummary() string {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if len(pm.Entries) == 0 {
		pm.Summary = ""
		return ""
	}

	var parts []string
	typeCount := map[string]int{}

	for _, e := range pm.Entries {
		typeCount[e.Type]++
		if e.Summary != "" {
			parts = append(parts, e.Summary)
		} else if len(e.Content) > 200 {
			parts = append(parts, e.Content[:200]+"...")
		} else {
			parts = append(parts, e.Content)
		}
	}

	summary := fmt.Sprintf("Project %s: %d memories (%s). ",
		pm.ProjectName,
		len(pm.Entries),
		formatTypeCount(typeCount),
	)

	if len(parts) > 0 {
		recent := parts
		if len(recent) > 3 {
			recent = recent[len(recent)-3:]
		}
		summary += "Recent: " + strings.Join(recent, " | ")
	}

	pm.Summary = summary
	return summary
}

// Stats returns memory statistics.
func (pm *ProjectMemory) Stats() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	typeCount := map[string]int{}
	for _, e := range pm.Entries {
		typeCount[e.Type]++
	}

	return map[string]interface{}{
		"project_id":     pm.ProjectID,
		"project_name":   pm.ProjectName,
		"project_path":   pm.ProjectPath,
		"total_entries":  len(pm.Entries),
		"entries_by_type": typeCount,
		"has_summary":    pm.Summary != "",
		"summary":        pm.Summary,
		"last_updated":   pm.UpdatedAt.Format(time.RFC3339),
		"ai_enabled":     pm.AIEnabled,
		"tools_enabled":  pm.ToolsEnabled,
	}
}

// SetConfig updates config fields (AI clients, tools).
func (pm *ProjectMemory) SetConfig(aiEnabled, toolsEnabled []string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.AIEnabled = aiEnabled
	pm.ToolsEnabled = toolsEnabled
}

// Forget removes all memory for the project.
func (pm *ProjectMemory) Forget() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	memFile := filepath.Join(pm.baseDir, "memory.json")
	if err := os.Remove(memFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("memory forget: %w", err)
	}

	snapDir := filepath.Join(pm.baseDir, "snapshots")
	os.RemoveAll(snapDir)

	pm.Entries = nil
	pm.Summary = ""
	pm.Snapshots = nil
	pm.UpdatedAt = time.Now()
	return nil
}

// SearchByType returns entries filtered by type.
func (pm *ProjectMemory) SearchByType(entryType string) []MemoryEntry {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var results []MemoryEntry
	for _, e := range pm.Entries {
		if e.Type == entryType {
			results = append(results, e)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})
	return results
}

// GetRecentEntries returns the N most recent entries.
func (pm *ProjectMemory) GetRecentEntries(n int) []MemoryEntry {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if n > len(pm.Entries) {
		n = len(pm.Entries)
	}
	sorted := make([]MemoryEntry, len(pm.Entries))
	copy(sorted, pm.Entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].UpdatedAt.After(sorted[j].UpdatedAt)
	})
	return sorted[:n]
}

// ── snapshots ─────────────────────────────────────────────────────────────

func snapDir(pm *ProjectMemory) string {
	return filepath.Join(pm.baseDir, "snapshots")
}

func generateSnapshotID() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("snap_%s_fallback", time.Now().Format("20060102_150405"))
	}
	return fmt.Sprintf("snap_%s_%s", time.Now().Format("20060102_150405"), hex.EncodeToString(b)[:3])
}

// SaveSnapshot saves the current memory state as a named snapshot file.
func (pm *ProjectMemory) SaveSnapshot(tag string) (*MemorySnapshot, error) {
	pm.mu.RLock()
	snap := MemorySnapshot{
		ID:        generateSnapshotID(),
		Tag:       tag,
		Summary:   pm.Summary,
		Entries:   make([]MemoryEntry, len(pm.Entries)),
		CreatedAt: time.Now(),
		ProjectID: pm.ProjectID,
	}
	copy(snap.Entries, pm.Entries)
	pm.mu.RUnlock()

	dir := snapDir(pm)
	os.MkdirAll(dir, 0755)

	snapFile := filepath.Join(dir, snap.ID+".json")
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("snapshot save: %w", err)
	}
	if err := os.WriteFile(snapFile, data, 0644); err != nil {
		return nil, fmt.Errorf("snapshot save: %w", err)
	}
	return &snap, nil
}

// LoadSnapshot restores memory state from a snapshot file.
func (pm *ProjectMemory) LoadSnapshot(snapshotID string) error {
	snapFile := filepath.Join(snapDir(pm), snapshotID+".json")
	data, err := os.ReadFile(snapFile)
	if err != nil {
		return fmt.Errorf("snapshot load: %w", err)
	}

	var snap MemorySnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("snapshot load: %w", err)
	}

	pm.mu.Lock()
	pm.Entries = snap.Entries
	pm.Summary = snap.Summary
	pm.UpdatedAt = time.Now()
	pm.mu.Unlock()

	return pm.Save()
}

// ListSnapshots returns all snapshots sorted by creation time descending.
func (pm *ProjectMemory) ListSnapshots() []MemorySnapshot {
	dir := snapDir(pm)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var snaps []MemorySnapshot
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var snap MemorySnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}
		snaps = append(snaps, snap)
	}

	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].CreatedAt.After(snaps[j].CreatedAt)
	})
	return snaps
}

// DeleteSnapshot deletes a snapshot file by ID.
func (pm *ProjectMemory) DeleteSnapshot(snapshotID string) error {
	snapFile := filepath.Join(snapDir(pm), snapshotID+".json")
	if err := os.Remove(snapFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("snapshot delete: %w", err)
	}
	return nil
}

// AutoSnapshot saves a snapshot and prunes old hourly/daily snapshots.
func (pm *ProjectMemory) AutoSnapshot(tag string) {
	pm.SaveSnapshot(tag)

	snaps := pm.ListSnapshots()

	var hourly, daily []MemorySnapshot
	for _, s := range snaps {
		if strings.HasPrefix(s.Tag, "auto-1h") {
			hourly = append(hourly, s)
		} else if strings.HasPrefix(s.Tag, "auto-24h") {
			daily = append(daily, s)
		}
	}

	for len(hourly) > 24 {
		oldest := hourly[len(hourly)-1]
		pm.DeleteSnapshot(oldest.ID)
		hourly = hourly[:len(hourly)-1]
	}
	for len(daily) > 7 {
		oldest := daily[len(daily)-1]
		pm.DeleteSnapshot(oldest.ID)
		daily = daily[:len(daily)-1]
	}
}

// AutoSaveSessionEnd saves a session-end snapshot.
func (pm *ProjectMemory) AutoSaveSessionEnd() {
	pm.SaveSnapshot("session-end")
}

// ── helpers ───────────────────────────────────────────────────────────────

func formatTypeCount(tc map[string]int) string {
	parts := make([]string, 0, len(tc))
	for t, c := range tc {
		parts = append(parts, fmt.Sprintf("%d %s", c, t))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func hashPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	h := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(h[:])[:12]
}

// EnsureDirs creates the per-project directory structure.
func EnsureDirs(dwytHome, projectPath string) error {
	id := hashPath(projectPath)
	dirs := []string{
		filepath.Join(dwytHome, "projects", id, "memory"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	// Also create the project.json metadata file
	return EnsureProjectJSON(dwytHome, projectPath)
}

// ProjectMeta holds the per-project metadata structure.
type ProjectMeta struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	CreatedAt    time.Time `json:"created_at"`
	LastOpen     time.Time `json:"last_open"`
	ToolsEnabled []string  `json:"tools_enabled"`
	AIEnabled    []string  `json:"ai_enabled"`
	IndexStatus  string    `json:"index_status"`
	MemoryCount  int       `json:"memory_count"`
}

// EnsureProjectJSON creates/updates the project.json metadata file.
func EnsureProjectJSON(dwytHome, projectPath string) error {
	id := hashPath(projectPath)
	projDir := filepath.Join(dwytHome, "projects", id)
	os.MkdirAll(projDir, 0755)

	projFile := filepath.Join(projDir, "project.json")

	// Load existing if present
	meta := ProjectMeta{
		Name:      filepath.Base(projectPath),
		Path:      projectPath,
		CreatedAt: time.Now(),
		LastOpen:  time.Now(),
	}

	if data, err := os.ReadFile(projFile); err == nil {
		json.Unmarshal(data, &meta)
		meta.LastOpen = time.Now()
		meta.Path = projectPath // Ensure path is updated
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("project.json save: %w", err)
	}
	return os.WriteFile(projFile, data, 0644)
}

// UpdateProjectMeta updates fields in project.json.
func UpdateProjectMeta(dwytHome, projectPath string, fn func(*ProjectMeta)) error {
	id := hashPath(projectPath)
	projFile := filepath.Join(dwytHome, "projects", id, "project.json")

	var meta ProjectMeta
	if data, err := os.ReadFile(projFile); err == nil {
		json.Unmarshal(data, &meta)
	} else {
		meta = ProjectMeta{
			Name:      filepath.Base(projectPath),
			Path:      projectPath,
			CreatedAt: time.Now(),
		}
	}
	meta.LastOpen = time.Now()
	fn(&meta)

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("project.json update: %w", err)
	}
	return os.WriteFile(projFile, data, 0644)
}

// AutoSaveCommand records a command executed via DWYT in the project memory.
func (pm *ProjectMemory) AutoSaveCommand(command string) error {
	if len(command) > 500 {
		command = command[:497] + "..."
	}
	return pm.AddEntry("command", fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), command))
}

// AutoSaveDecision records a decision made for the project.
func (pm *ProjectMemory) AutoSaveDecision(decision string) error {
	return pm.AddEntry("decision", decision)
}

// AutoSaveAction records a recent AI action.
func (pm *ProjectMemory) AutoSaveAction(action string) error {
	return pm.AddEntry("action", action)
}

// AutoSaveError records a recurring error and its solution.
func (pm *ProjectMemory) AutoSaveError(err, solution string) error {
	return pm.AddEntry("error", fmt.Sprintf("Error: %s\nSolution: %s", err, solution))
}
