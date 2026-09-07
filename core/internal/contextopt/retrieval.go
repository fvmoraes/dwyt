package contextopt

import (
	"fmt"
	"sort"
	"strings"
)

// Progressive retrieval (spec §9) and the confidence gate (spec §47).
//
// The planner answers the question the DWYT MCP exists to answer: given a task
// and what the session already knows, what is the *next smallest* retrieval
// step, from which MCP, and what must be excluded.

// RetrievalLevel is a step on the progressive retrieval ladder. Lower is
// cheaper and must be exhausted before the next one.
type RetrievalLevel int

const (
	LevelProjectIdentity RetrievalLevel = iota
	LevelProjectMap
	LevelModuleMap
	LevelFileSummary
	LevelSymbol
	LevelRange
	LevelDependencies
	LevelFullFile
)

// String renders a level as the token used in MCP responses.
func (l RetrievalLevel) String() string {
	switch l {
	case LevelProjectIdentity:
		return "project_identity"
	case LevelProjectMap:
		return "project_map"
	case LevelModuleMap:
		return "module_map"
	case LevelFileSummary:
		return "file_summary"
	case LevelSymbol:
		return "symbol"
	case LevelRange:
		return "range"
	case LevelDependencies:
		return "dependencies"
	case LevelFullFile:
		return "full_file"
	default:
		return "unknown"
	}
}

// Source names the MCP that serves a retrieval level.
func (l RetrievalLevel) Source() string {
	if l == LevelProjectIdentity {
		return "obsidian"
	}
	return "codebase"
}

// ParseRetrievalLevel maps a free-form string to a level. Unknown values map to
// LevelProjectMap: the cheapest structural step, which is always a safe start.
func ParseRetrievalLevel(s string) RetrievalLevel {
	switch normalizeToken(s) {
	case "project_identity", "identity", "project":
		return LevelProjectIdentity
	case "project_map", "map", "repo_map":
		return LevelProjectMap
	case "module_map", "module", "modules":
		return LevelModuleMap
	case "file_summary", "file", "summary":
		return LevelFileSummary
	case "symbol", "symbols":
		return LevelSymbol
	case "range", "snippet", "lines":
		return LevelRange
	case "dependencies", "deps", "references", "tests":
		return LevelDependencies
	case "full_file", "full", "whole_file":
		return LevelFullFile
	}
	return LevelProjectMap
}

// PlanRequest describes what the agent is trying to do. Everything is
// optional; a bare task string still produces a usable plan.
type PlanRequest struct {
	Task       string     `json:"task"`
	Phase      Phase      `json:"phase,omitempty"`
	Complexity Complexity `json:"complexity,omitempty"`

	// Hints are free-form nouns from the agent: symbol names, file paths,
	// module names. They steer which memory and code targets are suggested.
	Hints []string `json:"hints,omitempty"`

	// Confidence is the agent's self-reported confidence that it can act with
	// what it already has. Below ConfidenceActThreshold the planner authorizes
	// another retrieval step.
	Confidence float64 `json:"confidence,omitempty"`
	// MissingContext is what the agent says it lacks. When present, the plan
	// targets exactly those items instead of guessing (spec §47).
	MissingContext []string `json:"missing_context,omitempty"`

	// AchievedLevel is the highest retrieval level already completed, so the
	// planner can advance one rung instead of restarting at the bottom.
	AchievedLevel *RetrievalLevel `json:"achieved_level,omitempty"`

	// BudgetProfile lets a caller override the budget defaults.
	BudgetProfile BudgetProfile `json:"budget_profile,omitempty"`
}

// ConfidenceActThreshold is the point at which the optimizer tells the agent to
// stop retrieving and act (spec §15 "stop when sufficient", §47).
const ConfidenceActThreshold = 0.75

// Plan is the compact answer the DWYT MCP returns. It is intentionally small:
// the optimizer must not become another source of token waste (spec §3.1).
type Plan struct {
	Budget Budget `json:"budget"`

	// Memory lists the canonical memory keys worth loading, HOT first.
	Memory []string `json:"memory,omitempty"`
	// Code lists the code targets worth retrieving at the planned level.
	Code []string `json:"code,omitempty"`
	// Exclude lists what must stay out of context.
	Exclude []string `json:"exclude,omitempty"`

	// Level is the retrieval level authorized for this step.
	Level string `json:"level"`
	// Source is the MCP that should serve it.
	Source string `json:"source"`
	// NextLevel is the rung to use if this step proves insufficient. Empty
	// when the ladder is exhausted.
	NextLevel string `json:"next_level,omitempty"`

	// Action is either "retrieve" or "act". "act" means the optimizer judges
	// the current context sufficient.
	Action string `json:"action"`

	// Cache carries the prompt-assembly guidance.
	Cache CacheGuidance `json:"cache"`

	// Output is the visible-token target for the response.
	Output OutputGuidance `json:"output"`

	// Reused lists candidate IDs the session already has, which the agent must
	// reference rather than re-retrieve.
	Reused []string `json:"reused,omitempty"`

	// Notes carries at most a couple of short justifications. It exists so a
	// surprising plan can be explained without a second round-trip; it is
	// never a place for prose.
	Notes []string `json:"notes,omitempty"`
}

// CacheGuidance is the cache section of a plan.
type CacheGuidance struct {
	PreservePrefix bool         `json:"preserve_prefix"`
	Order          []CacheClass `json:"order"`
}

// OutputGuidance is the output section of a plan.
type OutputGuidance struct {
	TargetTokens int  `json:"target_tokens"`
	Structured   bool `json:"structured"`
	// ArtifactException is true when the requested output *is* the artifact,
	// so the operational cap must not apply (spec §19 principle, §33).
	ArtifactException bool `json:"artifact_exception,omitempty"`
}

// defaultExclusions are the categories that never enter context by default
// (spec §5 CONTEXT, §29).
func defaultExclusions() []string {
	return []string{"raw_logs", "resolved_errors", "stale_summaries", "old_sessions"}
}

// memoryTargetsFor returns the canonical memory keys to load for a phase,
// ordered HOT → WARM. Cold memory is never suggested automatically (spec §17).
func memoryTargetsFor(p Phase, complexity Complexity) []string {
	base := []string{"project", "active-constraints"}
	switch p {
	case PhaseClassify:
		return base
	case PhaseRetrieve:
		return append(base, "architecture")
	case PhaseReview, PhasePlan:
		return append(base, "architecture", "decisions", "conventions", "known-issues")
	case PhaseArtifact:
		return append(base, "architecture", "decisions")
	}
	targets := append(base, "architecture", "conventions")
	if complexity == ComplexityComplex || complexity == ComplexityCritical {
		targets = append(targets, "decisions", "known-issues", "lessons")
	}
	return targets
}

// Planner turns a PlanRequest into a Plan. It holds no state of its own: the
// session is passed in, so the same planner serves every project.
type Planner struct{}

// NewPlanner returns a planner.
func NewPlanner() *Planner { return &Planner{} }

// Plan computes the next retrieval step. sess may be nil for a stateless plan
// (the first call of a task), in which case nothing is treated as already seen.
func (Planner) Plan(req PlanRequest, sess *Session) Plan {
	phase := req.Phase
	if phase == "" {
		phase = PhaseFix
	}
	complexity := req.Complexity
	if complexity == "" {
		complexity = ComplexityMedium
	}

	profile := req.BudgetProfile
	profile.Phase = phase
	profile.Complexity = complexity
	budget := ComputeBudget(profile)
	if sess != nil {
		// An existing session may already have expanded past the base budget;
		// never hand back a smaller allowance than the agent already has.
		if current := sess.Budget(); current.Total > budget.Total {
			budget = current
		}
	}

	level := LevelProjectMap
	if req.AchievedLevel != nil {
		level = *req.AchievedLevel + 1
		if level > LevelFullFile {
			level = LevelFullFile
		}
	} else if len(req.MissingContext) > 0 {
		// The agent named what it lacks, so skip the map rungs and go
		// straight to the symbol level (spec §47: retrieve only the gap).
		level = LevelSymbol
	}

	plan := Plan{
		Budget:  budget,
		Memory:  memoryTargetsFor(phase, complexity),
		Code:    codeTargets(req, level),
		Exclude: defaultExclusions(),
		Level:   level.String(),
		Source:  level.Source(),
		Action:  "retrieve",
		Cache: CacheGuidance{
			PreservePrefix: true,
			Order:          CacheClasses(),
		},
		Output: OutputGuidance{
			TargetTokens:      OutputTargetForPhase(phase),
			Structured:        phase != PhaseArtifact && phase != PhasePlan && phase != PhaseReview,
			ArtifactException: phase == PhaseArtifact,
		},
	}
	if level < LevelFullFile {
		plan.NextLevel = (level + 1).String()
	}

	// Stop when sufficient. A confident agent is told to act, not to keep
	// reading — that is the single biggest token sink the optimizer prevents.
	if req.Confidence >= ConfidenceActThreshold && len(req.MissingContext) == 0 {
		plan.Action = "act"
		plan.Code = nil
		plan.Notes = append(plan.Notes, "confidence sufficient; act without further retrieval")
	}

	if phase == PhaseClassify {
		// Classification needs no code at all.
		plan.Code = nil
		plan.Level = LevelProjectIdentity.String()
		plan.Source = LevelProjectIdentity.Source()
		plan.NextLevel = LevelProjectMap.String()
	}

	if level == LevelFullFile {
		plan.Notes = append(plan.Notes,
			"full file is exceptional: justify why symbol/range is insufficient")
	}

	if sess != nil {
		for _, sym := range sess.Symbols() {
			if sym.ChangedRange == "" && sym.ContentHash != "" {
				plan.Reused = append(plan.Reused, sym.Key())
			}
		}
		sort.Strings(plan.Reused)
	}

	return plan
}

// codeTargets picks the code identifiers to retrieve. Explicit missing context
// wins over hints, and hints win over the task string, because each is more
// specific than the last.
func codeTargets(req PlanRequest, level RetrievalLevel) []string {
	if level <= LevelProjectMap {
		return nil
	}
	if len(req.MissingContext) > 0 {
		return dedupeStrings(req.MissingContext)
	}
	if len(req.Hints) > 0 {
		return dedupeStrings(req.Hints)
	}
	// Fall back to capitalised/qualified words in the task, which are the
	// identifiers an agent typically names ("fix ResourceTable delete flow").
	return dedupeStrings(identifierCandidates(req.Task))
}

// identifierCandidates extracts probable code identifiers from free text: words
// containing an interior capital, a dot, a slash or an underscore. Plain lower
// case prose words are ignored to avoid suggesting "the" as a symbol.
func identifierCandidates(text string) []string {
	var out []string
	for _, raw := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == ';' || r == '(' || r == ')' ||
			r == '"' || r == '\'' || r == '`' || r == '\n' || r == '\t'
	}) {
		word := strings.Trim(raw, ".:!?")
		if len(word) < 3 {
			continue
		}
		hasUpperInside := false
		for i, r := range word {
			if i > 0 && r >= 'A' && r <= 'Z' {
				hasUpperInside = true
				break
			}
		}
		if hasUpperInside || strings.ContainsAny(word, "/_") ||
			(strings.Contains(word, ".") && !strings.HasSuffix(word, ".")) {
			out = append(out, word)
		}
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// StopLimits bound an agentic loop (spec §49).
type StopLimits struct {
	MaxToolIterations   int     `json:"max_tool_iterations,omitempty"`
	MaxFailedBuilds     int     `json:"max_failed_builds,omitempty"`
	MaxSameErrorRetries int     `json:"max_same_error_retries,omitempty"`
	MaxTokenBudget      int     `json:"max_token_budget,omitempty"`
	MaxCostUSD          float64 `json:"max_cost_usd,omitempty"`
	SoftCostUSD         float64 `json:"soft_cost_usd,omitempty"`
	MaxExpansions       int     `json:"max_expansions,omitempty"`
}

// DefaultStopLimits are conservative defaults that stop a runaway loop without
// interrupting ordinary work.
func DefaultStopLimits() StopLimits {
	return StopLimits{
		MaxToolIterations:   40,
		MaxFailedBuilds:     5,
		MaxSameErrorRetries: 3,
		MaxTokenBudget:      MaxBudget,
		MaxCostUSD:          0.50,
		SoftCostUSD:         0.35,
		MaxExpansions:       6,
	}
}

// LoopObservation is the measured state of the current agentic loop.
type LoopObservation struct {
	ToolIterations int     `json:"tool_iterations,omitempty"`
	FailedBuilds   int     `json:"failed_builds,omitempty"`
	MaxSameError   int     `json:"max_same_error,omitempty"`
	TokensUsed     int     `json:"tokens_used,omitempty"`
	CostUSD        float64 `json:"cost_usd,omitempty"`
	Expansions     int     `json:"expansions,omitempty"`
}

// StopDecision is the optimizer's verdict on whether the loop may continue.
type StopDecision struct {
	// Continue is false when a hard limit was hit.
	Continue bool `json:"continue"`
	// SoftLimit is true when a soft threshold was crossed: keep going, but
	// reduce context and output (spec §50).
	SoftLimit bool     `json:"soft_limit,omitempty"`
	Reasons   []string `json:"reasons,omitempty"`
	// Action is the recommended next move: "continue", "reduce", "escalate"
	// or "stop".
	Action string `json:"action"`
}

// EvaluateStop applies the stop conditions. It never throws: an unset limit is
// treated as "no limit", so a partially-configured StopLimits is safe.
func EvaluateStop(limits StopLimits, obs LoopObservation) StopDecision {
	d := StopDecision{Continue: true, Action: "continue"}

	hard := func(reason string) {
		d.Continue = false
		d.Action = "stop"
		d.Reasons = append(d.Reasons, reason)
	}

	if limits.MaxToolIterations > 0 && obs.ToolIterations >= limits.MaxToolIterations {
		hard(fmt.Sprintf("tool iterations %d >= %d", obs.ToolIterations, limits.MaxToolIterations))
	}
	if limits.MaxFailedBuilds > 0 && obs.FailedBuilds >= limits.MaxFailedBuilds {
		hard(fmt.Sprintf("failed builds %d >= %d", obs.FailedBuilds, limits.MaxFailedBuilds))
	}
	if limits.MaxSameErrorRetries > 0 && obs.MaxSameError >= limits.MaxSameErrorRetries {
		hard(fmt.Sprintf("same error repeated %dx >= %d", obs.MaxSameError, limits.MaxSameErrorRetries))
		// Repeating one error is the signal for escalation rather than a plain
		// stop: a stronger model or a human is the productive next step.
		d.Action = "escalate"
	}
	if limits.MaxTokenBudget > 0 && obs.TokensUsed >= limits.MaxTokenBudget {
		hard(fmt.Sprintf("tokens %d >= %d", obs.TokensUsed, limits.MaxTokenBudget))
	}
	if limits.MaxCostUSD > 0 && obs.CostUSD >= limits.MaxCostUSD {
		hard(fmt.Sprintf("cost $%.4f >= $%.4f", obs.CostUSD, limits.MaxCostUSD))
	}
	if limits.MaxExpansions > 0 && obs.Expansions >= limits.MaxExpansions {
		hard(fmt.Sprintf("context expansions %d >= %d", obs.Expansions, limits.MaxExpansions))
	}

	if d.Continue && limits.SoftCostUSD > 0 && obs.CostUSD >= limits.SoftCostUSD {
		d.SoftLimit = true
		d.Action = "reduce"
		d.Reasons = append(d.Reasons,
			fmt.Sprintf("cost $%.4f crossed soft limit $%.4f: drop low-ROI context and shrink output",
				obs.CostUSD, limits.SoftCostUSD))
	}
	return d
}
