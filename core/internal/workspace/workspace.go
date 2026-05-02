package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type ProjectState struct {
	Path        string    `json:"path"`
	IndexedAt   time.Time `json:"indexed_at,omitempty"`
	Nodes       int       `json:"nodes,omitempty"`
	Edges       int       `json:"edges,omitempty"`
	LastOpen    time.Time `json:"last_open"`
	RTKCommands int64     `json:"rtk_commands,omitempty"`
	RTKSaved    int64     `json:"rtk_saved,omitempty"`
}

func ProjectDir(repoPath string) string {
	return filepath.Join(repoPath, ".dwyt")
}

func Read(repoPath string) (*ProjectState, error) {
	ps := &ProjectState{
		Path:     repoPath,
		LastOpen: time.Now(),
	}
	file := filepath.Join(ProjectDir(repoPath), "project.json")
	data, err := os.ReadFile(file)
	if err != nil {
		return ps, nil
	}
	json.Unmarshal(data, ps)
	return ps, nil
}

func Save(ps *ProjectState) error {
	dir := ProjectDir(ps.Path)
	os.MkdirAll(dir, 0755)
	data, _ := json.MarshalIndent(ps, "", "  ")
	return os.WriteFile(filepath.Join(dir, "project.json"), data, 0644)
}

func Touch(repoPath string) {
	os.MkdirAll(ProjectDir(repoPath), 0755)
	ps, _ := Read(repoPath)
	ps.LastOpen = time.Now()
	Save(ps)
}
