package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/log"
)

// ProcessInfo tracks a single managed process.
type ProcessInfo struct {
	Name      string    `json:"name"`
	PID       int       `json:"pid"`
	Port      int       `json:"port,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Healthy   bool      `json:"healthy"`
	LastError string    `json:"last_error,omitempty"`
	Uptime    int64     `json:"uptime_secs,omitempty"`
}

// RuntimeState holds the live operational state of DWYT.
// It is persisted to state.json for crash recovery.
type RuntimeState struct {
	mu sync.RWMutex `json:"-"`

	Version            string                  `json:"version"`
	CurrentProject     string                  `json:"current_project"`
	CurrentProjectName string                  `json:"current_project_name"`
	Processes          map[string]ProcessInfo  `json:"processes"`
	ToolErrors         map[string]string        `json:"tool_errors"` // last error per tool
	Projects           map[string]ProjectEntry  `json:"projects"`
	Clients            []string                 `json:"clients"`
	Path               string                  `json:"-"` // state.json path
}

// ProjectEntry tracks per-project metadata in runtime state.
type ProjectEntry struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	LastOpen   time.Time `json:"last_open"`
	IndexedAt  time.Time `json:"indexed_at,omitempty"`
	Nodes      int       `json:"nodes,omitempty"`
	Edges      int       `json:"edges,omitempty"`
	BrainFiles int       `json:"brain_files,omitempty"`
}

var globalState *RuntimeState

// Init creates or loads the global runtime state.
func Init(dwytHome string) *RuntimeState {
	p := filepath.Join(dwytHome, "state.json")
	os.MkdirAll(filepath.Dir(p), 0755)

	s := &RuntimeState{
		Version:    "3.1.0",
		Processes:  make(map[string]ProcessInfo),
		ToolErrors: make(map[string]string),
		Projects:   make(map[string]ProjectEntry),
		Path:       p,
	}

	if data, err := os.ReadFile(p); err == nil {
		json.Unmarshal(data, s)
	}
	if s.Processes == nil {
		s.Processes = make(map[string]ProcessInfo)
	}
	if s.ToolErrors == nil {
		s.ToolErrors = make(map[string]string)
	}
	if s.Projects == nil {
		s.Projects = make(map[string]ProjectEntry)
	}

	globalState = s
	return s
}

// Get returns the global runtime state.
func Get() *RuntimeState {
	return globalState
}

// ── Process tracking ──────────────────────────────────────────────────────

// RegisterProcess adds or updates a managed process in the state.
func (s *RuntimeState) RegisterProcess(name string, pid, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Processes[name] = ProcessInfo{
		Name:      name,
		PID:       pid,
		Port:      port,
		StartedAt: time.Now(),
		Healthy:   true,
	}
	s.maybeSave()
}

// SetProcessHealthy updates the health status of a process.
func (s *RuntimeState) SetProcessHealthy(name string, healthy bool, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.Processes[name]; ok {
		p.Healthy = healthy
		if !healthy {
			p.LastError = errMsg
			s.ToolErrors[name] = errMsg
		} else {
			p.LastError = ""
			delete(s.ToolErrors, name)
		}
		s.Processes[name] = p
	}
	s.maybeSave()
}

// RemoveProcess removes a process from tracking.
func (s *RuntimeState) RemoveProcess(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Processes, name)
	s.maybeSave()
}

// GetProcess returns info for a specific process.
func (s *RuntimeState) GetProcess(name string) (ProcessInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.Processes[name]
	return p, ok
}

// AllProcesses returns a copy of all tracked processes.
func (s *RuntimeState) AllProcesses() map[string]ProcessInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]ProcessInfo, len(s.Processes))
	for k, v := range s.Processes {
		out[k] = v
	}
	return out
}

// ── Project tracking ──────────────────────────────────────────────────────

// SetCurrentProject updates the active project.
func (s *RuntimeState) SetCurrentProject(path, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentProject = path
	s.CurrentProjectName = name
	if _, exists := s.Projects[path]; !exists {
		s.Projects[path] = ProjectEntry{
			Path:     path,
			Name:     name,
			LastOpen: time.Now(),
		}
	} else {
		pe := s.Projects[path]
		pe.LastOpen = time.Now()
		s.Projects[path] = pe
	}
	s.maybeSave()
}

// UpdateProjectBrain updates the brain files count for a project.
func (s *RuntimeState) UpdateProjectBrain(path string, brainFiles int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pe, ok := s.Projects[path]; ok {
		pe.BrainFiles = brainFiles
		s.Projects[path] = pe
		s.maybeSave()
	}
}

// ── Client tracking ───────────────────────────────────────────────────────

// SetClients updates the enabled AI clients list.
func (s *RuntimeState) SetClients(clients []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Clients = clients
	s.maybeSave()
}

// ── Persistence ───────────────────────────────────────────────────────────

// Save persists the state to disk.
func (s *RuntimeState) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}

func (s *RuntimeState) saveLocked() error {
	if s.Path == "" {
		return nil
	}
	os.MkdirAll(filepath.Dir(s.Path), 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, data, 0644)
}

func (s *RuntimeState) maybeSave() {
	if err := s.saveLocked(); err != nil {
		log.Error("failed to save state", log.Fields{"error": err.Error()})
		// Try to save backup
		if s.Path != "" {
			backupPath := s.Path + ".backup"
			if data, marshalErr := json.MarshalIndent(s, "", "  "); marshalErr == nil {
				os.WriteFile(backupPath, data, 0644)
			}
		}
	}
}

// ── Snapshot for API ──────────────────────────────────────────────────────

// Snapshot returns a read-safe copy of the state for API responses.
func (s *RuntimeState) Snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	processes := make([]map[string]interface{}, 0, len(s.Processes))
	for _, p := range s.Processes {
		processes = append(processes, map[string]interface{}{
			"name":       p.Name,
			"pid":        p.PID,
			"port":       p.Port,
			"started_at": p.StartedAt.Format(time.RFC3339),
			"healthy":    p.Healthy,
			"last_error": p.LastError,
			"uptime_secs": p.Uptime,
		})
	}

	return map[string]interface{}{
		"version":              s.Version,
		"current_project":      s.CurrentProject,
		"current_project_name": s.CurrentProjectName,
		"processes":            processes,
		"tool_errors":          s.ToolErrors,
		"clients":              s.Clients,
	}
}
