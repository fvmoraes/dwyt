package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func persistedProcessFields(t *testing.T, home, name string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	var payload struct {
		Processes map[string]json.RawMessage `json:"processes"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode state.json: %v", err)
	}
	process, ok := payload.Processes[name]
	if !ok {
		t.Fatalf("process %q missing from persisted state", name)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(process, &fields); err != nil {
		t.Fatalf("decode process %q: %v", name, err)
	}
	return fields
}

func requireTimestampFields(t *testing.T, fields map[string]json.RawMessage, wantPresent bool, names ...string) {
	t.Helper()
	for _, name := range names {
		_, got := fields[name]
		if got != wantPresent {
			t.Fatalf("timestamp field %q present=%v, want %v; fields=%v", name, got, wantPresent, fields)
		}
	}
}

func TestRuntimeStatePersistenceOmitsZeroStatusTimestamps(t *testing.T) {
	home := t.TempDir()
	rs := Init(home)
	rs.SetProcessDesired("codebase", "stopped")

	fields := persistedProcessFields(t, home, "codebase")
	requireTimestampFields(t, fields, false,
		"last_activity_at",
		"last_health_at",
		"last_healthy_at",
		"last_transition_at",
	)
}

func TestRuntimeStatePersistenceKeepsObservedStatusTimestamps(t *testing.T) {
	home := t.TempDir()
	rs := Init(home)
	rs.SetProcessLifecycle(ProcessInfo{Name: "codebase", State: "starting"})

	starting := persistedProcessFields(t, home, "codebase")
	requireTimestampFields(t, starting, true, "last_transition_at")
	requireTimestampFields(t, starting, false,
		"last_activity_at",
		"last_health_at",
		"last_healthy_at",
	)

	rs.SetProcessLifecycle(ProcessInfo{Name: "codebase", State: "healthy", Healthy: true})
	rs.RecordMCPActivityAt("codebase", time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC))

	observed := persistedProcessFields(t, home, "codebase")
	requireTimestampFields(t, observed, true,
		"last_activity_at",
		"last_health_at",
		"last_healthy_at",
		"last_transition_at",
	)
}
