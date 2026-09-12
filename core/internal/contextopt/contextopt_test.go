package contextopt

import (
	"testing"
)

func TestComputeBudgetScalesWithComplexityAndPhase(t *testing.T) {
	trivial := ComputeBudget(BudgetProfile{Complexity: ComplexityTrivial, Phase: PhaseFix})
	medium := ComputeBudget(BudgetProfile{Complexity: ComplexityMedium, Phase: PhaseFix})
	complex := ComputeBudget(BudgetProfile{Complexity: ComplexityComplex, Phase: PhaseFix})

	if trivial.Total >= medium.Total || medium.Total >= complex.Total {
		t.Fatalf("budget must grow with complexity: %d %d %d", trivial.Total, medium.Total, complex.Total)
	}
	if medium.Total != DefaultBudget {
		t.Fatalf("medium/fix should equal the default budget, got %d", medium.Total)
	}
	// A classification step must not budget for the codebase.
	classify := ComputeBudget(BudgetProfile{Complexity: ComplexityMedium, Phase: PhaseClassify})
	if classify.Total >= medium.Total {
		t.Fatalf("classify budget %d should be far below fix budget %d", classify.Total, medium.Total)
	}
}

func TestComputeBudgetNeverExceedsCeilings(t *testing.T) {
	b := ComputeBudget(BudgetProfile{Complexity: ComplexityCritical, Phase: PhasePlan, MaxBudget: 10000})
	if b.Total > 10000 {
		t.Fatalf("budget %d exceeded MaxBudget", b.Total)
	}
	b = ComputeBudget(BudgetProfile{Complexity: ComplexityCritical, ModelContextWindow: 8000})
	if b.Total > 8000 {
		t.Fatalf("budget %d exceeded the model context window", b.Total)
	}
	b = ComputeBudget(BudgetProfile{Complexity: ComplexityCritical, LongContextThreshold: 30000})
	if b.Total > 30000 {
		t.Fatalf("budget %d crossed the long-context threshold without permission", b.Total)
	}
	b = ComputeBudget(BudgetProfile{Complexity: ComplexityCritical, LongContextThreshold: 30000, AllowLongContext: true})
	if b.Total <= 30000 {
		t.Fatalf("explicit permission should allow crossing the threshold, got %d", b.Total)
	}
}

func TestBudgetReserveIsHeldBack(t *testing.T) {
	b := ComputeBudget(BudgetProfile{})
	if b.Reserve <= 0 {
		t.Fatal("a budget with no reserve leaves nothing for tool loops or the answer")
	}
	if b.Retrievable() != b.Total-b.Reserve {
		t.Fatalf("retrievable %d != total-reserve %d", b.Retrievable(), b.Total-b.Reserve)
	}
	sections := b.SystemAndPolicy + b.ProjectMemory + b.RelevantCode + b.ToolResults
	if sections != b.Retrievable() {
		t.Fatalf("section split %d must sum to the retrievable pool %d", sections, b.Retrievable())
	}
}

func TestExpandRespectsCeilingAndReportsGrant(t *testing.T) {
	b := ComputeBudget(BudgetProfile{MaxBudget: 20000, DefaultBudget: 10000})
	next, granted := b.Expand(5000)
	if granted != 5000 || next.Total != b.Total+5000 {
		t.Fatalf("expected full grant, got granted=%d total=%d", granted, next.Total)
	}
	// Second expansion must be clipped at the ceiling.
	capped, granted := next.Expand(100000)
	if capped.Total != 20000 {
		t.Fatalf("expected clipping at MaxBudget, got %d", capped.Total)
	}
	if granted != 20000-next.Total {
		t.Fatalf("granted %d should equal the remaining headroom", granted)
	}
	// At the ceiling nothing more is granted.
	if _, granted := capped.Expand(1000); granted != 0 {
		t.Fatalf("expected no grant at the ceiling, got %d", granted)
	}
}

func TestNormalizeFillsDefaultsWithoutOverridingCallerValues(t *testing.T) {
	c := ContextCandidate{Kind: KindDecision, Relevance: 0.9}
	c.Normalize()
	if c.Relevance != 0.9 {
		t.Fatal("Normalize must not overwrite an explicit signal")
	}
	if c.CacheClass != CacheLongLived {
		t.Fatalf("a decision should default to long_lived, got %s", c.CacheClass)
	}
	if c.State != StateActive || c.ID == "" {
		t.Fatalf("expected active state and derived id, got %+v", c)
	}
	// A raw log must never default into a stable cache class.
	raw := ContextCandidate{Kind: KindRawLog}
	raw.Normalize()
	if raw.CacheClass != CacheVolatile || raw.Temperature != Cold {
		t.Fatalf("raw logs must be volatile and cold, got %s/%s", raw.CacheClass, raw.Temperature)
	}
}

func TestCachedContentRanksAboveEquivalentUncached(t *testing.T) {
	base := ContextCandidate{
		Kind: KindArchitecture, Tokens: 4000, ContentHash: "sha256:aaa",
		Relevance: 0.8, Confidence: 0.9, Recency: 0.5, Dependency: 0.5,
	}
	other := base
	other.ContentHash = "sha256:bbb"

	in := RankInput{Cost: DefaultCostModel(), CachedHashes: map[string]bool{"sha256:aaa": true}}
	ranked := Rank([]ContextCandidate{other, base}, in)
	if ranked[0].Candidate.ContentHash != "sha256:aaa" {
		t.Fatalf("cached content should rank first, got %s", ranked[0].Candidate.ContentHash)
	}
}

func TestSeenContentIsReusedInsteadOfResent(t *testing.T) {
	c := ContextCandidate{Kind: KindSymbol, Tokens: 500, ContentHash: "sha256:seen"}
	in := RankInput{Cost: DefaultCostModel(), SeenHashes: map[string]bool{"sha256:seen": true}}
	sel := Select(Rank([]ContextCandidate{c}, in), ComputeBudget(BudgetProfile{}))
	if len(sel.Reused) != 1 || len(sel.Included) != 0 {
		t.Fatalf("already-seen content must be reused, not resent: %+v", sel)
	}
	if sel.TokensIncluded != 0 {
		t.Fatalf("reused content must not consume budget, spent %d", sel.TokensIncluded)
	}
}

func TestSelectIncludesPinnedEvenOverBudget(t *testing.T) {
	budget := ComputeBudget(BudgetProfile{Phase: PhaseClassify, Complexity: ComplexityTrivial})
	huge := ContextCandidate{Kind: KindConstraint, Tokens: budget.Total * 10, Pinned: true, ContentHash: "sha256:pin"}
	filler := ContextCandidate{Kind: KindSessionHistory, Tokens: 100, ContentHash: "sha256:filler"}

	sel := Select(Rank([]ContextCandidate{filler, huge}, RankInput{Cost: DefaultCostModel()}), budget)
	found := false
	for _, sc := range sel.Included {
		if sc.Candidate.Pinned {
			found = true
		}
	}
	if !found {
		t.Fatal("pinned candidates must never be dropped for budget reasons")
	}
}

func TestSelectOrdersIncludedByCacheClass(t *testing.T) {
	items := []ContextCandidate{
		{ID: "vol", Kind: KindDiff, Tokens: 10, ContentHash: "h1"},
		{ID: "imm", Kind: KindPolicy, Tokens: 10, ContentHash: "h2"},
		{ID: "sess", Kind: KindSessionState, Tokens: 10, ContentHash: "h3"},
		{ID: "long", Kind: KindArchitecture, Tokens: 10, ContentHash: "h4"},
	}
	sel := Select(Rank(items, RankInput{Cost: DefaultCostModel()}), ComputeBudget(BudgetProfile{}))
	want := []CacheClass{CacheImmutable, CacheLongLived, CacheSession, CacheVolatile}
	if len(sel.Included) != 4 {
		t.Fatalf("expected all four included, got %d", len(sel.Included))
	}
	for i, sc := range sel.Included {
		if sc.Candidate.CacheClass != want[i] {
			t.Fatalf("position %d: want %s, got %s (order: %v)", i, want[i], sc.Candidate.CacheClass, cacheOrderOf(sel.Included))
		}
	}
}

func cacheOrderOf(items []ScoredCandidate) []CacheClass {
	out := make([]CacheClass, 0, len(items))
	for _, i := range items {
		out = append(out, i.Candidate.CacheClass)
	}
	return out
}

func TestTrimCutsNoiseBeforeKnowledge(t *testing.T) {
	items := []ContextCandidate{
		{ID: "banner", Kind: KindToolOutput, Tokens: 500, State: StateDiscardable, ContentHash: "h1"},
		{ID: "oldbuild", Kind: KindToolOutput, Tokens: 500, State: StateStale, ContentHash: "h2"},
		{ID: "adr", Kind: KindDecision, Tokens: 500, ContentHash: "h3"},
		{ID: "constraint", Kind: KindConstraint, Tokens: 500, ContentHash: "h4"},
	}
	res := Trim(items, 1000, RankInput{Cost: DefaultCostModel()})
	if !res.TargetMet {
		t.Fatalf("expected the target to be met: %+v", res)
	}
	kept := map[string]bool{}
	for _, c := range res.Kept {
		kept[c.ID] = true
	}
	if !kept["adr"] || !kept["constraint"] {
		t.Fatalf("knowledge was cut before noise: kept %v", kept)
	}
	if kept["banner"] || kept["oldbuild"] {
		t.Fatalf("discardable/stale content survived: kept %v", kept)
	}
	// Cut order must be reported so a removal is explainable.
	for _, r := range res.Removed {
		if r.Reason == "" {
			t.Fatalf("removal without a reason: %+v", r)
		}
	}
}

func TestTrimCollapsesDuplicatesWhenNeeded(t *testing.T) {
	items := []ContextCandidate{
		{ID: "a", Kind: KindFileSummary, Tokens: 400, ContentHash: "same"},
		{ID: "b", Kind: KindFileSummary, Tokens: 400, ContentHash: "same"},
	}
	res := Trim(items, 400, RankInput{Cost: DefaultCostModel()})
	if len(res.Kept) != 1 {
		t.Fatalf("expected one copy to survive, got %d", len(res.Kept))
	}
	if len(res.Removed) != 1 || res.Removed[0].Reason != ReasonDuplicate {
		t.Fatalf("expected a duplicate removal, got %+v", res.Removed)
	}
}

func TestTrimUsesNormativeCutOrder(t *testing.T) {
	items := []ContextCandidate{
		{ID: "raw", Kind: KindRawLog, Tokens: 100, ContentHash: "raw"},
		{ID: "first-copy", Kind: KindFileSummary, Tokens: 100, ContentHash: "same"},
		{ID: "second-copy", Kind: KindFileSummary, Tokens: 100, ContentHash: "same"},
	}
	res := Trim(items, 100, RankInput{Cost: DefaultCostModel()})
	if len(res.Removed) != 2 {
		t.Fatalf("expected raw and duplicate removals, got %+v", res.Removed)
	}
	if res.Removed[0].Reason != ReasonResolvedRaw || res.Removed[1].Reason != ReasonDuplicate {
		t.Fatalf("got cut order %q then %q, want %q then %q", res.Removed[0].Reason, res.Removed[1].Reason, ReasonResolvedRaw, ReasonDuplicate)
	}
}

func TestTrimCompactsResolvedNonCriticalContent(t *testing.T) {
	items := []ContextCandidate{
		{ID: "fixed", Kind: KindToolOutput, Tokens: 2000, State: StateResolved, ContentHash: "h1"},
		{ID: "live", Kind: KindError, Tokens: 200, ContentHash: "h2"},
	}
	res := Trim(items, 400, RankInput{Cost: DefaultCostModel()})
	if len(res.Compacted) != 1 {
		t.Fatalf("resolved non-critical content should be compacted to a line, got %+v", res)
	}
	for _, c := range res.Kept {
		if c.ID == "fixed" && c.Tokens >= 2000 {
			t.Fatal("resolved non-critical content kept its full size")
		}
	}
}

func TestTrimNeverDeduplicatesCriticalEvidence(t *testing.T) {
	items := []ContextCandidate{
		{ID: "first-error", Kind: KindError, Tokens: 100, ContentHash: "same-error"},
		{ID: "second-error", Kind: KindError, Tokens: 100, ContentHash: "same-error"},
	}
	res := Trim(items, 10, RankInput{Cost: DefaultCostModel()})
	if len(res.Kept) != len(items) {
		t.Fatalf("critical evidence must survive duplicate handling, got %+v", res)
	}
	if res.TargetMet {
		t.Fatal("TargetMet must be false when critical evidence overshoots the target")
	}
	for _, removed := range res.Removed {
		if removed.Reason == ReasonDuplicate {
			t.Fatalf("critical evidence was deduplicated: %+v", removed)
		}
	}
}

func TestTrimPreservesCriticalEvidenceWhenTargetIsImpossible(t *testing.T) {
	items := []ContextCandidate{
		{ID: "active-error", Kind: KindError, Tokens: 100, ContentHash: "active-error", ContentRef: "dwyt://objects/active-error"},
		{ID: "resolved-error", Kind: KindError, Tokens: 100, State: StateResolved, ContentHash: "resolved-error", ContentRef: "dwyt://objects/resolved-error"},
		{ID: "constraint", Kind: KindConstraint, Tokens: 100, ContentHash: "constraint"},
		{ID: "pinned", Kind: KindRawLog, Tokens: 100, State: StateDiscardable, Pinned: true, ContentHash: "pinned"},
		{ID: "noise", Kind: KindToolOutput, Tokens: 100, State: StateDiscardable, ContentHash: "noise"},
	}
	res := Trim(items, 50, RankInput{Cost: DefaultCostModel()})
	if res.TargetMet {
		t.Fatal("TargetMet must be false when only critical evidence remains over budget")
	}

	kept := make(map[string]ContextCandidate, len(res.Kept))
	for _, candidate := range res.Kept {
		kept[candidate.ID] = candidate
	}
	for _, id := range []string{"active-error", "resolved-error", "constraint", "pinned"} {
		if _, ok := kept[id]; !ok {
			t.Fatalf("critical evidence %q was cut: %+v", id, res)
		}
	}
	if kept["resolved-error"].Tokens != 100 || kept["resolved-error"].ContentRef != "dwyt://objects/resolved-error" {
		t.Fatalf("resolved error must retain its raw reference and full metadata: %+v", kept["resolved-error"])
	}
	for _, candidate := range res.Compacted {
		if candidate.ID == "active-error" || candidate.ID == "resolved-error" || candidate.ID == "constraint" || candidate.ID == "pinned" {
			t.Fatalf("critical evidence was compacted: %+v", candidate)
		}
	}
	for _, removed := range res.Removed {
		if removed.Candidate.ID == "active-error" || removed.Candidate.ID == "resolved-error" || removed.Candidate.ID == "constraint" || removed.Candidate.ID == "pinned" {
			t.Fatalf("critical evidence was removed: %+v", removed)
		}
	}
}

func TestTrimNeverRemovesPinned(t *testing.T) {
	items := []ContextCandidate{
		{ID: "pin", Kind: KindRawLog, Tokens: 5000, State: StateDiscardable, Pinned: true, ContentHash: "h1"},
	}
	res := Trim(items, 10, RankInput{Cost: DefaultCostModel()})
	if len(res.Kept) != 1 {
		t.Fatal("pinned content must survive even an impossible target")
	}
	if res.TargetMet {
		t.Fatal("TargetMet must be false when pinned content overshoots the target")
	}
}

func TestInvalidateMarksDerivedSummariesStale(t *testing.T) {
	items := []ContextCandidate{
		{ID: "sum", Kind: KindFileSummary, ContentRef: "internal/db/db.go", Tokens: 100},
		{ID: "other", Kind: KindFileSummary, ContentRef: "internal/brain/brain.go", Tokens: 100},
	}
	out := Invalidate(items, EventFileChanged, "internal/db/db.go")
	if out[0].State != StateStale {
		t.Fatalf("summary of the changed file should be stale, got %s", out[0].State)
	}
	if out[1].State == StateStale {
		t.Fatal("an unrelated file summary must not be invalidated")
	}
}

func TestSessionDeltaDecisions(t *testing.T) {
	s := NewSession("t1", ComputeBudget(BudgetProfile{}))

	first, _ := s.ObserveSymbol(SymbolState{Path: "a.go", Symbol: "F", ContentHash: "h1"})
	if first != DeltaFull {
		t.Fatalf("first observation must be a full retrieve, got %s", first)
	}
	again, _ := s.ObserveSymbol(SymbolState{Path: "a.go", Symbol: "F", ContentHash: "h1"})
	if again != DeltaReuse {
		t.Fatalf("unchanged content must be reused, got %s", again)
	}
	changed, state := s.ObserveSymbol(SymbolState{Path: "a.go", Symbol: "F", ContentHash: "h2", ChangedRange: "L10-L20"})
	if changed != DeltaRange || state.ChangedRange != "L10-L20" {
		t.Fatalf("a known-range change must return a range delta, got %s %+v", changed, state)
	}
}

func TestStateHashIgnoresTimeAndTurn(t *testing.T) {
	s := NewSession("t1", ComputeBudget(BudgetProfile{}))
	s.SetObjective("fix the thing", PhaseFix)
	before := s.State().StateHash()

	s.NextTurn()
	s.NextTurn()
	if s.State().StateHash() != before {
		t.Fatal("advancing turns must not change the state hash")
	}

	s.TouchFiles("a.go")
	if s.State().StateHash() == before {
		t.Fatal("a meaningful change must change the state hash")
	}
}

func TestSnapshotNeededOnlyOnMeaningfulChange(t *testing.T) {
	s := NewSession("t1", ComputeBudget(BudgetProfile{}))
	needed, hash := s.SnapshotNeeded()
	if !needed {
		t.Fatal("a fresh session has no persisted snapshot yet")
	}
	s.MarkSnapshotted(hash)

	if needed, _ := s.SnapshotNeeded(); needed {
		t.Fatal("no state change must not require a new snapshot")
	}
	s.SetValidation("tests", "pass")
	if needed, _ := s.SnapshotNeeded(); !needed {
		t.Fatal("a validation change must require a snapshot")
	}
}

func TestRecordErrorDeduplicatesByFingerprint(t *testing.T) {
	s := NewSession("t1", ComputeBudget(BudgetProfile{}))
	sig := ErrorSignature{Code: "TS2345", File: "a.tsx", Message: "argument of type 'number'"}

	if _, first := s.RecordError(sig); !first {
		t.Fatal("first occurrence should report as new")
	}
	if _, first := s.RecordError(sig); first {
		t.Fatal("second occurrence must dedupe, not create a new entry")
	}
	if got := s.ErrorOccurrences(sig); got != 2 {
		t.Fatalf("expected count 2, got %d", got)
	}
	if len(s.State().ActiveErrors) != 1 {
		t.Fatalf("expected one active error, got %d", len(s.State().ActiveErrors))
	}
}

func TestFingerprintCollapsesNumericVariance(t *testing.T) {
	a := ErrorSignature{Code: "E1", File: "x.go", Message: "expected 3 got 4"}
	b := ErrorSignature{Code: "E1", File: "x.go", Message: "expected 7 got 9"}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("numeric variance in the same error must collapse to one fingerprint")
	}
}

func TestResolveErrorKeepsAOneLineRecord(t *testing.T) {
	s := NewSession("t1", ComputeBudget(BudgetProfile{}))
	sig := ErrorSignature{Code: "TS2345", File: "a.tsx"}
	s.RecordError(sig)

	if !s.ResolveError(sig, "narrowed the type") {
		t.Fatal("resolving a known error should succeed")
	}
	st := s.State()
	if len(st.ActiveErrors) != 0 {
		t.Fatal("resolved error must leave the active set")
	}
	if len(st.Resolved) != 1 {
		t.Fatalf("expected a one-line resolved record, got %v", st.Resolved)
	}
}

func TestPlanFollowsProgressiveLadder(t *testing.T) {
	p := NewPlanner()
	first := p.Plan(PlanRequest{Task: "fix ResourceTable"}, nil)
	if first.Level != LevelProjectMap.String() {
		t.Fatalf("a fresh task must start at the project map, got %s", first.Level)
	}
	if first.Source != "codebase" {
		t.Fatalf("structural retrieval must come from codebase, got %s", first.Source)
	}

	level := LevelProjectMap
	second := p.Plan(PlanRequest{Task: "fix ResourceTable", AchievedLevel: &level}, nil)
	if second.Level != LevelModuleMap.String() {
		t.Fatalf("expected one rung up, got %s", second.Level)
	}
}

func TestPlanStopsWhenConfidenceIsSufficient(t *testing.T) {
	p := NewPlanner()
	plan := p.Plan(PlanRequest{Task: "rename a local variable", Confidence: 0.9}, nil)
	if plan.Action != "act" {
		t.Fatalf("a confident agent must be told to act, got %s", plan.Action)
	}
	if len(plan.Code) != 0 {
		t.Fatalf("no further code retrieval should be suggested, got %v", plan.Code)
	}
}

func TestPlanTargetsOnlyMissingContext(t *testing.T) {
	p := NewPlanner()
	plan := p.Plan(PlanRequest{
		Task:           "fix delete flow",
		Confidence:     0.9,
		MissingContext: []string{"DeleteDialog", "delete confirmation tests"},
	}, nil)
	if plan.Action != "retrieve" {
		t.Fatal("declared missing context must override a high confidence score")
	}
	if plan.Level != LevelSymbol.String() {
		t.Fatalf("expected the symbol level, got %s", plan.Level)
	}
	if len(plan.Code) != 2 {
		t.Fatalf("expected exactly the missing items, got %v", plan.Code)
	}
}

func TestPlanExcludesRawAndOldSessionsByDefault(t *testing.T) {
	plan := NewPlanner().Plan(PlanRequest{Task: "anything"}, nil)
	want := map[string]bool{"raw_logs": true, "old_sessions": true, "stale_summaries": true, "resolved_errors": true}
	for _, e := range plan.Exclude {
		delete(want, e)
	}
	if len(want) != 0 {
		t.Fatalf("missing default exclusions: %v", want)
	}
}

func TestPlanCacheOrderIsCanonical(t *testing.T) {
	plan := NewPlanner().Plan(PlanRequest{Task: "anything"}, nil)
	want := CacheClasses()
	if len(plan.Cache.Order) != len(want) {
		t.Fatalf("expected %d cache classes, got %d", len(want), len(plan.Cache.Order))
	}
	for i := range want {
		if plan.Cache.Order[i] != want[i] {
			t.Fatalf("cache order position %d: want %s got %s", i, want[i], plan.Cache.Order[i])
		}
	}
}

func TestArtifactPhaseHasNoOperationalOutputCap(t *testing.T) {
	plan := NewPlanner().Plan(PlanRequest{Task: "write the migration guide", Phase: PhaseArtifact}, nil)
	if plan.Output.TargetTokens != 0 || !plan.Output.ArtifactException {
		t.Fatalf("the artifact exception must disable the operational cap, got %+v", plan.Output)
	}
}

func TestEvaluateStopHardAndSoftLimits(t *testing.T) {
	limits := StopLimits{MaxToolIterations: 5, MaxCostUSD: 1.0, SoftCostUSD: 0.5, MaxSameErrorRetries: 3}

	if d := EvaluateStop(limits, LoopObservation{ToolIterations: 2}); !d.Continue {
		t.Fatalf("well within limits should continue: %+v", d)
	}
	if d := EvaluateStop(limits, LoopObservation{ToolIterations: 5}); d.Continue {
		t.Fatal("hitting the iteration limit must stop the loop")
	}
	soft := EvaluateStop(limits, LoopObservation{CostUSD: 0.6})
	if !soft.Continue || !soft.SoftLimit || soft.Action != "reduce" {
		t.Fatalf("soft cost limit should keep going but reduce: %+v", soft)
	}
	escalate := EvaluateStop(limits, LoopObservation{MaxSameError: 3})
	if escalate.Continue || escalate.Action != "escalate" {
		t.Fatalf("a repeated error should stop and escalate: %+v", escalate)
	}
}

func TestParseHelpersAreForgiving(t *testing.T) {
	if ParseKind("Module-Summary") != KindModuleSummary {
		t.Fatal("hyphen/case variants must parse")
	}
	if ParseKind("nonsense") != KindUnknown {
		t.Fatal("unknown kinds must not error out")
	}
	if ParseCacheClass("nonsense") != CacheVolatile {
		t.Fatal("an unknown cache class must default to volatile so it stays out of the prefix")
	}
	if ParsePhase("documentation") != PhaseArtifact {
		t.Fatal("documentation must map to the artifact phase")
	}
	if ParseComplexity("") != ComplexityMedium {
		t.Fatal("empty complexity must default to medium")
	}
	if ParseRetrievalLevel("whole_file") != LevelFullFile {
		t.Fatal("alias parsing failed")
	}
}

func TestEstimateTokensHandlesDenseContent(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Fatal("empty content costs nothing")
	}
	// Many short whitespace-separated tokens must not be undercounted by the
	// 4-bytes-per-token heuristic.
	dense := "a b c d e f g h i j"
	if EstimateTokens(dense) < 10 {
		t.Fatalf("dense content undercounted: %d", EstimateTokens(dense))
	}
}

// A configured ceiling must beat the usability floor: an operator who caps the
// budget at 800 tokens gets 800, not the 2000-token minimum.
func TestConfiguredCeilingBeatsTheFloor(t *testing.T) {
	b := ComputeBudget(BudgetProfile{Complexity: ComplexityTrivial, MaxBudget: 800})
	if b.Total > 800 {
		t.Fatalf("floor overrode the ceiling: %d", b.Total)
	}
	if b.Reserve >= b.Total {
		t.Fatalf("reserve %d must leave something spendable out of %d", b.Reserve, b.Total)
	}
	b = ComputeBudget(BudgetProfile{Complexity: ComplexityTrivial, ModelContextWindow: 500})
	if b.Total > 500 {
		t.Fatalf("floor overrode the model window: %d", b.Total)
	}
}
