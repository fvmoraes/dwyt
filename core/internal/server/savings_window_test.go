package server

import (
	"path/filepath"
	"testing"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/procman"
)

func TestNormalizeSavingsDerivesWithoutBaseline(t *testing.T) {
	// pct-based (RTK)
	if saved, without := normalizeSavings(&ToolDetail{TokensSaved: 945, PctSaved: 94.5}); saved != 945 || without != 1000 {
		t.Fatalf("pct baseline: saved=%d without=%d, want 945/1000", saved, without)
	}
	// explicit without wins
	if saved, without := normalizeSavings(&ToolDetail{TokensSaved: 100, WithoutDWYTTokens: 250}); saved != 100 || without != 250 {
		t.Fatalf("explicit without: saved=%d without=%d, want 100/250", saved, without)
	}
	// no signal -> 2x fallback
	if _, without := normalizeSavings(&ToolDetail{TokensSaved: 100}); without != 200 {
		t.Fatalf("fallback without=%d, want 200", without)
	}
	// no savings -> zero
	if saved, without := normalizeSavings(&ToolDetail{}); saved != 0 || without != 0 {
		t.Fatalf("no savings: saved=%d without=%d, want 0/0", saved, without)
	}
}

func TestSavingsWindowCutoff(t *testing.T) {
	for _, w := range []string{"1h", "6h", "24h", "2d", "7d"} {
		if _, ok := savingsWindowCutoff(w); !ok {
			t.Fatalf("window %q should be recognized", w)
		}
	}
	for _, w := range []string{"", "all", "bogus"} {
		if _, ok := savingsWindowCutoff(w); ok {
			t.Fatalf("window %q should mean no window", w)
		}
	}
}

// End-to-end: two polls record deltas, and the windowed view reflects only
// the growth between polls instead of the cumulative total — for every metric.
func TestToolDetailsWindowReflectsRecordedDeltas(t *testing.T) {
	dwytHome := t.TempDir()
	projectPath := t.TempDir()
	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ds := &DashboardServer{
		DwytHome:       dwytHome,
		DwytBin:        filepath.Join(t.TempDir(), "bin"), // no binaries -> detail funcs return uptime -1
		DefaultProject: projectPath,
		Store:          store,
		ProcMan:        procman.New(dwytHome),
	}
	pid := db.HashPath(projectPath)

	// Baseline observation then growth, directly via the store (the detail
	// funcs need real tools; the recording/aggregation path is what we test).
	if err := store.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 1000, "without": 1100, "commands": 50}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 1200, "without": 1320, "commands": 58}); err != nil {
		t.Fatal(err)
	}

	since, ok := savingsWindowCutoff("24h")
	if !ok {
		t.Fatal("24h must be a valid window")
	}
	details := map[string]*ToolDetail{"rtk": {TokensSaved: 1200, WithoutDWYTTokens: 1320, TotalCommands: 58, PctSaved: 90.9}}
	ds.applySavingsWindow(projectPath, details, since)

	if got := details["rtk"].TokensSaved; got != 200 {
		t.Fatalf("windowed rtk saved = %d, want 200 (growth, not cumulative 1200)", got)
	}
	if got := details["rtk"].WithoutDWYTTokens; got != 220 {
		t.Fatalf("windowed rtk without = %d, want 220", got)
	}
	if got := details["rtk"].TotalCommands; got != 8 {
		t.Fatalf("windowed rtk commands = %d, want 8 (58-50), not lifetime 58", got)
	}
	// pct is recomputed from the windowed totals: 200/220 ≈ 90.9%
	if got := details["rtk"].PctSaved; got < 90 || got > 92 {
		t.Fatalf("windowed rtk pct = %.2f, want ~90.9 (derived from window)", got)
	}
}
