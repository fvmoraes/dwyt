package benchmark

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/contextopt"
)

// The benchmark's value depends entirely on two properties: it must be
// reproducible, and it must never report a number it did not measure. These
// tests guard both, because a harness that drifts or that quietly fabricates a
// cost figure is worse than no harness at all.

func TestRunIsDeterministic(t *testing.T) {
	first := Run()
	second := Run()

	if len(first.Results) != len(second.Results) {
		t.Fatalf("scenario count changed between runs: %d vs %d", len(first.Results), len(second.Results))
	}
	for i := range first.Results {
		a, b := first.Results[i], second.Results[i]
		if a.Scenario != b.Scenario {
			t.Fatalf("scenario order changed: %s vs %s", a.Scenario, b.Scenario)
		}
		if len(a.Arms) != len(b.Arms) {
			t.Fatalf("%s: arm count changed", a.Scenario)
		}
		for j := range a.Arms {
			if !reflect.DeepEqual(a.Arms[j], b.Arms[j]) {
				t.Fatalf("%s/%s: measurement changed between runs\n%+v\n%+v",
					a.Scenario, a.Arms[j].Arm, a.Arms[j], b.Arms[j])
			}
		}
	}
	if first.Render() != second.Render() {
		t.Fatal("rendered report changed between runs")
	}
}

func TestScenariosCoverTheSpecCases(t *testing.T) {
	scenarios := Scenarios()
	if len(scenarios) != 5 {
		t.Fatalf("spec §69 names five scenarios, got %d", len(scenarios))
	}
	wantIDs := map[string]bool{"A": true, "B": true, "C": true, "D": true, "E": true}
	for _, s := range scenarios {
		if !wantIDs[s.ID] {
			t.Fatalf("unexpected scenario %q", s.ID)
		}
		delete(wantIDs, s.ID)
		if len(s.Blocks) == 0 {
			t.Fatalf("%s: no context blocks", s.ID)
		}
		if s.Turns < 1 {
			t.Fatalf("%s: turns must be at least 1", s.ID)
		}
	}
	if len(wantIDs) != 0 {
		t.Fatalf("missing scenarios: %v", wantIDs)
	}

	// The long agentic case is the one the spec sizes explicitly: 10-20 turns.
	long, ok := scenarioByID("E")
	if !ok {
		t.Fatal("scenario E is missing")
	}
	if long.Turns < 10 || long.Turns > 20 {
		t.Fatalf("scenario E must run 10-20 turns, got %d", long.Turns)
	}
}

func TestEveryScenarioMeasuresEveryArm(t *testing.T) {
	for _, res := range Run().Results {
		if len(res.Arms) != len(Arms()) {
			t.Fatalf("%s: expected %d arms, got %d", res.Scenario, len(Arms()), len(res.Arms))
		}
		for i, want := range Arms() {
			if res.Arms[i].Arm != want {
				t.Fatalf("%s: arm %d is %s, want %s", res.Scenario, i, res.Arms[i].Arm, want)
			}
		}
	}
}

func TestOptimizedArmsSendLessThanUngovernedOnes(t *testing.T) {
	for _, res := range Run().Results {
		base := mustArm(t, res, ArmBaseline)
		current := mustArm(t, res, ArmCurrent)
		opt := mustArm(t, res, ArmOptimized)

		if current.InputTokens >= base.InputTokens {
			t.Errorf("%s: symbol-level retrieval should beat whole-file reads (%d vs %d)",
				res.Scenario, current.InputTokens, base.InputTokens)
		}
		if opt.InputTokens >= current.InputTokens {
			t.Errorf("%s: the optimizer should send less than ungoverned DWYT (%d vs %d)",
				res.Scenario, opt.InputTokens, current.InputTokens)
		}
		if opt.InputReductionPct <= 0 {
			t.Errorf("%s: input reduction should be positive, got %.1f%%", res.Scenario, opt.InputReductionPct)
		}
		// A reduction of exactly 100% would mean nothing was sent, which cannot
		// be right: every task needs some context.
		if opt.InputReductionPct >= 100 {
			t.Errorf("%s: input reduction of %.1f%% implies an empty context",
				res.Scenario, opt.InputReductionPct)
		}
	}
}

func TestMemoryRetrievalIsBoundedBySearchV2(t *testing.T) {
	for _, res := range Run().Results {
		current := mustArm(t, res, ArmCurrent)
		opt := mustArm(t, res, ArmOptimized)
		if current.MemoryTokens == 0 {
			t.Fatalf("%s: the dwyt_v4 arm should search the vault", res.Scenario)
		}
		if opt.MemoryTokens >= current.MemoryTokens {
			t.Errorf("%s: Search V2 should retrieve less memory than the broad search (%d vs %d)",
				res.Scenario, opt.MemoryTokens, current.MemoryTokens)
		}
		if opt.MemoryTokens == 0 {
			t.Errorf("%s: Search V2 returning nothing would starve the task", res.Scenario)
		}
		if opt.MemoryReductionVsCurrentPct == nil || *opt.MemoryReductionVsCurrentPct <= 0 {
			t.Errorf("%s: Search V2 should report a measured memory reduction", res.Scenario)
		}

		// The baseline never touches the vault, so "100% less memory" would be a
		// flattering non-fact. It must report nothing at all.
		base := mustArm(t, res, ArmBaseline)
		if base.MemoryReductionVsCurrentPct != nil {
			t.Errorf("%s: an arm with no project memory must not claim a reduction", res.Scenario)
		}
	}
}

func TestToolOutputIsCompactedWhereThereIsOutput(t *testing.T) {
	for _, res := range Run().Results {
		base := mustArm(t, res, ArmBaseline)
		opt := mustArm(t, res, ArmOptimized)
		if base.ToolOutputTokens == 0 {
			// Scenario A runs no tool, which is itself the point.
			if opt.ToolOutputTokens != 0 {
				t.Errorf("%s: no raw output but %d compacted tokens", res.Scenario, opt.ToolOutputTokens)
			}
			continue
		}
		if opt.ToolOutputTokens >= base.ToolOutputTokens {
			t.Errorf("%s: compaction should shrink tool output (%d vs %d)",
				res.Scenario, opt.ToolOutputTokens, base.ToolOutputTokens)
		}
	}
}

func TestOutputBudgetIsGovernedNotUnbounded(t *testing.T) {
	for _, res := range Run().Results {
		base := mustArm(t, res, ArmBaseline)
		opt := mustArm(t, res, ArmOptimized)
		if base.OutputBudget != ungovernedOutputTokens {
			t.Fatalf("%s: baseline output budget changed unexpectedly", res.Scenario)
		}
		if opt.OutputBudget <= 0 {
			t.Errorf("%s: an operational phase must still get an output budget", res.Scenario)
		}
		if opt.OutputBudget >= base.OutputBudget {
			t.Errorf("%s: the output contract should shrink the answer (%d vs %d)",
				res.Scenario, opt.OutputBudget, base.OutputBudget)
		}
	}
}

// The cache arm deliberately puts more tokens back on the wire in exchange for a
// cheaper rate. Asserting both halves keeps the tradeoff visible: if someone
// "optimizes" the cache arm into sending fewer tokens than the plain arm, they
// have changed what it models.
func TestCacheArmTradesTokensForRate(t *testing.T) {
	for _, res := range Run().Results {
		plain := mustArm(t, res, ArmOptimized)
		cached := mustArm(t, res, ArmOptimizedCached)
		if res.Turns == 1 {
			if cached.InputTokens != plain.InputTokens {
				t.Errorf("%s: a single-turn task has no prefix to reuse", res.Scenario)
			}
			continue
		}
		if cached.InputTokens <= plain.InputTokens {
			t.Errorf("%s: the cache arm resends the prefix, so it must send more (%d vs %d)",
				res.Scenario, cached.InputTokens, plain.InputTokens)
		}
		if cached.CacheReadTokens == 0 {
			t.Errorf("%s: a multi-turn cache arm must bill cache reads", res.Scenario)
		}
		if cached.CacheWriteTokens == 0 {
			t.Errorf("%s: populating the cache costs a write", res.Scenario)
		}
		// Cheaper per token than sending the same bytes uncached.
		uncachedEquivalent := float64(cached.InputTokens) * contextopt.DefaultCostModel().Uncached
		if cached.CostUnits >= uncachedEquivalent {
			t.Errorf("%s: caching must lower the cost of the same payload (%.0f vs %.0f)",
				res.Scenario, cached.CostUnits, uncachedEquivalent)
		}
	}
}

func TestReportRefusesToClaimSavings(t *testing.T) {
	report := Run()
	if report.ClaimAllowed {
		t.Fatal("this harness cannot observe completion rate, so it must never allow a savings claim")
	}
	if strings.TrimSpace(report.ClaimNote) == "" {
		t.Fatal("refusing a claim without saying why is not honest, it is silent")
	}

	want := map[string]bool{
		"completion_rate":                      false,
		"cost_per_successfully_completed_task": false,
		"cache_hit_rate":                       false,
		"latency":                              false,
	}
	for _, u := range report.Unmeasured {
		if _, ok := want[u.KPI]; !ok {
			t.Fatalf("unexpected unmeasured KPI %q", u.KPI)
		}
		if strings.TrimSpace(u.Reason) == "" {
			t.Fatalf("%s: an unmeasured KPI needs a reason", u.KPI)
		}
		want[u.KPI] = true
	}
	for kpi, seen := range want {
		if !seen {
			t.Errorf("%s must be declared unmeasured; a fixture cannot produce it", kpi)
		}
	}
}

func TestTotalsAggregateEveryArm(t *testing.T) {
	report := Run()
	if len(report.Totals) != len(Arms()) {
		t.Fatalf("expected totals for %d arms, got %d", len(Arms()), len(report.Totals))
	}

	byArm := map[Arm]Totals{}
	for _, t := range report.Totals {
		byArm[t.Arm] = t
	}
	for _, arm := range Arms() {
		want := 0
		for _, res := range report.Results {
			m, ok := res.Arm(arm)
			if !ok {
				t.Fatalf("%s: missing arm %s", res.Scenario, arm)
			}
			want += m.InputTokens
		}
		if got := byArm[arm].InputTokens; got != want {
			t.Errorf("%s: total input %d, sum of scenarios %d", arm, got, want)
		}
	}

	if byArm[ArmBaseline].InputReductionPct != 0 {
		t.Error("the baseline cannot be a reduction against itself")
	}
	if byArm[ArmOptimized].InputReductionPct <= 0 {
		t.Error("the optimizer should reduce total input against the baseline")
	}
}

func TestRenderIncludesEveryArmAndTheHonestyNote(t *testing.T) {
	out := Run().Render()
	for _, arm := range Arms() {
		if !strings.Contains(out, string(arm)) {
			t.Errorf("rendered report omits arm %s", arm)
		}
	}
	if !strings.Contains(out, "claim_allowed: false") {
		t.Error("the rendered report must state that no claim is allowed")
	}
	if !strings.Contains(out, "not currency") {
		t.Error("the rendered report must label cost units as relative, not money")
	}
}

func TestOptimizedMemoryRespectsTopKAndCeiling(t *testing.T) {
	// Ten eligible canonical notes, each well under the ceiling: only the top-k
	// may be counted.
	notes := make([]VaultNote, 0, 12)
	for i := 0; i < 10; i++ {
		notes = append(notes, VaultNote{Key: "canon", Tokens: 100, Canonical: true})
	}
	notes = append(notes,
		VaultNote{Key: "raw", Tokens: 5000, Raw: true},
		VaultNote{Key: "stale", Tokens: 5000, Stale: true},
	)
	got := optimizedMemoryTokens(notes)
	if want := 100 * 5; got != want {
		t.Fatalf("expected the top-%d of 100-token notes (%d), got %d", 5, want, got)
	}

	// A single note above the ceiling is still returned: refusing to return
	// anything would starve the task, and the ceiling is a bound on the set.
	if got := optimizedMemoryTokens([]VaultNote{{Key: "big", Tokens: 9000, Canonical: true}}); got != 9000 {
		t.Fatalf("a lone oversized note must still be retrievable, got %d", got)
	}
}

func TestBaselinePaysForWholeFilesTheOptimizerNeverLoads(t *testing.T) {
	s, ok := scenarioByID("C")
	if !ok {
		t.Fatal("scenario C is missing")
	}
	symbolTokens, fullFileTokens := 0, 0
	for _, b := range s.Blocks {
		symbolTokens += b.Candidate.Tokens
		fullFileTokens += b.baselineTokens()
	}
	if fullFileTokens <= symbolTokens {
		t.Fatal("the fixture must model whole-file reads costing more than symbols")
	}
}

func scenarioByID(id string) (Scenario, bool) {
	for _, s := range Scenarios() {
		if s.ID == id {
			return s, true
		}
	}
	return Scenario{}, false
}

func mustArm(t *testing.T, res Result, arm Arm) Measurement {
	t.Helper()
	m, ok := res.Arm(arm)
	if !ok {
		t.Fatalf("%s: missing arm %s", res.Scenario, arm)
	}
	return m
}
