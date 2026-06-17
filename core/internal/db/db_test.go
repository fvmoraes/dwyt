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
	if err := s.RecordSavingsDelta(pid, "rtk", 1000, 2000); err != nil {
		t.Fatal(err)
	}
	if sums, _ := s.SumSavingsByTool(pid, 0); len(sums) != 0 {
		t.Fatalf("first observation must not record an event, got %#v", sums)
	}

	// Growth is attributed to the window.
	if err := s.RecordSavingsDelta(pid, "rtk", 1500, 2800); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSavingsDelta(pid, "headroom", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSavingsDelta(pid, "headroom", 300, 600); err != nil {
		t.Fatal(err)
	}

	sums, err := s.SumSavingsByTool(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sums["rtk"].Saved != 500 || sums["rtk"].Without != 800 {
		t.Fatalf("rtk window = %#v, want saved 500 / without 800", sums["rtk"])
	}
	if sums["headroom"].Saved != 300 {
		t.Fatalf("headroom window saved = %d, want 300", sums["headroom"].Saved)
	}

	// A cumulative drop (reindex/reset) rebases without emitting an event.
	if err := s.RecordSavingsDelta(pid, "rtk", 100, 200); err != nil {
		t.Fatal(err)
	}
	if sums, _ := s.SumSavingsByTool(pid, 0); sums["rtk"].Saved != 500 {
		t.Fatalf("reset must not change recorded events, got %#v", sums["rtk"])
	}

	// Future cutoff excludes past events.
	future := time.Now().Add(time.Hour).Unix()
	if sums, _ := s.SumSavingsByTool(pid, future); len(sums) != 0 {
		t.Fatalf("future cutoff should exclude all events, got %#v", sums)
	}
}
