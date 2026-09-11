package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRuntimeState_Init(t *testing.T) {
	tmpDir := t.TempDir()
	state := Init(tmpDir)

	if state == nil {
		t.Fatal("Init returned nil")
	}
	if state.Version == "" {
		t.Error("Version not set")
	}
	if state.Processes == nil {
		t.Error("Processes map not initialized")
	}
	if state.ToolErrors == nil {
		t.Error("ToolErrors map not initialized")
	}
	if state.Projects == nil {
		t.Error("Projects map not initialized")
	}
}

func TestRuntimeState_RegisterProcess(t *testing.T) {
	tmpDir := t.TempDir()
	state := Init(tmpDir)

	state.RegisterProcess("test", 12345, 8080)

	proc, ok := state.GetProcess("test")
	if !ok {
		t.Fatal("Process not registered")
	}
	if proc.PID != 12345 {
		t.Errorf("Expected PID 12345, got %d", proc.PID)
	}
	if proc.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", proc.Port)
	}
	if !proc.Healthy {
		t.Error("Expected healthy process")
	}
}

func TestRuntimeState_ConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	state := Init(tmpDir)

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			state.RegisterProcess(fmt.Sprintf("proc-%d", n), n, 8000+n)
		}(i)
	}
	wg.Wait()

	// Verify all processes were registered
	procs := state.AllProcesses()
	if len(procs) != numGoroutines {
		t.Errorf("Expected %d processes, got %d", numGoroutines, len(procs))
	}
}

func TestRuntimeState_SetProcessHealthy(t *testing.T) {
	tmpDir := t.TempDir()
	state := Init(tmpDir)

	state.RegisterProcess("test", 12345, 8080)
	state.SetProcessHealthy("test", false, "connection refused")

	proc, ok := state.GetProcess("test")
	if !ok {
		t.Fatal("Process not found")
	}
	if proc.Healthy {
		t.Error("Expected unhealthy process")
	}
	if proc.LastError != "connection refused" {
		t.Errorf("Expected error 'connection refused', got '%s'", proc.LastError)
	}

	// Check tool errors
	if state.ToolErrors["test"] != "connection refused" {
		t.Error("Tool error not set")
	}
}

func TestRuntimeState_RemoveProcess(t *testing.T) {
	tmpDir := t.TempDir()
	state := Init(tmpDir)

	state.RegisterProcess("test", 12345, 8080)
	state.RemoveProcess("test")

	_, ok := state.GetProcess("test")
	if ok {
		t.Error("Process should be removed")
	}
}

func TestRuntimeState_SetCurrentProject(t *testing.T) {
	tmpDir := t.TempDir()
	state := Init(tmpDir)

	state.SetCurrentProject("/tmp/project", "project")

	if state.CurrentProject != "/tmp/project" {
		t.Errorf("Expected '/tmp/project', got '%s'", state.CurrentProject)
	}
	if state.CurrentProjectName != "project" {
		t.Errorf("Expected 'project', got '%s'", state.CurrentProjectName)
	}

	// Check project was added to projects map
	proj, ok := state.Projects["/tmp/project"]
	if !ok {
		t.Fatal("Project not added to projects map")
	}
	if proj.Name != "project" {
		t.Errorf("Expected name 'project', got '%s'", proj.Name)
	}
}

func TestRuntimeState_UpdateProjectObsidian(t *testing.T) {
	tmpDir := t.TempDir()
	state := Init(tmpDir)

	state.SetCurrentProject("/tmp/project", "project")
	state.UpdateProjectObsidian("/tmp/project", 42)

	proj, ok := state.Projects["/tmp/project"]
	if !ok {
		t.Fatal("Project not found")
	}
	if proj.ObsidianFiles != 42 {
		t.Errorf("Expected 42 brain files, got %d", proj.ObsidianFiles)
	}
}

func TestRuntimeState_SetClients(t *testing.T) {
	tmpDir := t.TempDir()
	state := Init(tmpDir)

	clients := []string{"claude", "cursor", "kiro"}
	state.SetClients(clients)

	if len(state.Clients) != 3 {
		t.Errorf("Expected 3 clients, got %d", len(state.Clients))
	}
	if state.Clients[0] != "claude" {
		t.Errorf("Expected 'claude', got '%s'", state.Clients[0])
	}
}

func TestRuntimeState_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create and populate state
	state1 := Init(tmpDir)
	state1.RegisterProcess("test", 12345, 8080)
	state1.SetCurrentProject("/tmp/project", "project")
	if err := state1.Save(); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Fatal("state.json not created")
	}

	// Load state in new instance
	state2 := Init(tmpDir)

	// Verify data was persisted
	proc, ok := state2.GetProcess("test")
	if !ok {
		t.Fatal("Process not persisted")
	}
	if proc.PID != 12345 {
		t.Errorf("Expected PID 12345, got %d", proc.PID)
	}
	if state2.CurrentProject != "/tmp/project" {
		t.Errorf("Expected '/tmp/project', got '%s'", state2.CurrentProject)
	}
}

func TestRuntimeState_SaveWritesMatchingMainAndBackup(t *testing.T) {
	home := t.TempDir()
	state := Init(home)
	state.RegisterProcess("test", 12345, 8080)
	if err := state.Save(); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	mainData, err := os.ReadFile(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	backupData, err := os.ReadFile(filepath.Join(home, "state.json.backup"))
	if err != nil {
		t.Fatalf("read state.json.backup: %v", err)
	}
	if string(mainData) != string(backupData) {
		t.Fatalf("main and backup differ:\nmain: %s\nbackup: %s", mainData, backupData)
	}
}

func TestRuntimeState_Snapshot(t *testing.T) {
	tmpDir := t.TempDir()
	state := Init(tmpDir)

	state.RegisterProcess("test", 12345, 8080)
	state.SetCurrentProject("/tmp/project", "project")

	snapshot := state.Snapshot()

	if snapshot["version"] != state.Version {
		t.Error("Version not in snapshot")
	}
	if snapshot["current_project"] != "/tmp/project" {
		t.Error("Current project not in snapshot")
	}

	processes, ok := snapshot["processes"].([]map[string]interface{})
	if !ok || len(processes) == 0 {
		t.Error("Processes not in snapshot")
	}
}

func TestRuntimeState_ProjectLastOpen(t *testing.T) {
	tmpDir := t.TempDir()
	state := Init(tmpDir)

	state.SetCurrentProject("/tmp/project", "project")
	project := state.Projects["/tmp/project"]
	baseline := time.Unix(1, 0)
	project.LastOpen = baseline
	state.Projects["/tmp/project"] = project

	state.SetCurrentProject("/tmp/project", "project")
	updated := state.Projects["/tmp/project"].LastOpen
	if !updated.After(baseline) {
		t.Fatalf("LastOpen = %v, want after %v", updated, baseline)
	}
}

func TestRuntimeStatePersistsLifecycleContract(t *testing.T) {
	home := t.TempDir()
	first := Init(home)
	first.SetProcessLifecycle(ProcessInfo{
		Name: "codebase", PID: 4242, Port: 9750,
		RequestedPort: 9749, EffectivePort: 9750,
		Healthy: true, State: "healthy", DesiredState: "running",
		Ownership: "managed", Identity: "identity-token",
	})

	reloaded := Init(home)
	process, ok := reloaded.GetProcess("codebase")
	if !ok {
		t.Fatal("lifecycle process was not persisted")
	}
	if process.RequestedPort != 9749 || process.EffectivePort != 9750 || process.Port != 9750 ||
		process.DesiredState != "running" || process.Ownership != "managed" || process.Identity != "identity-token" {
		t.Fatalf("persisted lifecycle contract = %+v", process)
	}
}

func TestRuntimeStateInitFallsBackToBackupAfterTruncatedState(t *testing.T) {
	home := t.TempDir()
	statePath := filepath.Join(home, "state.json")
	backupPath := statePath + ".backup"
	if err := os.WriteFile(statePath, []byte(`{"processes":`), 0600); err != nil {
		t.Fatal(err)
	}
	backup := `{"version":"backup","processes":{"codebase":{"name":"codebase","desired_state":"stopped"}},"tool_errors":{},"projects":{}}`
	if err := os.WriteFile(backupPath, []byte(backup), 0600); err != nil {
		t.Fatal(err)
	}

	loaded := Init(home)
	if loaded.Version != "backup" {
		t.Fatalf("version = %q, want backup", loaded.Version)
	}
	process, ok := loaded.GetProcess("codebase")
	if !ok || process.DesiredState != "stopped" {
		t.Fatalf("backup lifecycle state was not recovered: %+v (present=%v)", process, ok)
	}
}

func TestAtomicWriteFileReplacesWholeDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte(`{"complete":true}`), 0600); err != nil {
		t.Fatalf("atomicWriteFile error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != `{"complete":true}` {
		t.Fatalf("contents = %q", contents)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".state.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary state files leaked: %v", matches)
	}
}
