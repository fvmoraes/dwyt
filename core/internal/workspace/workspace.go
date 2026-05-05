package workspace

import (
	"crypto/sha256"
	"encoding/hex"
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

// dwytHome returns ~/.dwyt (or $DWYT_HOME if set).
func dwytHome() string {
	if h := os.Getenv("DWYT_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dwyt")
}

// hashPath returns a 12-char hex ID for the given path — same algorithm as db.HashPath.
func hashPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	h := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(h[:])[:12]
}

// ProjectDir returns ~/.dwyt/projects/<id> — never inside the project itself.
func ProjectDir(repoPath string) string {
	return filepath.Join(dwytHome(), "projects", hashPath(repoPath))
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
	ps, _ := Read(repoPath)
	ps.LastOpen = time.Now()
	Save(ps)
}
