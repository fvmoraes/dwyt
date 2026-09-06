package contextgov

// Budgeter turns a task profile into a concrete token allowance before any
// broad retrieval happens (spec §7). The defaults are the operational
// recommendation from the spec, not a universal limit: a profile may ask for
// less on a trivial task and more on a cross-module refactor.

// Phase names the step of the agent loop a request belongs to. Both the
// context budget and the output budget are phase-dependent (spec §33).
type Phase string

const (
	PhaseClassify Phase = "classify"
	PhaseRetrieve Phase = "retrieve"
	PhaseToolLoop Phase = "tool_loop"
	PhaseFix      Phase = "fix"
	PhaseReview   Phase = "review"
	PhasePlan     Phase = "plan"
	PhaseArtifact Phase = "artifact"
)

// Complexity is the deterministic task classification that drives budget size,
// output size and (optionally) model routing.
type Complexity string

const (
	ComplexityTrivial  Complexity = "trivial"
	ComplexitySimple   Complexity = "simple"
	ComplexityMedium   Complexity = "medium"
	ComplexityComplex  Complexity = "complex"
	ComplexityCritical Complexity = "critical"
)

// Defaults from spec §7 / §57. Exported so the config layer and the tests
// share one source of truth instead of repeating magic numbers.
const (
	DefaultBudget         = 48000
	MaxBudget             = 140000
	DefaultReservePercent = 12
)

// Budget is the allowance handed to the agent for a single request. All values
// are token counts. Total is the ceiling for everything that is *sent*;
// Reserve is carved out of Total and held back for tool loops and the answer,
// so Total-Reserve is what retrieval may actually spend.
type Budget struct {
	Total   int `json:"total"`
	Reserve int `json:"reserve"`

	// Allocation is the per-section split of Total-Reserve. It is advisory:
	// the ranker may spend a section's unused share on another section, but
	// it may never exceed Total.
	SystemAndPolicy int `json:"system_and_policy"`
	ProjectMemory   int `json:"project_memory"`
	RelevantCode    int `json:"relevant_code"`
	ToolResults     int `json:"tool_results"`

	// OutputTarget is the visible-token target for the response, chosen from
	// the same phase profile so input and output governance stay consistent.
	OutputTarget int `json:"output_target"`

	// MaxTotal is the ceiling progressive expansion may grow Total to.
	MaxTotal int `json:"max_total"`
}

// Retrievable is the number of tokens retrieval may consume.
func (b Budget) Retrievable() int {
	v := b.Total - b.Reserve
	if v < 0 {
		return 0
	}
	return v
}

// BudgetProfile describes the task being budgeted. Every field is optional;
// the zero value produces the adaptive default budget.
type BudgetProfile struct {
	Phase      Phase      `json:"phase,omitempty"`
	Complexity Complexity `json:"complexity,omitempty"`

	// DefaultBudget/MaxBudget/ReservePercent override the package defaults so
	// the config file (spec §57) can tune them without code changes.
	DefaultBudget  int `json:"default_budget,omitempty"`
	MaxBudget      int `json:"max_budget,omitempty"`
	ReservePercent int `json:"reserve_percent,omitempty"`

	// ModelContextWindow, when known, caps the budget: DWYT must never plan a
	// context larger than the window it will be sent to.
	ModelContextWindow int `json:"model_context_window,omitempty"`

	// LongContextThreshold is the point above which the provider charges a
	// multiplier (spec §44). When set, the budgeter keeps Total below it
	// unless the caller explicitly allows crossing.
	LongContextThreshold int  `json:"long_context_threshold,omitempty"`
	AllowLongContext     bool `json:"allow_long_context,omitempty"`
}

// complexityScale multiplies the base budget. A trivial task does not need
// 48k tokens of context, and a cross-module refactor may legitimately need
// more than the default.
func complexityScale(c Complexity) float64 {
	switch c {
	case ComplexityTrivial:
		return 0.15
	case ComplexitySimple:
		return 0.4
	case ComplexityMedium:
		return 1.0
	case ComplexityComplex:
		return 1.75
	case ComplexityCritical:
		return 2.25
	default:
		return 1.0
	}
}

// phaseScale shrinks the budget for phases that only need to make a routing
// decision. Classification does not need the codebase in context.
func phaseScale(p Phase) float64 {
	switch p {
	case PhaseClassify:
		return 0.08
	case PhaseRetrieve:
		return 0.35
	case PhaseToolLoop:
		return 0.85
	case PhaseFix:
		return 1.0
	case PhaseReview:
		return 1.2
	case PhasePlan:
		return 1.3
	case PhaseArtifact:
		return 1.2
	default:
		return 1.0
	}
}

// ComputeBudget derives a Budget from a profile. It is pure and deterministic.
func ComputeBudget(p BudgetProfile) Budget {
	base := p.DefaultBudget
	if base <= 0 {
		base = DefaultBudget
	}
	max := p.MaxBudget
	if max <= 0 {
		max = MaxBudget
	}
	// MaxBudget is a hard ceiling, so a base larger than it must be clamped
	// down rather than raising the ceiling. Raising it would let a caller that
	// only tightened MaxBudget silently get a bigger budget than it asked for.
	if base > max {
		base = max
	}
	reservePct := p.ReservePercent
	if reservePct <= 0 || reservePct >= 100 {
		reservePct = DefaultReservePercent
	}

	total := int(float64(base) * complexityScale(p.Complexity) * phaseScale(p.Phase))

	// Floor: below this a request cannot carry the DWYT contract plus a user
	// message, and a budget that cannot fit the essentials is worse than no
	// budget at all.
	const minTotal = 2000
	if total < minTotal {
		total = minTotal
	}

	// Every ceiling is applied after the floor, so a tiny configured ceiling
	// always wins over the floor. Order matters here: the floor is a usability
	// hint, the ceilings are contracts with the model and the provider.
	if total > max {
		total = max
	}
	// Never plan above the model's window, and leave the reserve inside it.
	if p.ModelContextWindow > 0 && total > p.ModelContextWindow {
		total = p.ModelContextWindow
	}
	// Stay under a long-context pricing threshold unless explicitly allowed
	// (spec §44).
	if !p.AllowLongContext && p.LongContextThreshold > 0 && total > p.LongContextThreshold {
		total = p.LongContextThreshold
	}

	reserve := total * reservePct / 100
	if reserve < 1 {
		reserve = 1
	}
	spendable := total - reserve

	b := Budget{
		Total:        total,
		Reserve:      reserve,
		MaxTotal:     max,
		OutputTarget: OutputTargetForPhase(p.Phase),
	}
	// Split mirrors the recommended allocation in spec §7, expressed as
	// fractions of the spendable pool so it scales with the budget.
	b.SystemAndPolicy = pctOf(spendable, 7)
	b.ProjectMemory = pctOf(spendable, 12)
	b.RelevantCode = pctOf(spendable, 65)
	b.ToolResults = spendable - b.SystemAndPolicy - b.ProjectMemory - b.RelevantCode
	if b.ToolResults < 0 {
		b.ToolResults = 0
	}
	return b
}

func pctOf(v, pct int) int {
	if v <= 0 {
		return 0
	}
	return v * pct / 100
}

// OutputTargetForPhase returns the visible-token target for a phase
// (spec §33). These are targets, not hard truncation limits: the output
// governor documents the artifact exception separately.
func OutputTargetForPhase(p Phase) int {
	switch p {
	case PhaseClassify:
		return 120
	case PhaseRetrieve:
		return 200
	case PhaseToolLoop:
		return 300
	case PhaseFix:
		return 350
	case PhaseReview:
		return 1500
	case PhasePlan:
		return 4000
	case PhaseArtifact:
		return 0 // 0 means "no operational cap": the output is the artifact.
	default:
		return 300
	}
}

// Expand grows a budget for progressive expansion (spec §48). It adds the
// requested tokens, never exceeding MaxTotal, and recomputes the reserve so
// the proportion is preserved. Returning the granted delta (rather than only
// the new budget) lets the caller tell the agent how much more it actually
// got, which matters when the ceiling was hit.
func (b Budget) Expand(additional int) (Budget, int) {
	if additional <= 0 {
		return b, 0
	}
	max := b.MaxTotal
	if max <= 0 {
		max = MaxBudget
	}
	if b.Total >= max {
		return b, 0
	}
	granted := additional
	if b.Total+granted > max {
		granted = max - b.Total
	}

	// Preserve the reserve ratio across the expansion. Recomputing from the
	// original ratio (instead of scaling the old absolute value) keeps the
	// reserve meaningful after several expansions.
	ratio := 0.0
	if b.Total > 0 {
		ratio = float64(b.Reserve) / float64(b.Total)
	}
	next := b
	next.Total = b.Total + granted
	next.Reserve = int(float64(next.Total) * ratio)
	if next.Reserve < 1 {
		next.Reserve = 1
	}
	spendable := next.Total - next.Reserve
	next.SystemAndPolicy = pctOf(spendable, 7)
	next.ProjectMemory = pctOf(spendable, 12)
	next.RelevantCode = pctOf(spendable, 65)
	next.ToolResults = spendable - next.SystemAndPolicy - next.ProjectMemory - next.RelevantCode
	if next.ToolResults < 0 {
		next.ToolResults = 0
	}
	return next, granted
}

// ParsePhase maps a free-form string to a Phase, defaulting to PhaseFix,
// which carries the ordinary operational output target.
func ParsePhase(s string) Phase {
	switch Phase(normalizeToken(s)) {
	case PhaseClassify:
		return PhaseClassify
	case PhaseRetrieve:
		return PhaseRetrieve
	case PhaseToolLoop:
		return PhaseToolLoop
	case PhaseFix:
		return PhaseFix
	case PhaseReview:
		return PhaseReview
	case PhasePlan:
		return PhasePlan
	case PhaseArtifact:
		return PhaseArtifact
	}
	switch normalizeToken(s) {
	case "tools", "tool", "loop", "execute", "execution":
		return PhaseToolLoop
	case "document", "documentation", "artifact_output", "doc":
		return PhaseArtifact
	case "planning", "spec":
		return PhasePlan
	case "audit", "code_review":
		return PhaseReview
	case "classification", "route", "routing":
		return PhaseClassify
	case "retrieval", "search":
		return PhaseRetrieve
	}
	return PhaseFix
}

// ParseComplexity maps a free-form string to a Complexity, defaulting to
// ComplexityMedium.
func ParseComplexity(s string) Complexity {
	switch Complexity(normalizeToken(s)) {
	case ComplexityTrivial:
		return ComplexityTrivial
	case ComplexitySimple:
		return ComplexitySimple
	case ComplexityMedium:
		return ComplexityMedium
	case ComplexityComplex:
		return ComplexityComplex
	case ComplexityCritical:
		return ComplexityCritical
	}
	switch normalizeToken(s) {
	case "tiny", "typo":
		return ComplexityTrivial
	case "small", "easy":
		return ComplexitySimple
	case "normal", "default":
		return ComplexityMedium
	case "large", "hard", "refactor":
		return ComplexityComplex
	case "security", "production":
		return ComplexityCritical
	}
	return ComplexityMedium
}
