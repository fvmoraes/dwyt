package governor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/contextgov"
	"github.com/fvmoraes/dwyt/internal/provider"
)

var jsonMarshal = json.Marshal

func newGovernor(t *testing.T) *Governor {
	t.Helper()
	return New(DefaultConfig(), t.TempDir())
}

func TestContextPlanReturnsBudgetAndBoundaries(t *testing.T) {
	g := newGovernor(t)
	resp := g.ContextPlan(PlanRequest{TaskID: "t1", Task: "fix the delete flow"})

	if resp.TaskID != "t1" || resp.PolicyVersion != PolicyVersion {
		t.Fatalf("unexpected envelope: %+v", resp)
	}
	if resp.Plan.Budget.Total <= 0 || resp.Plan.Budget.Reserve <= 0 {
		t.Fatalf("plan must carry a usable budget: %+v", resp.Plan.Budget)
	}
	if len(resp.Plan.Memory) == 0 {
		t.Fatal("plan must name the canonical memory to load")
	}
	if len(resp.Plan.Exclude) == 0 {
		t.Fatal("plan must state what stays out of context")
	}
	if !resp.Plan.Cache.PreservePrefix {
		t.Fatal("prefix preservation is on by default")
	}
	if !resp.Stop.Continue {
		t.Fatalf("a fresh task must be allowed to proceed: %+v", resp.Stop)
	}
}

func TestContextPlanIsCompact(t *testing.T) {
	g := newGovernor(t)
	resp := g.ContextPlan(PlanRequest{Task: "refactor the resource table", Complexity: "complex"})

	// The governor must not become another source of waste (spec §3.1). The
	// whole plan, serialized, has to stay far below a single operational answer.
	size := len(mustJSON(t, resp))
	if size > 2500 {
		t.Fatalf("plan response grew to %d bytes; it must stay compact:\n%s", size, mustJSON(t, resp))
	}
	// And it must not echo the policy text back.
	if strings.Contains(mustJSON(t, resp), "DWYT TOKEN EFFICIENCY POLICY") {
		t.Fatal("the governor must not re-send the policy on every call")
	}
}

func TestContextPlanHonoursDisabledConfidenceGate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfidenceGated = false
	g := New(cfg, t.TempDir())

	resp := g.ContextPlan(PlanRequest{Task: "anything", Confidence: 0.99})
	if resp.Plan.Action != "retrieve" {
		t.Fatalf("with gating disabled a self-reported confidence must not short-circuit: %s", resp.Plan.Action)
	}
}

func TestContextPlanRespectsTightenedOperationalTarget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OperationalOutputTarget = 120
	g := New(cfg, t.TempDir())

	resp := g.ContextPlan(PlanRequest{Task: "small fix", Phase: "fix"})
	if resp.Plan.Output.TargetTokens > 120 {
		t.Fatalf("configured target should tighten the phase default, got %d", resp.Plan.Output.TargetTokens)
	}
	// It must not *loosen* a phase that is already tighter.
	classify := g.ContextPlan(PlanRequest{Task: "classify", Phase: "classify"})
	if classify.Plan.Output.TargetTokens > 120 {
		t.Fatalf("classify should stay at or below its own tighter default, got %d",
			classify.Plan.Output.TargetTokens)
	}
}

func TestContextPlanPreservesAnExpandedBudgetAcrossTurns(t *testing.T) {
	g := newGovernor(t)
	first := g.ContextPlan(PlanRequest{TaskID: "t1", Task: "work"})

	sess, ok := g.Session("t1")
	if !ok {
		t.Fatal("expected a session to exist")
	}
	sess.ExpandBudget(10000)

	second := g.ContextPlan(PlanRequest{TaskID: "t1", Task: "work"})
	if second.Plan.Budget.Total <= first.Plan.Budget.Total {
		t.Fatalf("a later plan must not shrink an already-granted budget: %d -> %d",
			first.Plan.Budget.Total, second.Plan.Budget.Total)
	}
}

func TestRegisterContextKeepsDropsAndReuses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ProgressiveExpansion = false // isolate the selection decision
	g := New(cfg, t.TempDir())
	g.ContextPlan(PlanRequest{TaskID: "t1", Task: "fix", Complexity: "trivial", Phase: "classify"})

	budget := g.ContextStatus("t1").Budget
	resp := g.RegisterContext(RegisterRequest{
		TaskID: "t1",
		Items: []RegisterItem{
			{ID: "constraint", Kind: "constraint", Tokens: 100, ContentHash: "h1", Pinned: true},
			{ID: "huge-log", Kind: "raw_log", Tokens: budget.Total * 4, ContentHash: "h2"},
			{ID: "symbol", Kind: "symbol", Tokens: 50, ContentHash: "h3"},
		},
	})

	decisions := map[string]string{}
	for _, d := range resp.Decisions {
		decisions[d.ID] = d.Decision
	}
	if decisions["constraint"] != "keep" {
		t.Fatalf("pinned constraint must be kept: %v", decisions)
	}
	if decisions["huge-log"] != "drop" {
		t.Fatalf("an oversized raw log must be dropped: %v", decisions)
	}
	if decisions["symbol"] != "keep" {
		t.Fatalf("a small relevant symbol should fit: %v", decisions)
	}
	if len(resp.Order) == 0 {
		t.Fatal("kept items must come back in a cache-safe assembly order")
	}
}

func TestRegisterContextReusesUnchangedSymbols(t *testing.T) {
	g := newGovernor(t)
	g.ContextPlan(PlanRequest{TaskID: "t1", Task: "fix"})

	item := RegisterItem{
		ID: "rt", Kind: "symbol", Path: "src/ResourceTable.tsx", Symbol: "ResourceTable",
		Tokens: 400, ContentHash: "sha256:v1",
	}
	first := g.RegisterContext(RegisterRequest{TaskID: "t1", Items: []RegisterItem{item}})
	if first.Decisions[0].Decision != "keep" {
		t.Fatalf("first sighting must be kept: %+v", first.Decisions)
	}

	second := g.RegisterContext(RegisterRequest{TaskID: "t1", Items: []RegisterItem{item}})
	if second.Decisions[0].Decision != "reuse" {
		t.Fatalf("unchanged content must be reused, not resent: %+v", second.Decisions)
	}
	if second.TokensReused != 400 || second.TokensKept != 0 {
		t.Fatalf("reuse must cost no budget: %+v", second)
	}
}

func TestRegisterContextHashesContentWhenNotSupplied(t *testing.T) {
	g := newGovernor(t)
	resp := g.RegisterContext(RegisterRequest{
		TaskID: "t1",
		Items:  []RegisterItem{{ID: "a", Kind: "file_summary", Content: "package main\n"}},
	})
	if len(resp.Decisions) != 1 || resp.Decisions[0].Tokens == 0 {
		t.Fatalf("tokens should be estimated from the content: %+v", resp.Decisions)
	}
}

func TestRegisterContextGrantsProgressiveExpansion(t *testing.T) {
	g := newGovernor(t)
	g.ContextPlan(PlanRequest{TaskID: "t1", Task: "big refactor", Complexity: "trivial", Phase: "classify"})
	budget := g.ContextStatus("t1").Budget

	resp := g.RegisterContext(RegisterRequest{
		TaskID: "t1",
		Items: []RegisterItem{
			{ID: "a", Kind: "symbol", Tokens: budget.Total, ContentHash: "h1"},
			{ID: "b", Kind: "symbol", Tokens: budget.Total, ContentHash: "h2"},
		},
	})
	expanded := false
	for _, d := range resp.Decisions {
		if d.Decision == "budget_expanded" && d.Tokens > 0 {
			expanded = true
		}
	}
	if !expanded {
		t.Fatalf("dropping high-ROI context should trigger progressive expansion: %+v", resp)
	}
	if resp.Budget.Total <= budget.Total {
		t.Fatalf("the reported budget should reflect the expansion: %d -> %d", budget.Total, resp.Budget.Total)
	}
}

func TestContextStatusForUnknownTaskIsSafe(t *testing.T) {
	g := newGovernor(t)
	st := g.ContextStatus("never-seen")
	if st.Budget.Total <= 0 {
		t.Fatal("an unknown task must still get a usable default budget")
	}
	if !st.Stop.Continue {
		t.Fatal("an unknown task must not be reported as stopped")
	}
}

func TestCompactToolOutputArchivesRawAndReturnsRef(t *testing.T) {
	g := newGovernor(t)
	var b strings.Builder
	for i := 0; i < 300; i++ {
		b.WriteString("[INFO] compiling something\n")
	}
	raw := b.String()

	compacted, err := g.CompactToolOutput(CompactRequest{TaskID: "t1", Content: raw, Kind: "build"})
	if err != nil {
		t.Fatal(err)
	}
	if compacted.RawRef == "" {
		t.Fatal("a compaction must carry a raw reference so no evidence is lost")
	}
	if compacted.SentTokensEst >= compacted.RawTokensEst {
		t.Fatalf("expected real reduction: %d -> %d", compacted.RawTokensEst, compacted.SentTokensEst)
	}

	back, _, err := g.GetRaw(compacted.RawRef)
	if err != nil {
		t.Fatal(err)
	}
	if back != raw {
		t.Fatal("the raw bytes must round-trip exactly")
	}
}

func TestCompactToolOutputResolvesAnExistingRawRef(t *testing.T) {
	g := newGovernor(t)
	meta, err := g.PutRaw("src/a.ts(1,1): error TS1: boom\n", "tool_output", "tsc")
	if err != nil {
		t.Fatal(err)
	}
	compacted, err := g.CompactToolOutput(CompactRequest{RawRef: meta.Ref()})
	if err != nil {
		t.Fatal(err)
	}
	if len(compacted.Errors) != 1 {
		t.Fatalf("expected the diagnostic to be extracted: %+v", compacted)
	}
}

func TestCompactToolOutputRequiresInput(t *testing.T) {
	if _, err := newGovernor(t).CompactToolOutput(CompactRequest{}); err == nil {
		t.Fatal("compaction without content or a reference must be rejected")
	}
}

func TestRawStoreDisabledDegradesGracefully(t *testing.T) {
	g := New(DefaultConfig(), "")
	if g.RawStore() != nil {
		t.Fatal("an empty home must leave the raw store disabled")
	}
	// Compaction must still work; only the archive is unavailable.
	compacted, err := g.CompactToolOutput(CompactRequest{Content: "src/a.ts(1,1): error TS1: boom"})
	if err != nil {
		t.Fatalf("compaction must not fail because archiving is unavailable: %v", err)
	}
	if compacted.RawRef != "" {
		t.Fatal("no reference can be reported when nothing was archived")
	}
	if _, _, err := g.GetRaw("dwyt://objects/abc"); err == nil {
		t.Fatal("resolving a reference without a store must report the problem")
	}
	if enabled, _ := g.RawUsage()["enabled"].(bool); enabled {
		t.Fatal("RawUsage must report the store as disabled")
	}
}

func TestReportUsageUpdatesObservedCacheOnly(t *testing.T) {
	g := newGovernor(t)
	g.ContextPlan(PlanRequest{TaskID: "t1", Task: "work"})

	g.ReportUsage(Usage{TaskID: "t1", CachedHashes: []string{"h1"}, Observed: false})
	sess, _ := g.Session("t1")
	if len(sess.CachedHashes()) != 0 {
		t.Fatal("an unobserved report must never populate the cache set")
	}

	g.ReportUsage(Usage{TaskID: "t1", CachedHashes: []string{"h1"}, Observed: true})
	if !sess.CachedHashes()["h1"] {
		t.Fatal("an observed cache read must be recorded")
	}
}

func TestReportUsageAccumulatesLoopStateAndStops(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StopLimits.MaxToolIterations = 3
	g := New(cfg, t.TempDir())

	var last UsageResponse
	for i := 0; i < 3; i++ {
		last = g.ReportUsage(Usage{TaskID: "t1", ToolIterations: 1})
	}
	if last.Stop.Continue {
		t.Fatalf("the iteration limit should have stopped the loop: %+v", last.Stop)
	}
	if !last.Accepted {
		t.Fatal("a usage report is always accepted, even when it triggers a stop")
	}
	if last.Recorded {
		t.Fatal("without a telemetry sink nothing is recorded")
	}
	if last.Note == "" {
		t.Fatal("the absence of a telemetry sink must be stated, not hidden")
	}
}

type failingRecorder struct{ calls int }

func (f *failingRecorder) RecordUsage(Usage) error {
	f.calls++
	return errTest
}

var errTest = &testError{"disk full"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestReportUsageSurvivesTelemetryFailure(t *testing.T) {
	g := newGovernor(t)
	rec := &failingRecorder{}
	g.SetUsageRecorder(rec)

	resp := g.ReportUsage(Usage{TaskID: "t1", ToolIterations: 1})
	if !resp.Accepted {
		t.Fatal("telemetry is observability; its failure must not reject the report")
	}
	if resp.Recorded {
		t.Fatal("a failed write must not be reported as recorded")
	}
	if !strings.Contains(resp.Note, "disk full") {
		t.Fatalf("the failure must be surfaced: %q", resp.Note)
	}
	if rec.calls != 1 {
		t.Fatalf("expected exactly one write attempt, got %d", rec.calls)
	}
}

func TestCacheGuidanceIsHonestAboutCapability(t *testing.T) {
	g := newGovernor(t)
	guidance := g.CacheGuidance("openai", "some-model")

	if guidance.CapabilityState != CapabilityAdvised {
		t.Fatalf("DWYT does not control third-party transports; state must be advised, got %s",
			guidance.CapabilityState)
	}
	if guidance.CapabilityState == CapabilityEnforced {
		t.Fatal("claiming enforcement without controlling the request is forbidden")
	}
	want := contextgov.CacheClasses()
	if len(guidance.Order) != len(want) {
		t.Fatalf("expected the canonical order, got %v", guidance.Order)
	}
	joined := strings.Join(guidance.NeverBeforePrefix, ",")
	for _, forbidden := range []string{"timestamps", "request_ids", "nonces"} {
		if !strings.Contains(joined, forbidden) {
			t.Fatalf("guidance must forbid %s before the prefix: %v", forbidden, guidance.NeverBeforePrefix)
		}
	}
	if len(guidance.TrimOrder) == 0 || guidance.TrimOrder[0] != "discardable" {
		t.Fatalf("the trim order must start with discardable noise: %v", guidance.TrimOrder)
	}
	if guidance.TrimOrder[len(guidance.TrimOrder)-1] != "stable_last_resort" {
		t.Fatalf("stable cacheable content must be the last casualty: %v", guidance.TrimOrder)
	}
}

func TestUnwiredProvidersReportUnavailableInsteadOfFailing(t *testing.T) {
	g := newGovernor(t)
	if available, _ := g.MemoryHealth()["available"].(bool); available {
		t.Fatal("an unwired brain must report unavailable")
	}
	if enabled, _ := g.HousekeeperStatus()["enabled"].(bool); enabled {
		t.Fatal("an unwired housekeeper must report disabled")
	}
}

func TestSessionsAreBounded(t *testing.T) {
	g := newGovernor(t)
	for i := 0; i < maxSessions+10; i++ {
		g.ContextPlan(PlanRequest{TaskID: taskName(i), Task: "work"})
	}
	g.mu.RLock()
	count := len(g.sessions)
	g.mu.RUnlock()
	if count > maxSessions {
		t.Fatalf("session map is unbounded: %d entries", count)
	}
	// The most recent task must still be present.
	if _, ok := g.Session(taskName(maxSessions + 9)); !ok {
		t.Fatal("eviction removed the newest session")
	}
}

func taskName(i int) string {
	return "task-" + string(rune('a'+i%26)) + "-" + itoa(i)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf []byte
	for v > 0 {
		buf = append([]byte{byte('0' + v%10)}, buf...)
		v /= 10
	}
	return string(buf)
}

func TestOutputProfileAppliesConfiguredCeiling(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OperationalOutputMax = 200
	g := New(cfg, t.TempDir())

	if p := g.OutputProfile("fix", ""); p.MaxTokens > 200 {
		t.Fatalf("configured ceiling ignored: %+v", p)
	}
	// The artifact exception is never capped, whatever the config says.
	if p := g.OutputProfile("document", ""); p.MaxTokens != 0 || !p.ArtifactException {
		t.Fatalf("the artifact exception must survive a tightened config: %+v", p)
	}
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := jsonMarshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCacheGuidanceIsCapabilityDriven(t *testing.T) {
	g := newGovernor(t)

	anthropic := g.CacheGuidance("anthropic", "claude-x")
	if !anthropic.Capabilities.ExplicitBreakpoints {
		t.Fatalf("expected explicit breakpoints for anthropic: %+v", anthropic.Capabilities)
	}
	if !strings.Contains(anthropic.Note, "breakpoints") {
		t.Fatalf("the note should mention the capability: %q", anthropic.Note)
	}

	unknown := g.CacheGuidance("some-new-vendor", "their-model")
	if unknown.CapabilityState != CapabilityUnknown {
		t.Fatalf("an unknown provider must report unknown, got %s", unknown.CapabilityState)
	}
	if !strings.Contains(unknown.Note, "unknown") {
		t.Fatalf("the note should say the support is unknown: %q", unknown.Note)
	}
	// Structural guidance applies regardless.
	if len(unknown.Order) == 0 || len(unknown.TrimOrder) == 0 {
		t.Fatal("ordering guidance is provider-independent and must always be present")
	}
	if unknown.CatalogVersion == "" {
		t.Fatal("the catalog version must be reported so a decision is traceable")
	}
}

func TestCacheGuidanceBecomesObservedAfterAReport(t *testing.T) {
	g := newGovernor(t)
	if g.CacheGuidance("some-vendor", "m1").CapabilityState == CapabilityObserved {
		t.Fatal("nothing has been observed yet")
	}

	cached := 1200
	g.ReportUsage(Usage{
		TaskID: "t1", Provider: "some-vendor", Model: "m1",
		CachedInputTokens: &cached, Observed: true,
	})

	if got := g.CacheGuidance("some-vendor", "m1").CapabilityState; got != CapabilityObserved {
		t.Fatalf("an observed cache report should upgrade the state, got %s", got)
	}
}

func TestUnobservedReportTeachesNothingAboutTheProvider(t *testing.T) {
	g := newGovernor(t)
	cached := 1200
	g.ReportUsage(Usage{
		TaskID: "t1", Provider: "some-vendor", Model: "m1",
		CachedInputTokens: &cached, Observed: false,
	})
	if got := g.CacheGuidance("some-vendor", "m1").CapabilityState; got == CapabilityObserved {
		t.Fatal("an estimate must never be recorded as an observation")
	}
}

func TestReportUsageFillsInAnEstimateWhenPricingIsKnown(t *testing.T) {
	g := newGovernor(t)
	g.Pricing().Register(provider.Pricing{
		Provider: "openai", Model: "gpt-x", InputPerMTok: 2, OutputPerMTok: 8,
	})

	input, output := 1_000_000, 100_000
	g.ReportUsage(Usage{
		TaskID: "t1", Provider: "openai", Model: "gpt-x",
		InputTokens: &input, OutputTokens: &output, Observed: true,
	})

	// The cost must have been folded into the loop state, which the stop
	// conditions read.
	g.mu.RLock()
	obs := g.loop["t1"]
	g.mu.RUnlock()
	if obs == nil || obs.CostUSD <= 0 {
		t.Fatalf("an estimable cost should be accumulated: %+v", obs)
	}
}

func TestReportUsageLeavesCostUnknownWithoutPricing(t *testing.T) {
	g := newGovernor(t)
	input, output := 1_000_000, 100_000
	g.ReportUsage(Usage{
		TaskID: "t1", Provider: "unpriced-vendor",
		InputTokens: &input, OutputTokens: &output, Observed: true,
	})
	g.mu.RLock()
	obs := g.loop["t1"]
	g.mu.RUnlock()
	if obs != nil && obs.CostUSD != 0 {
		t.Fatalf("an unknown cost must not be invented: %f", obs.CostUSD)
	}
}
