// Package benchmark implements the mandatory DWYT v5 benchmark (spec §69).
//
// The spec is blunt about why this exists: percentages must not be published as
// product claims without a repeatable measurement behind them. So this package
// measures, and — just as importantly — refuses to report what it cannot
// measure.
//
// It compares the four arms named in §69:
//
//	baseline                  no DWYT: whole files, no memory, raw tool output
//	dwyt_v4                   symbol-level retrieval, broad vault search, raw output
//	dwyt_v5_optimizer         budget + Token ROI + Search V2 + tool compaction + delta reuse
//	dwyt_v5_optimizer_cache   the same, with the stable prefix billed as a cache read
//
// What it measures, deterministically and with no LLM involved:
//
//   - input context tokens per scenario, per arm;
//   - memory (vault) retrieval tokens, broad search versus Search V2;
//   - tool output tokens sent, raw versus compacted;
//   - the operational output budget the optimizer assigns;
//   - relative cost units, using the ranker's cost model ratios.
//
// What it deliberately does NOT measure, and reports as unmeasured rather than
// guessing:
//
//   - completion rate and attempts — needs a live agent loop;
//   - real cost in currency — needs provider pricing and observed usage;
//   - cache hit rate — only real when a provider reports it (spec §39);
//   - latency — no request is issued.
//
// That split is the whole point. A harness that emitted a plausible-looking
// "cost per task" from synthetic fixtures would be exactly the misleading metric
// spec §52 and §56 warn against, and it would make the one number the spec calls
// the north star (cost per successfully completed task) untrustworthy.
package benchmark

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/contextopt"
	"github.com/fvmoraes/dwyt/internal/outputopt"
	"github.com/fvmoraes/dwyt/internal/toolopt"
)

// legacySearchLimit is what the pre-v5 vault search returned: up to 30 notes
// ordered by modification time, raw and stale ones included.
const legacySearchLimit = 30

// ungovernedOutputTokens is the visible-token cost of an answer written with no
// output contract. 1200 is deliberately conservative for a model asked to
// explain what it did; assuming a larger number would flatter the optimizer.
const ungovernedOutputTokens = 1200

// Block is one context candidate plus what it costs an agent that cannot ask
// for a symbol. Without symbol-level retrieval an agent reads the whole file,
// which is the single biggest difference between the baseline and DWYT v4.
type Block struct {
	Candidate contextopt.ContextCandidate
	// FullFileTokens is the whole-file cost. Zero means the block is already a
	// whole file (or a note), so there is no surcharge.
	FullFileTokens int
}

// baselineTokens is what an ungoverned agent pays for this block.
func (b Block) baselineTokens() int {
	if b.FullFileTokens > b.Candidate.Tokens {
		return b.FullFileTokens
	}
	return b.Candidate.Tokens
}

// Scenario is one of the spec §69 cases.
type Scenario struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Complexity  contextopt.Complexity `json:"complexity"`
	Phase       contextopt.Phase      `json:"phase"`
	Description string                `json:"description"`

	// Blocks is the context a repository can offer for this task. Every arm
	// sees the same pool; they differ in how much of it they send.
	Blocks []Block `json:"-"`

	// VaultNotes are the memory notes a broad search would return.
	VaultNotes []VaultNote `json:"-"`

	// ToolOutput is the raw tool output the scenario produces.
	ToolOutput string `json:"-"`

	// Turns is how many agent turns the scenario runs, which is what makes the
	// long agentic case differ from a single fix.
	Turns int `json:"turns"`
}

// candidates returns the candidate view of the block pool.
func (s Scenario) candidates() []contextopt.ContextCandidate {
	out := make([]contextopt.ContextCandidate, 0, len(s.Blocks))
	for _, b := range s.Blocks {
		out = append(out, b.Candidate)
	}
	return out
}

// VaultNote is a memory note with its size and lifecycle flags.
type VaultNote struct {
	Key    string
	Tokens int
	// Canonical marks durable knowledge. Search V2 prefers it.
	Canonical bool
	// Raw marks log/debug content, excluded from the default search.
	Raw bool
	// Stale marks superseded content, excluded from the default search.
	Stale bool
}

// Arm is a measured configuration.
type Arm string

const (
	// ArmBaseline is no DWYT: whole files, no project memory, raw tool output,
	// everything resent on every turn.
	ArmBaseline Arm = "baseline"
	// ArmCurrent is DWYT before v5: symbol-level retrieval through the Codebase
	// MCP and a broad vault search, but no budget, no ranking, no compaction.
	ArmCurrent Arm = "dwyt_v4"
	// ArmOptimized is the v5 Optimizer: budget, ROI ranking, Search V2, tool
	// compaction and delta reuse across turns.
	ArmOptimized Arm = "dwyt_v5_optimizer"
	// ArmOptimizedCached is the v5 Optimizer plus provider cache intelligence:
	// the stable prefix is resent and billed as a cache read instead of being
	// replaced by a compact state block.
	ArmOptimizedCached Arm = "dwyt_v5_optimizer_cache"
)

// Arms returns the canonical arm order.
func Arms() []Arm {
	return []Arm{ArmBaseline, ArmCurrent, ArmOptimized, ArmOptimizedCached}
}

// Measurement is what one arm spent on one scenario.
type Measurement struct {
	Arm Arm `json:"arm"`

	// InputTokens is the total context sent across all turns.
	InputTokens int `json:"input_tokens"`
	// MemoryTokens is the share of that spent on vault retrieval.
	MemoryTokens int `json:"memory_tokens"`
	// CodeTokens is the share spent on code context.
	CodeTokens int `json:"code_tokens"`
	// ToolOutputTokens is the share spent on tool output.
	ToolOutputTokens int `json:"tool_output_tokens"`
	// ReusedTokens is context that was not resent because it was unchanged.
	ReusedTokens int `json:"reused_tokens"`
	// CacheReadTokens and CacheWriteTokens are the tokens billed at the cache
	// read and cache write rates. Only the cache arm sets them.
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
	// OutputBudget is the visible-token target for the final answer.
	OutputBudget int `json:"output_budget"`

	// CostUnits is the cost-model-weighted token cost. It is a relative number
	// derived from the ranker's cost ratios, not currency: real money needs
	// provider pricing and observed usage, which this harness does not have.
	CostUnits float64 `json:"cost_units"`

	// Reductions relative to the baseline arm, in percent. Positive means this
	// arm sent (or spent) less.
	InputReductionPct      float64 `json:"input_reduction_pct"`
	ToolOutputReductionPct float64 `json:"tool_output_reduction_pct"`
	OutputReductionPct     float64 `json:"output_reduction_pct"`
	CostReductionPct       float64 `json:"cost_reduction_pct"`

	// MemoryReductionVsCurrentPct is relative to the dwyt_v4 arm, not the
	// baseline, because the baseline consults no project memory at all. It is
	// nil for arms that retrieve no memory: reporting "100% less" for an arm
	// that never had the feature would be a flattering non-fact.
	MemoryReductionVsCurrentPct *float64 `json:"memory_reduction_vs_current_pct"`
}

// Result is every arm measured on one scenario.
type Result struct {
	Scenario    string                `json:"scenario"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Complexity  contextopt.Complexity `json:"complexity"`
	Turns       int                   `json:"turns"`
	Arms        []Measurement         `json:"arms"`
}

// Arm returns the measurement for one arm.
func (r Result) Arm(a Arm) (Measurement, bool) {
	for _, m := range r.Arms {
		if m.Arm == a {
			return m, true
		}
	}
	return Measurement{}, false
}

// Unmeasured names a KPI the harness cannot produce, with the reason.
type Unmeasured struct {
	KPI    string `json:"kpi"`
	Reason string `json:"reason"`
}

// Totals aggregates one arm over every scenario.
type Totals struct {
	Arm              Arm     `json:"arm"`
	InputTokens      int     `json:"input_tokens"`
	MemoryTokens     int     `json:"memory_tokens"`
	ToolOutputTokens int     `json:"tool_output_tokens"`
	OutputBudget     int     `json:"output_budget"`
	CostUnits        float64 `json:"cost_units"`

	InputReductionPct      float64 `json:"input_reduction_pct"`
	ToolOutputReductionPct float64 `json:"tool_output_reduction_pct"`
	OutputReductionPct     float64 `json:"output_reduction_pct"`
	CostReductionPct       float64 `json:"cost_reduction_pct"`
	// MemoryReductionVsCurrentPct compares against dwyt_v4, see Measurement.
	MemoryReductionVsCurrentPct *float64 `json:"memory_reduction_vs_current_pct"`
}

// Report is the full benchmark outcome.
type Report struct {
	Results []Result `json:"results"`
	Totals  []Totals `json:"totals"`

	// Unmeasured is the honest list of KPIs this harness cannot produce.
	Unmeasured []Unmeasured `json:"unmeasured"`

	// ClaimAllowed is false whenever any KPI required to support a public
	// savings claim is unmeasured. The spec forbids publishing a percentage
	// without a repeatable benchmark *and* without evidence that completion did
	// not regress, and this harness cannot observe completion.
	ClaimAllowed bool   `json:"claim_allowed"`
	ClaimNote    string `json:"claim_note"`
}

// unmeasuredKPIs is fixed: these need a live agent and a real provider.
func unmeasuredKPIs() []Unmeasured {
	return []Unmeasured{
		{
			KPI:    "completion_rate",
			Reason: "requires a live agent loop; a synthetic harness cannot observe whether a task succeeded",
		},
		{
			KPI:    "cost_per_successfully_completed_task",
			Reason: "requires observed provider usage and a priced model; deriving it from fixtures would fabricate the spec's north-star metric",
		},
		{
			KPI:    "cache_hit_rate",
			Reason: "only real when the provider reports it; DWYT never claims an unobserved cache hit",
		},
		{
			KPI:    "latency",
			Reason: "no request is issued, so there is nothing to time",
		},
	}
}

// Scenarios returns the five spec §69 cases.
//
// The fixtures are synthetic but deterministic, so a run is reproducible and a
// regression in the optimizer shows up as a changed number rather than as noise.
// They are shaped after real DWYT usage: a repository has far more context
// available than any single task needs, most vault notes are session history
// rather than knowledge, and build/test output is dominated by lines that carry
// no information.
func Scenarios() []Scenario {
	return []Scenario{
		trivialScenario(),
		smallFrontendScenario(),
		mediumGoScenario(),
		complexRefactorScenario(),
		longAgenticScenario(),
	}
}

// Run measures every scenario across every arm.
func Run() Report {
	report := Report{Unmeasured: unmeasuredKPIs()}
	totals := map[Arm]*Totals{}
	for _, a := range Arms() {
		totals[a] = &Totals{Arm: a}
	}

	for _, s := range Scenarios() {
		r := RunScenario(s)
		report.Results = append(report.Results, r)
		for _, m := range r.Arms {
			t := totals[m.Arm]
			t.InputTokens += m.InputTokens
			t.MemoryTokens += m.MemoryTokens
			t.ToolOutputTokens += m.ToolOutputTokens
			t.OutputBudget += m.OutputBudget
			t.CostUnits += m.CostUnits
		}
	}

	base := totals[ArmBaseline]
	current := totals[ArmCurrent]
	for _, a := range Arms() {
		t := totals[a]
		t.InputReductionPct = reduction(base.InputTokens, t.InputTokens)
		t.ToolOutputReductionPct = reduction(base.ToolOutputTokens, t.ToolOutputTokens)
		t.OutputReductionPct = reduction(base.OutputBudget, t.OutputBudget)
		t.CostReductionPct = reductionF(base.CostUnits, t.CostUnits)
		t.MemoryReductionVsCurrentPct = memoryReduction(current.MemoryTokens, t.MemoryTokens)
		report.Totals = append(report.Totals, *t)
	}

	// A savings claim needs completion evidence, which this harness cannot
	// produce. Saying so here is what keeps the number from being quoted as a
	// product claim (spec §69: "do not accept a savings claim if the completion
	// rate drops materially" — an unmeasured completion rate cannot clear that
	// bar).
	report.ClaimAllowed = false
	report.ClaimNote = "The context, memory and tool-output reductions reported here are measured and reproducible. " +
		"They are NOT a product savings claim: completion rate, real cost and cache " +
		"hit rate are unmeasured here, and spec §69 requires evidence that completion " +
		"did not regress before any percentage is published."
	return report
}

// RunScenario measures one scenario across every arm.
func RunScenario(s Scenario) Result {
	turns := s.Turns
	if turns < 1 {
		turns = 1
	}
	r := Result{
		Scenario:    s.ID,
		Name:        s.Name,
		Description: s.Description,
		Complexity:  s.Complexity,
		Turns:       turns,
	}

	base := measureBaseline(s, turns)
	current := measureCurrent(s, turns)
	optimized, cached := measureOptimizedArms(s, turns)

	r.Arms = []Measurement{base, current, optimized, cached}
	for i := range r.Arms {
		m := &r.Arms[i]
		m.InputReductionPct = reduction(base.InputTokens, m.InputTokens)
		m.ToolOutputReductionPct = reduction(base.ToolOutputTokens, m.ToolOutputTokens)
		m.OutputReductionPct = reduction(base.OutputBudget, m.OutputBudget)
		m.CostReductionPct = reductionF(base.CostUnits, m.CostUnits)
		m.MemoryReductionVsCurrentPct = memoryReduction(current.MemoryTokens, m.MemoryTokens)
	}
	return r
}

// memoryReduction compares an arm's vault retrieval against the dwyt_v4 broad
// search. An arm that retrieves no memory at all gets nil, not 100%: it did not
// improve on the broad search, it simply never had the feature.
func memoryReduction(current, arm int) *float64 {
	if current <= 0 || arm <= 0 {
		return nil
	}
	v := reduction(current, arm)
	return &v
}

// measureBaseline models an agent with no DWYT at all: it reads whole files to
// find a symbol, has no project memory to consult, pastes raw tool output, and
// resends everything on every turn.
//
// This is not a straw man. It is what an agent does by default when nothing
// constrains it and no retrieval tool is available.
func measureBaseline(s Scenario, turns int) Measurement {
	m := Measurement{Arm: ArmBaseline}
	for _, b := range s.Blocks {
		m.CodeTokens += b.baselineTokens()
	}
	m.ToolOutputTokens = contextopt.EstimateTokens(s.ToolOutput)
	m.OutputBudget = ungovernedOutputTokens
	return finishResent(m, turns)
}

// measureCurrent models DWYT before v5: the Codebase MCP answers with symbols
// instead of whole files, and the vault is searched, but nothing bounds what is
// sent. The pre-v5 instruction files actively encouraged this — "save complete
// context before every final response", a search that returned 30 notes by
// recency, raw tool output pasted verbatim.
func measureCurrent(s Scenario, turns int) Measurement {
	m := Measurement{Arm: ArmCurrent}
	for _, b := range s.Blocks {
		m.CodeTokens += b.Candidate.Tokens
	}

	notes := s.VaultNotes
	if len(notes) > legacySearchLimit {
		notes = notes[:legacySearchLimit]
	}
	for _, n := range notes {
		m.MemoryTokens += n.Tokens
	}

	m.ToolOutputTokens = contextopt.EstimateTokens(s.ToolOutput)
	m.OutputBudget = ungovernedOutputTokens
	return finishResent(m, turns)
}

// finishResent multiplies a single-turn measurement over every turn, which is
// what an arm with no session state does: it has no way to say "you already
// have this", so it resends the lot.
func finishResent(m Measurement, turns int) Measurement {
	perTurn := m.CodeTokens + m.MemoryTokens + m.ToolOutputTokens
	m.CodeTokens *= turns
	m.MemoryTokens *= turns
	m.ToolOutputTokens *= turns
	m.InputTokens = perTurn * turns
	// No cache awareness: every token is billed at the uncached rate.
	m.CostUnits = float64(m.InputTokens) * contextopt.DefaultCostModel().Uncached
	return m
}

// measureOptimizedArms measures the v5 pipeline and its cache-aware variant.
//
// Both arms share the same first turn — budget, Search V2, tool compaction and
// ROI selection are identical. They diverge from turn two:
//
//   - the plain arm does not resend the stable prefix at all, substituting the
//     compact session state (spec §11, §12);
//   - the cache arm resends the stable prefix so the model keeps full fidelity,
//     and pays the provider's cache read rate for it (spec §37–§43).
//
// Reporting both is the honest answer to "which is cheaper": the plain arm sends
// fewer tokens, the cache arm can cost less per token. Which one wins depends on
// the provider's cache ratio, so the harness measures instead of asserting.
func measureOptimizedArms(s Scenario, turns int) (Measurement, Measurement) {
	cost := contextopt.DefaultCostModel()
	budget := contextopt.ComputeBudget(contextopt.BudgetProfile{
		Phase:      s.Phase,
		Complexity: s.Complexity,
	})
	outputBudget := outputopt.ProfileFor(s.Phase).TargetTokens

	memoryTokens := optimizedMemoryTokens(s.VaultNotes)

	toolTokens := 0
	if strings.TrimSpace(s.ToolOutput) != "" {
		toolTokens = contextopt.EstimateTokens(toolopt.Compact(s.ToolOutput, toolopt.Options{}).Render())
	}

	// Code: rank by Token ROI and fit into what the budget leaves after memory
	// and tool output.
	session := contextopt.NewSession(s.ID, budget)
	codeBudget := budget
	if spent := memoryTokens + toolTokens; spent < codeBudget.Total {
		codeBudget.Total -= spent
	} else {
		codeBudget.Total = 0
	}

	ranked := contextopt.Rank(s.candidates(), session.RankInput(cost))
	selection := contextopt.Select(ranked, codeBudget)
	firstTurnCode := selection.TokensIncluded

	// The stable prefix is everything the provider could keep across turns:
	// immutable, long-lived and session-stable classes. Volatile content never
	// belongs in a reusable prefix (spec §41).
	stableTokens := 0
	delivered := make([]contextopt.ContextCandidate, 0, len(selection.Included))
	for _, sc := range selection.Included {
		delivered = append(delivered, sc.Candidate)
		if contextopt.CacheClassRank(sc.Candidate.CacheClass) <= contextopt.CacheClassRank(contextopt.CacheSession) {
			stableTokens += sc.Candidate.Tokens
		}
	}
	session.RegisterDelivered(delivered)
	stableTokens += memoryTokens // canonical memory is long-lived by definition

	stateTokens := contextopt.EstimateTokens(session.State().Describe())
	firstTurnTotal := firstTurnCode + memoryTokens + toolTokens

	plain := Measurement{
		Arm:              ArmOptimized,
		CodeTokens:       firstTurnCode,
		MemoryTokens:     memoryTokens,
		ToolOutputTokens: toolTokens,
		InputTokens:      firstTurnTotal,
		OutputBudget:     outputBudget,
	}
	cached := Measurement{
		Arm:              ArmOptimizedCached,
		CodeTokens:       firstTurnCode,
		MemoryTokens:     memoryTokens,
		ToolOutputTokens: toolTokens,
		InputTokens:      firstTurnTotal,
		OutputBudget:     outputBudget,
		// Turn one populates the cache entry, which costs a one-off premium.
		CacheWriteTokens: stableTokens,
	}

	for turn := 2; turn <= turns; turn++ {
		delta, deltaTool := changedTokensForTurn(s, turn)

		// Plain arm: only what changed, plus the compact state that stands in
		// for the prefix it chose not to resend.
		plain.InputTokens += delta + deltaTool + stateTokens
		plain.CodeTokens += delta
		plain.ToolOutputTokens += deltaTool
		plain.ReusedTokens += stableTokens

		// Cache arm: the prefix goes back on the wire, billed as a cache read.
		cached.InputTokens += stableTokens + delta + deltaTool
		cached.CodeTokens += delta
		cached.ToolOutputTokens += deltaTool
		cached.CacheReadTokens += stableTokens
	}

	plain.CostUnits = float64(plain.InputTokens) * cost.Uncached
	cached.CostUnits = float64(cached.InputTokens-cached.CacheReadTokens-cached.CacheWriteTokens)*cost.Uncached +
		float64(cached.CacheReadTokens)*cost.CacheRead +
		float64(cached.CacheWriteTokens)*cost.CacheWrite
	if cached.CostUnits < 0 {
		cached.CostUnits = 0
	}
	return plain, cached
}

// optimizedMemoryTokens applies the Search V2 rules (spec §27): exclude raw and
// stale notes, put canonical knowledge first, cap at the default top-k and the
// default token ceiling.
func optimizedMemoryTokens(notes []VaultNote) int {
	eligible := make([]VaultNote, 0, len(notes))
	for _, n := range notes {
		if n.Raw || n.Stale {
			continue
		}
		eligible = append(eligible, n)
	}
	// Canonical first, then smallest — the cheapest way to add coverage.
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].Canonical != eligible[j].Canonical {
			return eligible[i].Canonical
		}
		if eligible[i].Tokens != eligible[j].Tokens {
			return eligible[i].Tokens < eligible[j].Tokens
		}
		return eligible[i].Key < eligible[j].Key
	})

	total := 0
	for i, n := range eligible {
		if i >= brain.DefaultSearchLimit {
			break
		}
		if total+n.Tokens > brain.DefaultSearchMaxTokens && total > 0 {
			continue
		}
		total += n.Tokens
	}
	return total
}

// changedTokensForTurn is how much genuinely new context a later turn needs,
// split into code delta and tool output delta.
//
// Modelled as a small delta: a turn in a real session adds an error, a diff or a
// symbol, not the whole repository. The numbers are deliberately conservative
// stand-ins — assuming a tiny delta would flatter the optimizer.
func changedTokensForTurn(s Scenario, turn int) (code int, tool int) {
	code = 350
	if s.Complexity == contextopt.ComplexityComplex || s.Complexity == contextopt.ComplexityCritical {
		code = 600
	}
	// Every third turn hits a fresh tool run, which costs a compacted output.
	if turn%3 == 0 && strings.TrimSpace(s.ToolOutput) != "" {
		tool = contextopt.EstimateTokens(toolopt.Compact(s.ToolOutput, toolopt.Options{}).Render())
	}
	return code, tool
}

func reduction(baseline, arm int) float64 {
	return reductionF(float64(baseline), float64(arm))
}

func reductionF(baseline, arm float64) float64 {
	if baseline <= 0 {
		return 0
	}
	saved := baseline - arm
	if saved < 0 {
		saved = 0
	}
	return saved / baseline * 100
}

// Render formats a report as a fixed-width table for the CLI.
func (r Report) Render() string {
	var b strings.Builder
	b.WriteString("DWYT v5 benchmark (spec §69)\n\n")

	const header = "%-24s %8s %7s %6s %5s %6s %6s %6s\n"
	const row = "%-24s %8d %7d %6d %5d %6.1f %6s %6.1f\n"

	for _, res := range r.Results {
		fmt.Fprintf(&b, "%s  %s  [%s, %d turn(s)]\n", res.Scenario, res.Name, res.Complexity, res.Turns)
		if res.Description != "" {
			fmt.Fprintf(&b, "  %s\n", indent(wrap(res.Description, 76), "  "))
		}
		fmt.Fprintf(&b, header, "ARM", "INPUT", "MEMORY", "TOOL", "OUT", "IN-%", "MEM-%", "COST-%")
		for _, m := range res.Arms {
			fmt.Fprintf(&b, row,
				m.Arm, m.InputTokens, m.MemoryTokens, m.ToolOutputTokens, m.OutputBudget,
				m.InputReductionPct, pct(m.MemoryReductionVsCurrentPct), m.CostReductionPct)
		}
		b.WriteString("\n")
	}

	b.WriteString("TOTALS (all scenarios)\n")
	fmt.Fprintf(&b, header, "ARM", "INPUT", "MEMORY", "TOOL", "OUT", "IN-%", "MEM-%", "COST-%")
	for _, t := range r.Totals {
		fmt.Fprintf(&b, row,
			t.Arm, t.InputTokens, t.MemoryTokens, t.ToolOutputTokens, t.OutputBudget,
			t.InputReductionPct, pct(t.MemoryReductionVsCurrentPct), t.CostReductionPct)
	}

	b.WriteString("\nAll -% columns are percent reductions. INPUT, TOOL, OUT and COST are\n")
	b.WriteString("relative to the baseline arm. MEM-% is relative to dwyt_v4, because the\n")
	b.WriteString("baseline consults no project memory at all: it reports n/a rather than a\n")
	b.WriteString("flattering 100%. COST-% uses the ranker's cost-model ratios, not currency.\n")

	b.WriteString("\nNOT MEASURED by this harness:\n")
	for _, u := range r.Unmeasured {
		fmt.Fprintf(&b, "  - %s\n", u.KPI)
		fmt.Fprintf(&b, "      %s\n", indent(wrap(u.Reason, 72), "      "))
	}
	fmt.Fprintf(&b, "\nclaim_allowed: %t\n%s\n", r.ClaimAllowed, wrap(r.ClaimNote, 78))
	return b.String()
}

// pct formats an optional percentage, rendering an absent value as "n/a"
// instead of inventing a zero.
func pct(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.1f", *v)
}

// indent prefixes every line after the first, which the caller has already
// placed.
func indent(s, prefix string) string {
	return strings.ReplaceAll(s, "\n", "\n"+prefix)
}

// wrap breaks text at word boundaries so the CLI note stays readable.
func wrap(s string, width int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			lines = append(lines, line)
			line = w
			continue
		}
		line += " " + w
	}
	return strings.Join(append(lines, line), "\n")
}
