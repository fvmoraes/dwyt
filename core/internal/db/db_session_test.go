package db

import (
	"path/filepath"
	"testing"
	"time"
)

// The mcp_usage table is cumulative by design, so it can never answer "what
// happened in this session". The event ledger is what windows and sessions are
// built from — every credited call must land there too.
func TestAddMCPUsageWritesTimestampedEvent(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	before := time.Now().Unix()
	if err := s.AddMCPUsage("proj1", "codebase", 1, 120, 300); err != nil {
		t.Fatal(err)
	}

	calls, saved, without, byTool, err := s.MCPUsageBetween("proj1", before-10, time.Now().Unix()+10)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || saved != 120 || without != 300 {
		t.Fatalf("unexpected window sums: calls=%d saved=%d without=%d", calls, saved, without)
	}
	if byTool["codebase"] != 1 {
		t.Fatalf("expected the call credited to codebase, got %v", byTool)
	}

	ts, err := s.MCPActivityTS("proj1", before-10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 {
		t.Fatalf("expected 1 activity timestamp, got %v", ts)
	}
}

func TestSumMetricsByToolBetweenRespectsUpperBound(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Unix()
	seed := func(ts int64, delta int64) {
		if _, err := s.db.Exec(
			`INSERT INTO metric_events (project_id, tool, metric, delta, ts) VALUES (?, 'rtk', 'saved', ?, ?)`,
			"proj1", delta, ts,
		); err != nil {
			t.Fatal(err)
		}
	}
	seed(now-3600, 100)
	seed(now, 50)

	sums, err := s.SumMetricsByToolBetween("proj1", now-7200, now-1800)
	if err != nil {
		t.Fatal(err)
	}
	if sums["rtk"]["saved"] != 100 {
		t.Fatalf("expected only the in-span event, got %v", sums["rtk"])
	}

	sums, err = s.SumMetricsByToolBetween("proj1", now-7200, now+60)
	if err != nil {
		t.Fatal(err)
	}
	if sums["rtk"]["saved"] != 150 {
		t.Fatalf("expected both events inside the span, got %v", sums["rtk"])
	}
}

func TestMetricActivityTS(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Unix()
	for _, ts := range []int64{now - 10, now - 10, now - 5} {
		if _, err := s.db.Exec(
			`INSERT INTO metric_events (project_id, tool, metric, delta, ts) VALUES ('proj1', 'rtk', 'saved', 1, ?)`, ts,
		); err != nil {
			t.Fatal(err)
		}
	}
	ts, err := s.MetricActivityTS("proj1", now-60)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 || ts[0] != now-10 || ts[1] != now-5 {
		t.Fatalf("expected distinct ascending timestamps, got %v", ts)
	}
}

func TestPruneMetricEventsCoversMCPEvents(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.AddMCPUsage("proj1", "codebase", 1, 10, 20); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneMetricEvents(time.Now().Add(time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	ts, err := s.MCPActivityTS("proj1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 0 {
		t.Fatalf("prune should have removed mcp_usage_events, got %v", ts)
	}
}
