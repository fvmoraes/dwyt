package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/toolsource"
)

// ProcessInfo tracks one managed or safely adopted service. Port remains the
// compatibility alias for EffectivePort; new code keeps requested and effective
// values distinct so a fallback never silently changes future configuration.
type ProcessInfo struct {
	Name          string    `json:"name"`
	PID           int       `json:"pid"`
	Port          int       `json:"port,omitempty"`
	RequestedPort int       `json:"requested_port,omitempty"`
	EffectivePort int       `json:"effective_port,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	Healthy       bool      `json:"healthy"`
	// State is the reconciler's observed lifecycle state. DesiredState is the
	// operator/startup intent and Ownership determines whether DWYT may stop the
	// process (managed) or must leave a pre-existing instance alone (adopted).
	State            string    `json:"state,omitempty"`
	DesiredState     string    `json:"desired_state,omitempty"`
	Ownership        string    `json:"ownership,omitempty"`
	Identity         string    `json:"identity,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	Uptime           int64     `json:"uptime_secs,omitempty"`
	LastActivityAt   time.Time `json:"last_activity_at,omitempty,omitzero"`
	LastHealthAt     time.Time `json:"last_health_at,omitempty,omitzero"`
	LastHealthyAt    time.Time `json:"last_healthy_at,omitempty,omitzero"`
	LastTransitionAt time.Time `json:"last_transition_at,omitempty,omitzero"`
	Attempt          int       `json:"attempt,omitempty"`
}

// RuntimeState holds the live operational state of DWYT.
// It is persisted to state.json for crash recovery.
type RuntimeState struct {
	mu sync.RWMutex `json:"-"`

	Version            string                          `json:"version"`
	CurrentProject     string                          `json:"current_project"`
	CurrentProjectName string                          `json:"current_project_name"`
	Processes          map[string]ProcessInfo          `json:"processes"`
	ToolErrors         map[string]string               `json:"tool_errors"` // last error per tool
	Projects           map[string]ProjectEntry         `json:"projects"`
	Clients            []string                        `json:"clients"`
	ToolSources        map[string]toolsource.Selection `json:"tool_sources,omitempty"`
	Path               string                          `json:"-"` // state.json path
}

// ProjectEntry tracks per-project metadata in runtime state.
type ProjectEntry struct {
	Path          string    `json:"path"`
	Name          string    `json:"name"`
	LastOpen      time.Time `json:"last_open"`
	IndexedAt     time.Time `json:"indexed_at,omitempty"`
	Nodes         int       `json:"nodes,omitempty"`
	Edges         int       `json:"edges,omitempty"`
	ObsidianFiles int       `json:"obsidian_files,omitempty"`
}

var globalState *RuntimeState

// Init creates or loads the global runtime state.
func Init(dwytHome string) *RuntimeState {
	p := filepath.Join(dwytHome, "state.json")
	os.MkdirAll(filepath.Dir(p), 0755)

	s := &RuntimeState{
		Version:     "dev",
		Processes:   make(map[string]ProcessInfo),
		ToolErrors:  make(map[string]string),
		Projects:    make(map[string]ProjectEntry),
		ToolSources: make(map[string]toolsource.Selection),
		Path:        p,
	}

	mainData, mainErr := os.ReadFile(p)
	var loaded RuntimeState
	if mainErr == nil {
		mainErr = json.Unmarshal(mainData, &loaded)
	}
	if mainErr == nil {
		s = &loaded
	} else {
		backupData, backupErr := os.ReadFile(p + ".backup")
		var recovered RuntimeState
		if backupErr == nil {
			backupErr = json.Unmarshal(backupData, &recovered)
		}
		if backupErr == nil {
			s = &recovered
			log.Warn("state: recovered valid backup after main state was unavailable", log.Fields{"error": mainErr.Error()})
		} else if !os.IsNotExist(mainErr) {
			log.Warn("state: main and backup state are unavailable", log.Fields{"state_error": mainErr.Error(), "backup_error": backupErr.Error()})
		}
	}
	s.Path = p
	if s.Processes == nil {
		s.Processes = make(map[string]ProcessInfo)
	}
	if s.ToolErrors == nil {
		s.ToolErrors = make(map[string]string)
	}
	if s.Projects == nil {
		s.Projects = make(map[string]ProjectEntry)
	}
	if s.ToolSources == nil {
		s.ToolSources = make(map[string]toolsource.Selection)
	}
	if s.Version == "" {
		s.Version = "dev"
	}

	globalState = s
	return s
}

// Get returns the global runtime state.
func Get() *RuntimeState {
	return globalState
}

// SetVersion records the running DWYT release version.
func (s *RuntimeState) SetVersion(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version == "" {
		version = "dev"
	}
	s.Version = version
	s.maybeSave()
}

// SetToolError records (or clears, when msg is empty) the last error for a
// tool. Centralizes access so ToolErrors is never mutated without the lock.
func (s *RuntimeState) SetToolError(tool, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg == "" {
		delete(s.ToolErrors, tool)
	} else {
		s.ToolErrors[tool] = msg
	}
	s.maybeSave()
}

// ── Process tracking ──────────────────────────────────────────────────────

// RegisterProcess adds or updates a managed process in the state without
// discarding lifecycle or configuration fields already published for it.
func (s *RuntimeState) RegisterProcess(name string, pid, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	process, exists := s.Processes[name]
	pidChanged := !exists || process.PID != pid
	process.Name = name
	process.PID = pid
	process.Port = port
	process.EffectivePort = port
	if process.RequestedPort == 0 {
		process.RequestedPort = port
	}
	if pidChanged {
		process.StartedAt = time.Now()
	}
	if !exists {
		process.Healthy = true
	}
	s.Processes[name] = process
	s.maybeSave()
}

// SetProcessState records the reconciler's lifecycle state for a managed
// process (starting/healthy/degraded/failed/stopped). Unlike
// SetProcessHealthy it also applies when no process is registered (e.g. a
// service that failed to spawn). Lifecycle updates flow through
// SetProcessLifecycle so transition timestamps stay consistent.
func (s *RuntimeState) SetProcessState(name, state, errMsg string) {
	p, ok := s.GetProcess(name)
	if !ok {
		p = ProcessInfo{Name: name}
	}
	p.State = state
	if errMsg != "" {
		p.LastError = errMsg
	} else if state == "healthy" {
		p.LastError = ""
	}
	s.SetProcessLifecycle(p)
}

// SetProcessHealthy updates the health status of an already tracked process.
// It delegates to SetProcessLifecycle so every health observation gets the
// same timestamp and error-clearing behavior as reconciler publications.
func (s *RuntimeState) SetProcessHealthy(name string, healthy bool, errMsg string) {
	p, ok := s.GetProcess(name)
	if !ok {
		return
	}
	p.Healthy = healthy
	if healthy {
		p.LastError = ""
	} else {
		p.LastError = errMsg
	}
	p.LastHealthAt = time.Now()
	if healthy {
		p.LastHealthyAt = p.LastHealthAt
	}
	s.SetProcessLifecycle(p)
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

// UpdateProjectObsidian updates the brain files count for a project.
func (s *RuntimeState) UpdateProjectObsidian(path string, brainFiles int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pe, ok := s.Projects[path]; ok {
		pe.ObsidianFiles = brainFiles
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

// SetToolSources persists the ownership and resolved local path of each Hub
// tool. Copy input values so a request-owned map cannot race with runtime
// readers after setup has completed.
func (s *RuntimeState) SetToolSources(sources map[string]toolsource.Selection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ToolSources = make(map[string]toolsource.Selection, len(sources))
	for tool, source := range sources {
		s.ToolSources[tool] = source
	}
	s.maybeSave()
}

// ToolSourcesSnapshot returns a copy suitable for source resolution without
// exposing the mutable state map to callers.
func (s *RuntimeState) ToolSourcesSnapshot() map[string]toolsource.Selection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]toolsource.Selection, len(s.ToolSources))
	for tool, source := range s.ToolSources {
		result[tool] = source
	}
	return result
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
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// 0600: the state lists project paths and client names for the local user.
	// Publish the main document first. A failed main replacement must leave the
	// last known-good backup untouched; only a successful main generation is
	// mirrored to the recovery file.
	if err := atomicWriteFile(s.Path, data, 0600); err != nil {
		return err
	}
	return atomicWriteFile(s.Path+".backup", data, 0600)
}

func (s *RuntimeState) maybeSave() {
	if err := s.saveLocked(); err != nil {
		log.Error("failed to save state", log.Fields{"error": err.Error()})
	}
}

// ── Snapshot for API ──────────────────────────────────────────────────────

// Snapshot returns a read-safe copy of the state for API responses.
func (s *RuntimeState) Snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	processes := make([]map[string]interface{}, 0, len(s.Processes))
	for _, p := range s.Processes {
		effectivePort := p.EffectivePort
		if effectivePort == 0 {
			effectivePort = p.Port
		}
		processes = append(processes, map[string]interface{}{
			"name":           p.Name,
			"pid":            p.PID,
			"port":           effectivePort,
			"requested_port": p.RequestedPort,
			"effective_port": effectivePort,
			"started_at":     p.StartedAt.Format(time.RFC3339),
			"healthy":        p.Healthy,
			"state":          p.State,
			"desired_state":  p.DesiredState,
			"ownership":      p.Ownership,
			"identity":       p.Identity,
			"last_error":     p.LastError,
			"uptime_secs":    p.Uptime,
		})
	}

	toolErrors := make(map[string]string, len(s.ToolErrors))
	for tool, message := range s.ToolErrors {
		toolErrors[tool] = message
	}
	clients := append([]string(nil), s.Clients...)
	toolSources := make(map[string]toolsource.Selection, len(s.ToolSources))
	for tool, source := range s.ToolSources {
		toolSources[tool] = source
	}

	return map[string]interface{}{
		"version":              s.Version,
		"current_project":      s.CurrentProject,
		"current_project_name": s.CurrentProjectName,
		"processes":            processes,
		"tool_errors":          toolErrors,
		"clients":              clients,
		"tool_sources":         toolSources,
	}
}
