package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/toolsource"
)

func TestRuntimeStateInvalidateProcessObservation(t *testing.T) {
	home := t.TempDir()
	state := Init(home)
	state.SetProcessLifecycle(ProcessInfo{
		Name:          "codebase",
		PID:           4242,
		Port:          9750,
		RequestedPort: 9749,
		EffectivePort: 9750,
		StartedAt:     time.Unix(123, 0).UTC(),
		Healthy:       true,
		State:         "healthy",
		DesiredState:  "stopped",
		Ownership:     "managed",
		Identity:      "stale-identity",
		LastError:     "stale error",
		Uptime:        99,
	})
	state.SetToolError("codebase", "stale error")

	state.InvalidateProcessObservation("codebase")

	got, ok := state.GetProcess("codebase")
	if !ok {
		t.Fatal("process disappeared during observation invalidation")
	}
	want := ProcessInfo{
		Name:          "codebase",
		RequestedPort: 9749,
		State:         "unknown",
		DesiredState:  "stopped",
		Ownership:     "unknown",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("invalidated process = %+v, want %+v", got, want)
	}
	if _, ok := state.ToolErrors["codebase"]; ok {
		t.Fatal("stale process error survived observation invalidation")
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
		t.Fatal("observation invalidation did not persist identical main and backup state")
	}

	reloaded := Init(home)
	persisted, ok := reloaded.GetProcess("codebase")
	if !ok || !reflect.DeepEqual(persisted, want) {
		t.Fatalf("persisted invalidation = %+v (present=%v), want %+v", persisted, ok, want)
	}
}

func TestRuntimeStateInitRecoversBackupWhenMainIsMissing(t *testing.T) {
	home := t.TempDir()
	first := Init(home)
	first.SetProcessDesired("codebase", "stopped")

	mainPath := filepath.Join(home, "state.json")
	if err := os.Remove(mainPath); err != nil {
		t.Fatalf("remove main state: %v", err)
	}

	recovered := Init(home)
	process, ok := recovered.GetProcess("codebase")
	if !ok || process.DesiredState != "stopped" {
		t.Fatalf("backup state was not recovered: %+v (present=%v)", process, ok)
	}
	if recovered.Path != mainPath {
		t.Fatalf("recovered Path = %q, want %q", recovered.Path, mainPath)
	}
}

func TestRuntimeStateRegisterProcessPreservesLifecycleAndConfig(t *testing.T) {
	state := Init(t.TempDir())
	initial := ProcessInfo{
		Name:          "codebase",
		PID:           4242,
		Port:          9750,
		RequestedPort: 9749,
		EffectivePort: 9750,
		StartedAt:     time.Unix(123, 0).UTC(),
		Healthy:       false,
		State:         "degraded",
		DesiredState:  "running",
		Ownership:     "managed",
		Identity:      "identity-token",
		LastError:     "probe failed",
		Uptime:        41,
	}
	state.SetProcessLifecycle(initial)

	state.RegisterProcess("codebase", 4242, 9751)
	samePID, ok := state.GetProcess("codebase")
	if !ok {
		t.Fatal("registered process not found")
	}
	wantSamePID := initial
	wantSamePID.Port = 9751
	wantSamePID.EffectivePort = 9751
	if !reflect.DeepEqual(samePID, wantSamePID) {
		t.Fatalf("same-PID registration = %+v, want %+v", samePID, wantSamePID)
	}

	state.RegisterProcess("codebase", 5252, 9752)
	changedPID, ok := state.GetProcess("codebase")
	if !ok {
		t.Fatal("re-registered process not found")
	}
	if changedPID.StartedAt.IsZero() || changedPID.StartedAt.Equal(initial.StartedAt) {
		t.Fatalf("StartedAt was not reset after PID change: %v", changedPID.StartedAt)
	}
	wantChangedPID := wantSamePID
	wantChangedPID.PID = 5252
	wantChangedPID.Port = 9752
	wantChangedPID.EffectivePort = 9752
	wantChangedPID.StartedAt = changedPID.StartedAt
	if !reflect.DeepEqual(changedPID, wantChangedPID) {
		t.Fatalf("changed-PID registration = %+v, want %+v", changedPID, wantChangedPID)
	}

	state.RegisterProcess("headroom", 6262, 8787)
	fresh, ok := state.GetProcess("headroom")
	if !ok {
		t.Fatal("new process not found")
	}
	if fresh.RequestedPort != 8787 || fresh.EffectivePort != 8787 || fresh.Port != 8787 {
		t.Fatalf("new process ports = requested:%d effective:%d compatibility:%d", fresh.RequestedPort, fresh.EffectivePort, fresh.Port)
	}
}

func TestRuntimeStateSnapshotIncludesLifecycleAndReturnsCopies(t *testing.T) {
	state := Init(t.TempDir())
	startedAt := time.Unix(123, 0).UTC()
	state.SetProcessLifecycle(ProcessInfo{
		Name:          "codebase",
		PID:           4242,
		Port:          9750,
		RequestedPort: 9749,
		StartedAt:     startedAt,
		Healthy:       false,
		State:         "degraded",
		DesiredState:  "running",
		Ownership:     "managed",
		Identity:      "identity-token",
		LastError:     "probe failed",
		Uptime:        41,
	})
	state.SetClients([]string{"kiro"})
	state.SetToolSources(map[string]toolsource.Selection{
		"codebase": {Mode: "managed", Path: "/tmp/codebase"},
	})

	snapshot := state.Snapshot()
	process := onlySnapshotProcess(t, snapshot)
	wantFields := map[string]interface{}{
		"name":           "codebase",
		"pid":            4242,
		"port":           9750,
		"requested_port": 9749,
		"effective_port": 9750,
		"started_at":     startedAt.Format(time.RFC3339),
		"healthy":        false,
		"state":          "degraded",
		"desired_state":  "running",
		"ownership":      "managed",
		"identity":       "identity-token",
		"last_error":     "probe failed",
		"uptime_secs":    int64(41),
	}
	for field, want := range wantFields {
		if got := process[field]; !reflect.DeepEqual(got, want) {
			t.Errorf("snapshot process[%q] = %#v, want %#v", field, got, want)
		}
	}

	toolErrors, ok := snapshot["tool_errors"].(map[string]string)
	if !ok {
		t.Fatalf("tool_errors type = %T", snapshot["tool_errors"])
	}
	clients, ok := snapshot["clients"].([]string)
	if !ok || len(clients) != 1 {
		t.Fatalf("clients = %#v", snapshot["clients"])
	}
	toolSources, ok := snapshot["tool_sources"].(map[string]toolsource.Selection)
	if !ok {
		t.Fatalf("tool_sources type = %T", snapshot["tool_sources"])
	}

	process["state"] = "tampered"
	toolErrors["codebase"] = "tampered"
	clients[0] = "tampered"
	toolSources["codebase"] = toolsource.Selection{Mode: "tampered"}

	second := state.Snapshot()
	secondProcess := onlySnapshotProcess(t, second)
	if secondProcess["state"] != "degraded" {
		t.Fatalf("process snapshot mutation escaped: %#v", secondProcess)
	}
	if got := second["tool_errors"].(map[string]string)["codebase"]; got != "probe failed" {
		t.Fatalf("tool_errors snapshot mutation escaped: %q", got)
	}
	if got := second["clients"].([]string)[0]; got != "kiro" {
		t.Fatalf("clients snapshot mutation escaped: %q", got)
	}
	if got := second["tool_sources"].(map[string]toolsource.Selection)["codebase"]; got.Mode != "managed" || got.Path != "/tmp/codebase" {
		t.Fatalf("tool_sources snapshot mutation escaped: %+v", got)
	}
}

func onlySnapshotProcess(t *testing.T, snapshot map[string]interface{}) map[string]interface{} {
	t.Helper()
	processes, ok := snapshot["processes"].([]map[string]interface{})
	if !ok || len(processes) != 1 {
		t.Fatalf("processes = %#v", snapshot["processes"])
	}
	return processes[0]
}
