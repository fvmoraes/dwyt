package contextopt

import (
	"fmt"
	"strings"
)

// Deterministic routing (spec §46, §50).
//
// The order the spec mandates is strict: deterministic classification first, a
// cheap auxiliary model only when its ROI is positive, and a frontier model only
// when complexity or risk demands it. Spending an LLM call to decide how to spend
// an LLM call is a cost, not an optimization — so everything here is arithmetic
// over signals the caller already has.
//
// This package deliberately does not name concrete models. Model names change
// monthly; the tier is the stable concept, and mapping a tier to a model is the
// configuration layer's job (spec §46: "the concrete model names must not be a
// fixed rule of the core").

// Tier is the capability class a task should be routed to.
type Tier string

const (
	// TierLocal is deterministic handling with no model call at all.
	TierLocal Tier = "local"
	// TierCheap is an inexpensive fast model.
	TierCheap Tier = "cheap"
	// TierMid is a mid-tier model.
	TierMid Tier = "mid"
	// TierFrontier is a frontier model.
	TierFrontier Tier = "frontier"
	// TierPremium is premium reasoning, for critical review.
	TierPremium Tier = "premium"
)

// Signals are the measurable properties of a task (spec §46).
//
// Every field is optional. The zero value describes a task DWYT knows nothing
// about, which routes to TierMid — the safe middle, since guessing "cheap" for an
// unknown task risks a failure that costs more than the saving.
type Signals struct {
	// Files, Modules and Languages are counts of what the task touches.
	Files     int `json:"files,omitempty"`
	Modules   int `json:"modules,omitempty"`
	Languages int `json:"languages,omitempty"`
	// DiffLines is the size of the change, when known.
	DiffLines int `json:"diff_lines,omitempty"`

	// TouchesArchitecture marks a change to structure rather than behaviour.
	TouchesArchitecture bool `json:"touches_architecture,omitempty"`
	// TouchesSecurity marks auth, crypto, secrets or access control.
	TouchesSecurity bool `json:"touches_security,omitempty"`
	// TouchesInfrastructure marks deployment, migrations or infrastructure code.
	TouchesInfrastructure bool `json:"touches_infrastructure,omitempty"`
	// Destructive marks a change that deletes or overwrites data.
	Destructive bool `json:"destructive,omitempty"`
	// HasTests reports whether the affected area is covered by tests, which
	// lowers risk because a mistake is likely to be caught.
	HasTests bool `json:"has_tests,omitempty"`

	// PriorFailures is how many times this task already failed.
	PriorFailures int `json:"prior_failures,omitempty"`
	// Attempts is how many attempts have been made.
	Attempts int `json:"attempts,omitempty"`
	// RepeatedError is the highest occurrence count of a single error.
	RepeatedError int `json:"repeated_error,omitempty"`

	// Confidence is the agent's self-reported confidence, 0..1.
	Confidence float64 `json:"confidence,omitempty"`

	// Phase lets a routing decision account for what the model is being asked
	// to do, not only how hard the code is.
	Phase Phase `json:"phase,omitempty"`
}

// RouteScore is a 0..1 measurement with the reasons behind it. Exposing the
// reasons is what makes a routing decision reviewable rather than magic.
//
// Named RouteScore rather than Score because Score is already the Token ROI
// function in this package; a routing score and a candidate score are different
// concepts and must not share a name.
type RouteScore struct {
	Value   float64  `json:"value"`
	Reasons []string `json:"reasons,omitempty"`
}

// ComplexityScore measures how much work the task is.
func ComplexityScore(s Signals) RouteScore {
	out := RouteScore{}
	add := func(weight float64, reason string) {
		out.Value += weight
		out.Reasons = append(out.Reasons, reason)
	}

	switch {
	case s.Files >= 10:
		add(0.30, fmt.Sprintf("%d files", s.Files))
	case s.Files >= 4:
		add(0.18, fmt.Sprintf("%d files", s.Files))
	case s.Files >= 2:
		add(0.08, fmt.Sprintf("%d files", s.Files))
	}
	switch {
	case s.Modules >= 3:
		add(0.20, fmt.Sprintf("%d modules", s.Modules))
	case s.Modules == 2:
		add(0.10, "2 modules")
	}
	if s.Languages >= 2 {
		add(0.12, fmt.Sprintf("%d languages", s.Languages))
	}
	switch {
	case s.DiffLines >= 500:
		add(0.20, fmt.Sprintf("%d diff lines", s.DiffLines))
	case s.DiffLines >= 150:
		add(0.12, fmt.Sprintf("%d diff lines", s.DiffLines))
	case s.DiffLines >= 40:
		add(0.05, fmt.Sprintf("%d diff lines", s.DiffLines))
	}
	if s.TouchesArchitecture {
		add(0.18, "touches architecture")
	}
	// A task that already failed is empirically harder than its size suggests.
	if s.PriorFailures > 0 {
		weight := 0.10 * float64(s.PriorFailures)
		if weight > 0.25 {
			weight = 0.25
		}
		add(weight, fmt.Sprintf("%d prior failure(s)", s.PriorFailures))
	}
	// Low self-reported confidence is a real signal, but a weak one: an agent
	// that under-reports must not be able to force an expensive model.
	if s.Confidence > 0 && s.Confidence < 0.4 {
		add(0.08, "low reported confidence")
	}
	if s.Phase == PhasePlan || s.Phase == PhaseReview {
		add(0.10, "planning/review phase")
	}

	out.Value = clamp01(out.Value)
	return out
}

// RiskScore measures how expensive a mistake would be.
//
// Risk and complexity are separate on purpose: a one-line change to an auth
// check is trivial to write and expensive to get wrong, and routing it by size
// alone would send it to the cheapest model.
func RiskScore(s Signals) RouteScore {
	out := RouteScore{}
	add := func(weight float64, reason string) {
		out.Value += weight
		out.Reasons = append(out.Reasons, reason)
	}

	if s.TouchesSecurity {
		add(0.45, "security-sensitive")
	}
	if s.Destructive {
		add(0.35, "destructive operation")
	}
	if s.TouchesInfrastructure {
		add(0.25, "infrastructure change")
	}
	if s.TouchesArchitecture {
		add(0.15, "architectural change")
	}
	if s.Modules >= 3 {
		add(0.10, "wide blast radius")
	}
	if s.RepeatedError >= 3 {
		add(0.15, "same error repeating")
	}
	// Test coverage genuinely lowers risk: a mistake is likely to be caught
	// before it reaches anyone.
	if s.HasTests {
		out.Value -= 0.15
		out.Reasons = append(out.Reasons, "covered by tests")
	}

	out.Value = clamp01(out.Value)
	return out
}

// RouteDecision is the routing outcome.
type RouteDecision struct {
	Tier       Tier       `json:"tier"`
	Complexity RouteScore `json:"complexity"`
	Risk       RouteScore `json:"risk"`
	// Classification is the Complexity label matching the score, so the budgeter
	// and the router agree on one vocabulary.
	Classification Complexity `json:"classification"`
	// UseCheapClassifier is true only when a deterministic decision was not
	// possible *and* an auxiliary model would pay for itself.
	UseCheapClassifier bool `json:"use_cheap_classifier"`
	// Escalate is true when evidence (not a hunch) justifies a stronger model.
	Escalate bool `json:"escalate"`
	// Rationale is a short explanation, always populated.
	Rationale string `json:"rationale"`
	// OutputProfile is the phase whose output budget fits this route.
	OutputPhase Phase `json:"output_phase"`
}

// RoutingConfig tunes the router.
type RoutingConfig struct {
	Enabled bool `json:"enabled"`
	// DeterministicFirst forbids an auxiliary classifier when the deterministic
	// signals already decide.
	DeterministicFirst bool `json:"deterministic_first"`
	// RiskBased lets risk raise the tier independently of complexity.
	RiskBased bool `json:"risk_based"`
	// EscalationEnabled permits raising the tier on evidence of failure.
	EscalationEnabled bool `json:"escalation_enabled"`
	// CheapClassifierWhenROIPositive permits a cheap auxiliary model, but only
	// when the deterministic signals are genuinely ambiguous.
	CheapClassifierWhenROIPositive bool `json:"cheap_classifier_only_when_roi_positive"`
	// MaxTier caps routing, so a cost-constrained setup cannot be escalated past
	// what the operator allows.
	MaxTier Tier `json:"max_tier,omitempty"`
}

// DefaultRoutingConfig follows spec §57.
func DefaultRoutingConfig() RoutingConfig {
	return RoutingConfig{
		Enabled:                        true,
		DeterministicFirst:             true,
		RiskBased:                      true,
		EscalationEnabled:              true,
		CheapClassifierWhenROIPositive: true,
	}
}

// tierRank orders tiers by cost so a cap can be applied.
func tierRank(t Tier) int {
	switch t {
	case TierLocal:
		return 0
	case TierCheap:
		return 1
	case TierMid:
		return 2
	case TierFrontier:
		return 3
	case TierPremium:
		return 4
	default:
		return 2
	}
}

func tierByRank(rank int) Tier {
	switch {
	case rank <= 0:
		return TierLocal
	case rank == 1:
		return TierCheap
	case rank == 2:
		return TierMid
	case rank == 3:
		return TierFrontier
	default:
		return TierPremium
	}
}

// Route picks a tier from the signals.
func Route(s Signals, cfg RoutingConfig) RouteDecision {
	complexity := ComplexityScore(s)
	risk := RiskScore(s)

	d := RouteDecision{
		Complexity:  complexity,
		Risk:        risk,
		OutputPhase: s.Phase,
	}
	if d.OutputPhase == "" {
		d.OutputPhase = PhaseFix
	}

	if !cfg.Enabled {
		d.Tier = TierMid
		d.Classification = ComplexityMedium
		d.Rationale = "routing disabled; using the mid tier"
		return d
	}

	unknown := ambiguous(s, complexity, risk)

	switch {
	case unknown && s.Phase == PhaseClassify:
		// A signal-free classification step needs no model at all. This is the
		// only case that routes to TierLocal, because claiming "no model needed"
		// wrongly is expensive: the task simply does not get done.
		d.Tier = TierLocal
		d.Classification = ComplexityTrivial
	case unknown:
		// Nothing is known about the task. Route to the safe middle rather than
		// the cheapest tier: guessing "cheap" for an unknown task risks a failure
		// that costs more than the saving (spec §46).
		d.Tier = TierMid
		d.Classification = ComplexityMedium
	case complexity.Value < 0.10:
		d.Tier = TierCheap
		d.Classification = ComplexityTrivial
	case complexity.Value < 0.28:
		d.Tier = TierCheap
		d.Classification = ComplexitySimple
	case complexity.Value < 0.55:
		d.Tier = TierMid
		d.Classification = ComplexityMedium
	default:
		d.Tier = TierFrontier
		d.Classification = ComplexityComplex
	}

	// Risk raises the tier independently of size (spec §46: criticality is a
	// signal in its own right).
	if cfg.RiskBased {
		switch {
		case risk.Value >= 0.60:
			d.Tier = raise(d.Tier, TierPremium)
			d.Classification = ComplexityCritical
		case risk.Value >= 0.35:
			d.Tier = raise(d.Tier, TierFrontier)
			if d.Classification != ComplexityCritical {
				d.Classification = ComplexityComplex
			}
		case risk.Value >= 0.20:
			d.Tier = raise(d.Tier, TierMid)
		}
	}

	// Escalation must be evidence-based, not precautionary.
	if cfg.EscalationEnabled {
		switch {
		case s.RepeatedError >= 3:
			d.Escalate = true
			d.Tier = raise(d.Tier, TierPremium)
		case s.PriorFailures >= 2:
			d.Escalate = true
			d.Tier = raise(d.Tier, TierFrontier)
		}
	}

	// An auxiliary classifier is only worth its own cost when the deterministic
	// signals genuinely do not decide: no size information, no risk flags, and
	// no self-reported confidence. Anything else already has an answer, and
	// DeterministicFirst makes that explicit.
	if cfg.CheapClassifierWhenROIPositive && unknown {
		d.UseCheapClassifier = true
	}
	if cfg.DeterministicFirst && !unknown {
		d.UseCheapClassifier = false
	}

	if cfg.MaxTier != "" {
		if tierRank(d.Tier) > tierRank(cfg.MaxTier) {
			d.Tier = cfg.MaxTier
			d.Rationale = "capped by the configured maximum tier; "
		}
	}
	d.Rationale += rationale(d, s)
	return d
}

// ambiguous reports whether the deterministic signals fail to decide.
func ambiguous(s Signals, complexity, risk RouteScore) bool {
	noSizeInfo := s.Files == 0 && s.Modules == 0 && s.DiffLines == 0 && s.Languages == 0
	noRiskFlags := !s.TouchesSecurity && !s.Destructive &&
		!s.TouchesInfrastructure && !s.TouchesArchitecture
	noHistory := s.PriorFailures == 0 && s.RepeatedError == 0
	noConfidence := s.Confidence == 0
	return noSizeInfo && noRiskFlags && noHistory && noConfidence &&
		complexity.Value == 0 && risk.Value == 0
}

func raise(current, floor Tier) Tier {
	if tierRank(floor) > tierRank(current) {
		return floor
	}
	return current
}

func rationale(d RouteDecision, s Signals) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("complexity %.2f", d.Complexity.Value))
	parts = append(parts, fmt.Sprintf("risk %.2f", d.Risk.Value))
	if len(d.Risk.Reasons) > 0 {
		parts = append(parts, strings.Join(d.Risk.Reasons, ", "))
	}
	if d.Escalate {
		parts = append(parts, "escalated on failure evidence")
	}
	if d.UseCheapClassifier {
		parts = append(parts, "signals ambiguous; a cheap classifier may pay for itself")
	}
	if s.HasTests {
		parts = append(parts, "test coverage lowers the cost of a mistake")
	}
	return strings.Join(parts, "; ")
}

// AdaptiveBudget adjusts a cost budget from a routing decision (spec §50).
//
// A cheap task gets a smaller allowance and a critical one a larger allowance,
// but the operator's hard limit is never exceeded: a soft limit exists to change
// behaviour, and a hard limit exists to stop.
func AdaptiveBudget(limits StopLimits, d RouteDecision) StopLimits {
	out := limits
	scale := 1.0
	switch d.Tier {
	case TierLocal, TierCheap:
		scale = 0.35
	case TierMid:
		scale = 1.0
	case TierFrontier:
		scale = 1.6
	case TierPremium:
		scale = 2.2
	}
	if limits.MaxCostUSD > 0 {
		scaled := limits.MaxCostUSD * scale
		if scaled > limits.MaxCostUSD {
			// Never raise a hard cost limit: it is the operator's ceiling.
			scaled = limits.MaxCostUSD
		}
		out.MaxCostUSD = scaled
	}
	if limits.SoftCostUSD > 0 {
		out.SoftCostUSD = limits.SoftCostUSD * scale
		if out.MaxCostUSD > 0 && out.SoftCostUSD > out.MaxCostUSD {
			// A soft limit above the hard limit would never fire.
			out.SoftCostUSD = out.MaxCostUSD * 0.7
		}
	}
	if limits.MaxTokenBudget > 0 {
		scaled := int(float64(limits.MaxTokenBudget) * scale)
		if scaled > limits.MaxTokenBudget {
			scaled = limits.MaxTokenBudget
		}
		out.MaxTokenBudget = scaled
	}
	return out
}

// ParseTier maps a string to a Tier, defaulting to TierMid.
func ParseTier(s string) Tier {
	switch Tier(normalizeToken(s)) {
	case TierLocal:
		return TierLocal
	case TierCheap:
		return TierCheap
	case TierMid:
		return TierMid
	case TierFrontier:
		return TierFrontier
	case TierPremium:
		return TierPremium
	}
	switch normalizeToken(s) {
	case "none", "deterministic":
		return TierLocal
	case "small", "fast", "mini":
		return TierCheap
	case "medium", "standard":
		return TierMid
	case "large", "flagship":
		return TierFrontier
	case "reasoning", "critical":
		return TierPremium
	}
	return TierMid
}
