package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type ToolState struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	UIPort    int    `json:"ui_port,omitempty"`
	VenvDir   string `json:"venv,omitempty"`
	ProxyPort int    `json:"port,omitempty"`
	Dir       string `json:"dir,omitempty"`
}

type ProjectEntry struct {
	Path      string    `json:"path"`
	IndexedAt time.Time `json:"indexed_at,omitempty"`
	Nodes     int       `json:"nodes,omitempty"`
	Edges     int       `json:"edges,omitempty"`
}

type Metrics struct {
	RTKTokensSaved      int64 `json:"rtk_tokens_saved"`
	HeadroomTokensSaved int64 `json:"headroom_tokens_saved"`
}

type State struct {
	Version            string                  `json:"version"`
	InstalledAt        time.Time               `json:"installed_at"`
	Tools              map[string]ToolState    `json:"tools"`
	Clients            []string                `json:"clients"`
	IntegratedProjects map[string]ProjectEntry `json:"integrated_projects"`
	Metrics            Metrics                 `json:"metrics"`
}

var statePath string

func SetPath(dwytHome string) { statePath = filepath.Join(dwytHome, "state.json") }

func Load() (*State, error) {
	s := &State{
		Version:            "3.0.0",
		InstalledAt:        time.Now(),
		Tools:              make(map[string]ToolState),
		Clients:            []string{},
		IntegratedProjects: make(map[string]ProjectEntry),
	}
	if statePath == "" {
		return s, nil
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		return s, nil
	}
	json.Unmarshal(data, s)
	if s.Tools == nil {
		s.Tools = make(map[string]ToolState)
	}
	if s.IntegratedProjects == nil {
		s.IntegratedProjects = make(map[string]ProjectEntry)
	}
	if s.Clients == nil {
		s.Clients = []string{}
	}
	return s, nil
}

func Save(s *State) error {
	if statePath == "" {
		return nil
	}
	os.MkdirAll(filepath.Dir(statePath), 0755)
	data, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(statePath, data, 0644)
}

func (s *State) SetTool(name string, ts ToolState) {
	if s.Tools == nil {
		s.Tools = make(map[string]ToolState)
	}
	s.Tools[name] = ts
}

func (s *State) AddProject(path string, nodes, edges int) {
	if s.IntegratedProjects == nil {
		s.IntegratedProjects = make(map[string]ProjectEntry)
	}
	s.IntegratedProjects[path] = ProjectEntry{
		Path:      path,
		IndexedAt: time.Now(),
		Nodes:     nodes,
		Edges:     edges,
	}
}

func (s *State) HasClient(name string) bool {
	for _, c := range s.Clients {
		if c == name {
			return true
		}
	}
	return false
}

func (s *State) AddClient(name string) {
	if !s.HasClient(name) {
		s.Clients = append(s.Clients, name)
	}
}
