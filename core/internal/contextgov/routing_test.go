package contextgov

import (
	"strings"
	"testing"
)

func TestComplexityGrowsWithScope(t *testing.T) {
	tiny := ComplexityScore(Signals{Files: 1, DiffLines: 5})
	medium := ComplexityScore(Signals{Files: 5, Modules: 2, DiffLines: 200})
	large := ComplexityScore(Signals{Files: 20, Modules: 4, Languages: 2, DiffLines: 900})

	if !(tiny.Value < medium.Value && medium.Value < large.Value) {
		t.Fatalf("complexity must grow with scope: %.2f %.2f %.2f",
			tiny.Value, medium.Value, large.Value)
	}
	if large.Value > 1 {
		t.Fatalf("scores are normalized to 0..1, got %.2f", large.Value)
	}
	if len(large.Reasons) == 0 {
		t.Fatal("a score without reasons is not reviewable")
	}
}

// A one-line change to an auth check is trivial to write and expensive to get
// wrong. Routing by size alone would send it to the cheapest model.
func TestRiskIsIndependentOfSize(t *testing.T) {
	tiny := Signals{Files: 1, DiffLines: 3, TouchesSecurity: true}
	complexity := ComplexityScore(tiny)
	risk := RiskScore(tiny)

	if complexity.Value > 0.15 {
		t.Fatalf("a one-line change is not complex: %.2f", complexity.Value)
	}
	if risk.Value < 0.4 {
		t.Fatalf("a security change is high risk regardless of size: %.2f", risk.Value)
	}

	d := Route(tiny, DefaultRoutingConfig())
	if tierRank(d.Tier) < tierRank(TierFrontier) {
		t.Fatalf("a security-sensitive change must not route cheap, got %s", d.Tier)
	}
}

func TestTestCoverageLowersRisk(t *testing.T) {
	base := Signals{Destructive: true, TouchesInfrastructure: true}
	covered := base
	covered.HasTests = true

	if RiskScore(covered).Value >= RiskScore(base).Value {
		t.Fatal("test coverage must lower risk: a mistake is likely to be caught")
	}
	if !strings.Contains(strings.Join(RiskScore(covered).Reasons, " "), "tests") {
		t.Fatal("the reason must be stated")
	}
}

func TestRouteTiersByComplexity(t *testing.T) {
	cfg := DefaultRoutingConfig()

	trivial := Route(Signals{Files: 1, DiffLines: 2, Phase: PhaseFix}, cfg)
	if trivial.Tier != TierCheap {
		t.Fatalf("a one-file change should route cheap, got %s", trivial.Tier)
	}
	if trivial.Classification != ComplexityTrivial && trivial.Classification != ComplexitySimple {
		t.Fatalf("unexpected classification %s", trivial.Classification)
	}

	medium := Route(Signals{Files: 5, Modules: 2, DiffLines: 200, Phase: PhaseFix}, cfg)
	if tierRank(medium.Tier) < tierRank(TierMid) {
		t.Fatalf("a multi-module change should not route cheap, got %s", medium.Tier)
	}

	big := Route(Signals{Files: 20, Modules: 4, Languages: 2, DiffLines: 900,
		TouchesArchitecture: true, Phase: PhasePlan}, cfg)
	if tierRank(big.Tier) < tierRank(TierFrontier) {
		t.Fatalf("a cross-module architectural refactor needs a strong model, got %s", big.Tier)
	}
}

func TestCriticalRiskRoutesPremium(t *testing.T) {
	d := Route(Signals{TouchesSecurity: true, Destructive: true}, DefaultRoutingConfig())
	if d.Tier != TierPremium {
		t.Fatalf("security plus destructive is critical, got %s", d.Tier)
	}
	if d.Classification != ComplexityCritical {
		t.Fatalf("expected the critical classification, got %s", d.Classification)
	}
}

func TestEscalationRequiresEvidence(t *testing.T) {
	cfg := DefaultRoutingConfig()

	// No history: no escalation, however hard the caller says it feels.
	calm := Route(Signals{Files: 1, Confidence: 0.1}, cfg)
	if calm.Escalate {
		t.Fatal("low self-reported confidence is not evidence of failure")
	}

	repeated := Route(Signals{Files: 1, RepeatedError: 3}, cfg)
	if !repeated.Escalate || repeated.Tier != TierPremium {
		t.Fatalf("a repeating error is evidence and must escalate: %+v", repeated)
	}
	failed := Route(Signals{Files: 1, PriorFailures: 2}, cfg)
	if !failed.Escalate || tierRank(failed.Tier) < tierRank(TierFrontier) {
		t.Fatalf("repeated failures must escalate: %+v", failed)
	}

	cfg.EscalationEnabled = false
	if Route(Signals{Files: 1, RepeatedError: 5}, cfg).Escalate {
		t.Fatal("escalation must be disableable")
	}
}

// An auxiliary classifier only pays for itself when the deterministic signals
// genuinely do not decide. Anything else already has an answer.
func TestCheapClassifierOnlyWhenSignalsAreAmbiguous(t *testing.T) {
	cfg := DefaultRoutingConfig()

	blind := Route(Signals{}, cfg)
	if !blind.UseCheapClassifier {
		t.Fatalf("with no signals at all a cheap classifier may help: %+v", blind)
	}

	for name, s := range map[string]Signals{
		"file count": {Files: 3},
		"risk flag":  {TouchesSecurity: true},
		"history":    {PriorFailures: 1},
		"confidence": {Confidence: 0.9},
	} {
		if Route(s, cfg).UseCheapClassifier {
			t.Fatalf("%s already decides; no classifier call is justified", name)
		}
	}

	cfg.CheapClassifierWhenROIPositive = false
	if Route(Signals{}, cfg).UseCheapClassifier {
		t.Fatal("the classifier must be disableable")
	}
}

func TestUnknownTaskRoutesToTheSafeMiddle(t *testing.T) {
	d := Route(Signals{}, DefaultRoutingConfig())
	// Guessing "cheap" for an unknown task risks a failure that costs more than
	// the saving; the router must not do that.
	if d.Tier == TierCheap || d.Tier == TierLocal {
		t.Fatalf("an unknown task must not route to the cheapest tier, got %s", d.Tier)
	}
}

func TestLocalTierOnlyForClassificationWithNoSignals(t *testing.T) {
	d := Route(Signals{Phase: PhaseClassify}, DefaultRoutingConfig())
	if d.Tier != TierLocal {
		t.Fatalf("a signal-free classification step needs no model, got %s", d.Tier)
	}
	// Any risk at all must take it off the local path.
	withRisk := Route(Signals{Phase: PhaseClassify, Destructive: true}, DefaultRoutingConfig())
	if withRisk.Tier == TierLocal {
		t.Fatal("a risky task must never be handled without a model")
	}
}

func TestMaxTierCapsRouting(t *testing.T) {
	cfg := DefaultRoutingConfig()
	cfg.MaxTier = TierMid

	d := Route(Signals{TouchesSecurity: true, Destructive: true}, cfg)
	if d.Tier != TierMid {
		t.Fatalf("the configured cap must win, got %s", d.Tier)
	}
	if !strings.Contains(d.Rationale, "capped") {
		t.Fatalf("the cap must be visible in the rationale: %q", d.Rationale)
	}
}

func TestDisabledRoutingIsNeutral(t *testing.T) {
	cfg := DefaultRoutingConfig()
	cfg.Enabled = false
	d := Route(Signals{TouchesSecurity: true}, cfg)
	if d.Tier != TierMid {
		t.Fatalf("disabled routing must be neutral, got %s", d.Tier)
	}
	if d.Rationale == "" {
		t.Fatal("even a neutral decision must explain itself")
	}
}

func TestRiskBasedCanBeDisabled(t *testing.T) {
	cfg := DefaultRoutingConfig()
	cfg.RiskBased = false
	d := Route(Signals{Files: 1, DiffLines: 2, TouchesSecurity: true}, cfg)
	if tierRank(d.Tier) > tierRank(TierCheap) {
		t.Fatalf("with risk-based routing off, size alone decides: %s", d.Tier)
	}
}

func TestRationaleAlwaysExplainsTheDecision(t *testing.T) {
	d := Route(Signals{Files: 6, TouchesSecurity: true, HasTests: true}, DefaultRoutingConfig())
	for _, want := range []string{"complexity", "risk", "security-sensitive", "test coverage"} {
		if !strings.Contains(d.Rationale, want) {
			t.Fatalf("rationale should mention %q: %q", want, d.Rationale)
		}
	}
}

func TestAdaptiveBudgetScalesWithTier(t *testing.T) {
	limits := StopLimits{MaxCostUSD: 1.0, SoftCostUSD: 0.7, MaxTokenBudget: 100000}

	cheap := AdaptiveBudget(limits, RouteDecision{Tier: TierCheap})
	if cheap.MaxCostUSD >= limits.MaxCostUSD {
		t.Fatalf("a cheap task should get a smaller allowance: %f", cheap.MaxCostUSD)
	}
	if cheap.MaxTokenBudget >= limits.MaxTokenBudget {
		t.Fatalf("a cheap task should get a smaller token budget: %d", cheap.MaxTokenBudget)
	}

	// A hard limit is the operator's ceiling and must never be raised.
	premium := AdaptiveBudget(limits, RouteDecision{Tier: TierPremium})
	if premium.MaxCostUSD > limits.MaxCostUSD {
		t.Fatalf("the hard cost limit was raised to %f", premium.MaxCostUSD)
	}
	if premium.MaxTokenBudget > limits.MaxTokenBudget {
		t.Fatalf("the hard token limit was raised to %d", premium.MaxTokenBudget)
	}
}

func TestAdaptiveBudgetKeepsSoftLimitBelowHard(t *testing.T) {
	limits := StopLimits{MaxCostUSD: 0.10, SoftCostUSD: 0.09}
	out := AdaptiveBudget(limits, RouteDecision{Tier: TierPremium})
	if out.SoftCostUSD >= out.MaxCostUSD {
		t.Fatalf("a soft limit at or above the hard limit would never fire: %f >= %f",
			out.SoftCostUSD, out.MaxCostUSD)
	}
}

func TestAdaptiveBudgetLeavesUnsetLimitsAlone(t *testing.T) {
	out := AdaptiveBudget(StopLimits{}, RouteDecision{Tier: TierPremium})
	if out.MaxCostUSD != 0 || out.SoftCostUSD != 0 || out.MaxTokenBudget != 0 {
		t.Fatalf("an unset limit must stay unset, not acquire a value: %+v", out)
	}
}

func TestParseTierIsForgiving(t *testing.T) {
	cases := map[string]Tier{
		"premium": TierPremium, "PREMIUM": TierPremium,
		"mini": TierCheap, "flagship": TierFrontier,
		"deterministic": TierLocal, "reasoning": TierPremium,
		"": TierMid, "nonsense": TierMid,
	}
	for in, want := range cases {
		if got := ParseTier(in); got != want {
			t.Fatalf("ParseTier(%q) = %s, want %s", in, got, want)
		}
	}
}
