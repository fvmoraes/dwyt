package db

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "dwyt.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRemoveProjectIsLogicalAndRestorable(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.UpsertProject("/tmp/projA"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProject("/tmp/projB"); err != nil {
		t.Fatal(err)
	}

	// Soft-remove projA.
	if err := s.RemoveProject("/tmp/projA"); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Path != "/tmp/projB" {
		t.Fatalf("removed project should be hidden from the active list, got %#v", list)
	}

	// The row must still exist (data preserved, only logically hidden).
	if p, err := s.GetProjectByPath("/tmp/projA"); err != nil {
		t.Fatalf("removed project row must still exist: %v", err)
	} else if p.Path != "/tmp/projA" {
		t.Fatalf("unexpected project: %#v", p)
	}

	// Re-adding the same project restores it to the active list.
	if _, err := s.UpsertProject("/tmp/projA"); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("re-adding a project should restore it, got %#v", list)
	}
}

func TestTouchProjectRestoresRemovedProject(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.UpsertProject("/tmp/proj"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveProject("/tmp/proj"); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchProject("/tmp/proj"); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("TouchProject should restore a removed project, got %#v", list)
	}
}

func TestHeadroomSavingsAreAttributedPerProject(t *testing.T) {
	s := newTestStore(t)
	idA := HashPath("/tmp/projA")
	idB := HashPath("/tmp/projB")

	if err := s.AddHeadroomSavings(idA, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHeadroomSavings(idA, 50); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHeadroomSavings(idB, 30); err != nil {
		t.Fatal(err)
	}

	if v, err := s.GetHeadroomSavings(idA); err != nil || v != 150 {
		t.Fatalf("projA savings = %d (err %v), want 150", v, err)
	}
	if v, err := s.GetHeadroomSavings(idB); err != nil || v != 30 {
		t.Fatalf("projB savings = %d (err %v), want 30", v, err)
	}
	// Unknown project has no savings, not an error.
	if v, err := s.GetHeadroomSavings(HashPath("/tmp/unknown")); err != nil || v != 0 {
		t.Fatalf("unknown project savings = %d (err %v), want 0", v, err)
	}
	// Non-positive deltas are ignored.
	if err := s.AddHeadroomSavings(idA, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHeadroomSavings(idA, -10); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.GetHeadroomSavings(idA); v != 150 {
		t.Fatalf("non-positive deltas must not change savings, got %d", v)
	}
}

func TestSavingsTimeWindowTracksDeltasPerTool(t *testing.T) {
	s := newTestStore(t)
	pid := HashPath("/tmp/proj")

	// First observation sets the baseline — no event recorded yet.
	if err := s.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 1000, "without": 2000, "commands": 10}); err != nil {
		t.Fatal(err)
	}
	if sums, _ := s.SumMetricsByTool(pid, 0); len(sums) != 0 {
		t.Fatalf("first observation must not record an event, got %#v", sums)
	}

	// Growth is attributed to the window, across every metric.
	if err := s.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 1500, "without": 2800, "commands": 14}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordMetricDeltas(pid, "headroom", map[string]int64{"saved": 0, "without": 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordMetricDeltas(pid, "headroom", map[string]int64{"saved": 300, "without": 600, "requests": 5}); err != nil {
		t.Fatal(err)
	}

	sums, err := s.SumMetricsByTool(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sums["rtk"]["saved"] != 500 || sums["rtk"]["without"] != 800 || sums["rtk"]["commands"] != 4 {
		t.Fatalf("rtk window = %#v, want saved 500 / without 800 / commands 4", sums["rtk"])
	}
	if sums["headroom"]["saved"] != 300 {
		t.Fatalf("headroom window saved = %d, want 300", sums["headroom"]["saved"])
	}

	// A cumulative drop (reindex/reset) rebases without emitting an event.
	if err := s.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 100, "without": 200, "commands": 1}); err != nil {
		t.Fatal(err)
	}
	if sums, _ := s.SumMetricsByTool(pid, 0); sums["rtk"]["saved"] != 500 {
		t.Fatalf("reset must not change recorded events, got %#v", sums["rtk"])
	}

	// Future cutoff excludes past events.
	future := time.Now().Add(time.Hour).Unix()
	if sums, _ := s.SumMetricsByTool(pid, future); len(sums) != 0 {
		t.Fatalf("future cutoff should exclude all events, got %#v", sums)
	}
}

func TestPruneMetricEventsDropsOldRows(t *testing.T) {
	s := newTestStore(t)
	pid := HashPath("/tmp/proj")
	// Seed a baseline then growth so one event exists.
	if err := s.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 200}); err != nil {
		t.Fatal(err)
	}
	if sums, _ := s.SumMetricsByTool(pid, 0); sums["rtk"]["saved"] != 100 {
		t.Fatalf("expected one recorded event of 100, got %#v", sums["rtk"])
	}
	// Prune everything up to the future — table should be empty.
	if err := s.PruneMetricEvents(time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if sums, _ := s.SumMetricsByTool(pid, 0); len(sums) != 0 {
		t.Fatalf("prune should have dropped all events, got %#v", sums)
	}
}
