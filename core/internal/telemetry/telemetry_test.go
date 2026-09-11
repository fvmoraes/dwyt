package telemetry

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ptrInt(v int) *int           { return &v }
func ptrFloat(v float64) *float64 { return &v }

func TestNewRejectsNilDatabase(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("a nil database must be rejected")
	}
}

func TestRecordRequestRequiresProject(t *testing.T) {
	if err := testStore(t).RecordRequest(RequestEvent{}); err == nil {
		t.Fatal("a request without a project cannot be attributed and must be rejected")
	}
}

func TestUnreportedFieldsStayNullNotZero(t *testing.T) {
	s := testStore(t)
	// Only input tokens reported; cached and output are unknown.
	if err := s.RecordRequest(RequestEvent{
		ProjectID: "p1", TaskID: "t1", Provider: "openai",
		InputTokens: ptrInt(1000), Observed: true,
	}); err != nil {
		t.Fatal(err)
	}

	sum, err := s.Summarize("p1", time.Unix(0, 0), "all")
	if err != nil {
		t.Fatal(err)
	}
	// No request reported cache numbers, so a hit ratio would be a fiction.
	if sum.CacheHitPct != nil {
		t.Fatalf("a cache hit ratio must not be invented: %v", *sum.CacheHitPct)
	}
	if sum.Coverage.CacheReported != 0 {
		t.Fatalf("coverage should record that nothing reported cache: %+v", sum.Coverage)
	}
	if sum.ContextReductionPct != nil {
		t.Fatal("a context reduction must not be invented")
	}

	events, err := s.RecentRequests("p1", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].CachedInputTokens != nil {
		t.Fatal("an unreported cached-token count must read back as nil, not 0")
	}
	if events[0].InputTokens == nil || *events[0].InputTokens != 1000 {
		t.Fatalf("reported values must round-trip: %+v", events[0].InputTokens)
	}
}

func TestSummarizeSeparatesObservedFromEstimatedCost(t *testing.T) {
	s := testStore(t)
	s.RecordRequest(RequestEvent{ProjectID: "p1", ActualCostUSD: ptrFloat(0.10), Observed: true})
	s.RecordRequest(RequestEvent{ProjectID: "p1", EstimatedCostUSD: ptrFloat(0.25)})

	sum, _ := s.Summarize("p1", time.Unix(0, 0), "all")
	if sum.ObservedCostUSD != 0.10 {
		t.Fatalf("observed cost %f", sum.ObservedCostUSD)
	}
	if sum.EstimatedCostUSD != 0.25 {
		t.Fatalf("estimated cost %f", sum.EstimatedCostUSD)
	}
	if sum.ObservedRequests != 1 {
		t.Fatalf("expected 1 observed request, got %d", sum.ObservedRequests)
	}
	if sum.Coverage.CostReported != 1 {
		t.Fatalf("only one request had a real cost: %+v", sum.Coverage)
	}
}

func TestCacheHitAndContextReductionWhenReported(t *testing.T) {
	s := testStore(t)
	s.RecordRequest(RequestEvent{
		ProjectID: "p1", Observed: true,
		InputTokens: ptrInt(1000), CachedInputTokens: ptrInt(700),
		ContextBefore: ptrInt(10000), ContextAfter: ptrInt(2500),
	})

	sum, _ := s.Summarize("p1", time.Unix(0, 0), "all")
	if sum.CacheHitPct == nil || *sum.CacheHitPct < 69 || *sum.CacheHitPct > 71 {
		t.Fatalf("unexpected cache hit pct: %v", sum.CacheHitPct)
	}
	if sum.ContextReductionPct == nil || *sum.ContextReductionPct < 74 || *sum.ContextReductionPct > 76 {
		t.Fatalf("unexpected context reduction: %v", sum.ContextReductionPct)
	}
	if sum.AvoidedTokens != 7500 {
		t.Fatalf("avoided tokens %d", sum.AvoidedTokens)
	}
}

func TestCostPerCompletedTaskDividesBySuccessesOnly(t *testing.T) {
	s := testStore(t)
	// Two tasks: one succeeds after spending, one fails cheaply.
	s.RecordRequest(RequestEvent{ProjectID: "p1", TaskID: "good", ActualCostUSD: ptrFloat(0.30),
		InputTokens: ptrInt(1000), OutputTokens: ptrInt(200), Observed: true})
	s.RecordRequest(RequestEvent{ProjectID: "p1", TaskID: "bad", ActualCostUSD: ptrFloat(0.01),
		InputTokens: ptrInt(50), Observed: true})
	if err := s.CompleteTask("good", "p1", true, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteTask("bad", "p1", false, false, time.Now()); err != nil {
		t.Fatal(err)
	}

	sum, _ := s.Summarize("p1", time.Unix(0, 0), "all")
	if sum.Tasks != 2 || sum.TasksSucceeded != 1 {
		t.Fatalf("unexpected task counts: %+v", sum)
	}
	if sum.CompletionPct == nil || *sum.CompletionPct != 50 {
		t.Fatalf("completion pct: %v", sum.CompletionPct)
	}
	// The headline metric must not be diluted by the cheap failure.
	if sum.CostPerCompletedTask == nil || *sum.CostPerCompletedTask != 0.30 {
		t.Fatalf("cost per completed task: %v", sum.CostPerCompletedTask)
	}
	if sum.TokensPerCompletedTask == nil || *sum.TokensPerCompletedTask != 1200 {
		t.Fatalf("tokens per completed task: %v", sum.TokensPerCompletedTask)
	}
}

func TestRatesAreNilWithoutTasks(t *testing.T) {
	s := testStore(t)
	s.RecordRequest(RequestEvent{ProjectID: "p1", InputTokens: ptrInt(10)})

	sum, _ := s.Summarize("p1", time.Unix(0, 0), "all")
	if sum.CompletionPct != nil {
		t.Fatal("a completion rate over zero tasks is meaningless, not zero")
	}
	if sum.CostPerCompletedTask != nil {
		t.Fatal("cost per completed task must be nil with no completions")
	}
}

func TestAttemptsAccumulateAcrossRequests(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 3; i++ {
		s.RecordRequest(RequestEvent{ProjectID: "p1", TaskID: "t1",
			InputTokens: ptrInt(100), EstimatedCostUSD: ptrFloat(0.01)})
	}
	s.CompleteTask("t1", "p1", true, true, time.Now())

	sum, _ := s.Summarize("p1", time.Unix(0, 0), "all")
	if sum.AvgAttempts == nil || *sum.AvgAttempts != 3 {
		t.Fatalf("attempts should accumulate: %v", sum.AvgAttempts)
	}
	if sum.CostPerCompletedTask == nil || *sum.CostPerCompletedTask < 0.029 {
		t.Fatalf("the task cost should be the sum of its requests: %v", sum.CostPerCompletedTask)
	}
}

func TestCompleteTaskBeforeAnyRequest(t *testing.T) {
	s := testStore(t)
	// Completing a task DWYT never saw a request for must still work: the agent
	// may have run entirely outside DWYT's request path.
	if err := s.CompleteTask("solo", "p1", true, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	sum, _ := s.Summarize("p1", time.Unix(0, 0), "all")
	if sum.TasksSucceeded != 1 {
		t.Fatalf("expected the completion to be recorded: %+v", sum)
	}
}

func TestCompleteTaskValidatesInput(t *testing.T) {
	s := testStore(t)
	if err := s.CompleteTask("", "p1", true, true, time.Now()); err == nil {
		t.Fatal("a missing task id must be rejected")
	}
	if err := s.CompleteTask("t", "", true, true, time.Now()); err == nil {
		t.Fatal("a missing project id must be rejected")
	}
}

func TestWindowScopesTheSummary(t *testing.T) {
	s := testStore(t)
	old := time.Now().Add(-48 * time.Hour)
	s.RecordRequest(RequestEvent{ProjectID: "p1", InputTokens: ptrInt(999), Timestamp: old})
	s.RecordRequest(RequestEvent{ProjectID: "p1", InputTokens: ptrInt(1)})

	recent, _ := s.Summarize("p1", time.Now().Add(-time.Hour), "1h")
	if recent.Requests != 1 || recent.InputTokens != 1 {
		t.Fatalf("the window should exclude the old event: %+v", recent)
	}
	all, _ := s.Summarize("p1", time.Unix(0, 0), "all")
	if all.Requests != 2 {
		t.Fatalf("the all-time window should include both: %+v", all)
	}
}

func TestSummarizeIsolatesProjects(t *testing.T) {
	s := testStore(t)
	s.RecordRequest(RequestEvent{ProjectID: "p1", InputTokens: ptrInt(100)})
	s.RecordRequest(RequestEvent{ProjectID: "p2", InputTokens: ptrInt(500)})

	sum, _ := s.Summarize("p1", time.Unix(0, 0), "all")
	if sum.InputTokens != 100 {
		t.Fatalf("another project's usage leaked in: %+v", sum)
	}
}

func TestPruneDropsEventsButKeepsTaskOutcomes(t *testing.T) {
	s := testStore(t)
	s.RecordRequest(RequestEvent{ProjectID: "p1", TaskID: "t1", InputTokens: ptrInt(100),
		Timestamp: time.Now().Add(-48 * time.Hour)})
	s.CompleteTask("t1", "p1", true, true, time.Now().Add(-48*time.Hour))

	if err := s.Prune(time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	sum, _ := s.Summarize("p1", time.Unix(0, 0), "all")
	if sum.Requests != 0 {
		t.Fatalf("old request events should be pruned: %+v", sum)
	}
	// The historical record of what DWYT achieved must survive.
	if sum.TasksSucceeded != 1 {
		t.Fatalf("task outcomes must be kept: %+v", sum)
	}
}

func TestRecordHousekeeperRun(t *testing.T) {
	s := testStore(t)
	if err := s.RecordHousekeeperRun("p1", "deep", map[string]interface{}{"sessions_removed": 3}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM housekeeper_runs WHERE project_id = ?`, "p1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one recorded run, got %d", count)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := New(db); err != nil {
		t.Fatal(err)
	}
	// Opening a second store against the same database must not fail.
	if _, err := New(db); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}
}

func TestRecentRequestsRespectsLimit(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 10; i++ {
		s.RecordRequest(RequestEvent{ProjectID: "p1", InputTokens: ptrInt(i + 1),
			Timestamp: time.Now().Add(time.Duration(i) * time.Second)})
	}
	events, err := s.RecentRequests("p1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	// Newest first.
	if events[0].Timestamp.Before(events[1].Timestamp) {
		t.Fatal("events should come back newest first")
	}
}
func TestSummarizeLatencyPreservesZeroAndCoverage(t *testing.T) {
	s := testStore(t)
	zero, positive := 0, 17
	if err := s.RecordRequest(RequestEvent{ProjectID: "p1", LatencyMS: &zero, Observed: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordRequest(RequestEvent{ProjectID: "p1", LatencyMS: &positive, Observed: true}); err != nil {
		t.Fatal(err)
	}

	sum, err := s.Summarize("p1", time.Unix(0, 0), "all")
	if err != nil {
		t.Fatal(err)
	}
	if sum.LatencyMS != positive || sum.Coverage.LatencyReported != 2 {
		t.Fatalf("latency total/coverage = %d/%d, want %d/2", sum.LatencyMS, sum.Coverage.LatencyReported, positive)
	}
	if got := sum.Provenance.For(MetricLatencyMS); got != ProvenanceObserved {
		t.Fatalf("latency provenance = %q, want observed", got)
	}

	if err := s.RecordRequest(RequestEvent{ProjectID: "p1", InputTokens: ptrInt(1)}); err != nil {
		t.Fatal(err)
	}
	sum, err = s.Summarize("p1", time.Unix(0, 0), "all")
	if err != nil {
		t.Fatal(err)
	}
	if sum.LatencyMS != positive || sum.Coverage.LatencyReported != 2 {
		t.Fatalf("partial latency total/coverage = %d/%d, want %d/2", sum.LatencyMS, sum.Coverage.LatencyReported, positive)
	}
	if got := sum.Provenance.For(MetricLatencyMS); got != ProvenanceUnsupported {
		t.Fatalf("partial latency provenance = %q, want unsupported", got)
	}
}
