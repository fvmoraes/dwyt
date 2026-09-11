package state

import (
	"testing"
	"time"
)

func TestProcessLifecycleTracksStatusTimestampsAndActivity(t *testing.T) {
	rs := Init(t.TempDir())
	rs.SetProcessLifecycle(ProcessInfo{Name: "codebase", State: "starting"})
	starting, ok := rs.GetProcess("codebase")
	if !ok || starting.LastTransitionAt.IsZero() {
		t.Fatalf("starting lifecycle did not set transition time: %+v, exists=%v", starting, ok)
	}

	rs.SetProcessLifecycle(ProcessInfo{Name: "codebase", State: "healthy", Healthy: true})
	healthy, ok := rs.GetProcess("codebase")
	if !ok || healthy.LastHealthAt.IsZero() || healthy.LastHealthyAt.IsZero() {
		t.Fatalf("healthy lifecycle did not set health timestamps: %+v, exists=%v", healthy, ok)
	}

	activity := time.Now().Add(-time.Second)
	rs.RecordMCPActivityAt("codebase", activity)
	observed, ok := rs.GetProcess("codebase")
	if !ok || !observed.LastActivityAt.Equal(activity) {
		t.Fatalf("activity timestamp = %s, want %s", observed.LastActivityAt, activity)
	}
}

func TestStatusSnapshotsAreCopies(t *testing.T) {
	rs := Init(t.TempDir())
	rs.SetToolError("codebase", "boom")
	rs.SetClients([]string{"kiro"})

	errors := rs.ToolErrorsSnapshot()
	clients := rs.ClientsSnapshot()
	errors["codebase"] = "mutated"
	clients[0] = "mutated"

	if got := rs.ToolErrorsSnapshot()["codebase"]; got != "boom" {
		t.Fatalf("tool error snapshot leaked mutation: %q", got)
	}
	if got := rs.ClientsSnapshot()[0]; got != "kiro" {
		t.Fatalf("clients snapshot leaked mutation: %q", got)
	}
}
