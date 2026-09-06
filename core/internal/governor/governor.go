// Package governor is the DWYT MCP Governor runtime (spec §3.1).
//
// It is the single authority that answers: what is the smallest context the
// agent needs right now, which sources should serve it, how much may be spent,
// and which rules apply. Everything it returns is compact by construction —
// the governor must not become another source of token waste, so responses are
// small structured payloads, never restatements of the policy text.
//
// The runtime is stateful only in the sense that it remembers per-task
// sessions. All the decision logic lives in contextgov/outputgov/toolgov and is
// pure, which keeps the governor testable and its answers reproducible.
package governor

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/contextgov"
	"github.com/fvmoraes/dwyt/internal/outputgov"
	"github.com/fvmoraes/dwyt/internal/rawstore"
	"github.com/fvmoraes/dwyt/internal/toolgov"
)

// PolicyVersion identifies the behaviour of the governor. It is part of the
// cache identity: a policy change must invalidate a cached prefix, because the
// rules baked into that prefix are no longer the rules in force.
const PolicyVersion = "5.0.0"

// MemoryHealthProvider is implemented by the Brain so the governor can report
// vault health without importing the brain package (which would create a cycle:
// the brain's HTTP handlers already reach into the governor).
type MemoryHealthProvider interface {
	MemoryHealth() map[string]interface{}
}

// HousekeeperStatusProvider is implemented by the housekeeper for the same
// reason.
type HousekeeperStatusProvider interface {
	HousekeeperStatus() map[string]interface{}
}

// UsageRecorder receives normalized usage reports so telemetry can persist
// them. It is an interface so the governor works with or without a database.
type UsageRecorder interface {
	RecordUsage(Usage) error
}

// Config holds the tunables from the DWYT v5 configuration (spec §57).
type Config struct {
	DefaultBudget  int `json:"default_budget"`
	MaxBudget      int `json:"max_budget"`
	ReservePercent int `json:"reserve_percent"`

	ProgressiveExpansion bool `json:"progressive_expansion"`
	ConfidenceGated      bool `json:"confidence_gated"`
	PreserveCachePrefix  bool `json:"preserve_cache_prefix"`
	ReuseBeforeRetrieve  bool `json:"reuse_before_retrieve"`

	DefaultTopK int `json:"default_top_k"`

	OperationalOutputTarget int  `json:"operational_output_target"`
	OperationalOutputMax    int  `json:"operational_output_max"`
	StructuredOperational   bool `json:"structured_operational"`

	StopLimits contextgov.StopLimits `json:"stop_limits"`

	// RawTTL is the retention of tool-output raw objects (spec §22 P3:
	// 72 hours).
	RawTTL time.Duration `json:"raw_ttl"`
}

// DefaultConfig returns the operational recommendation from the spec.
func DefaultConfig() Config {
	return Config{
		DefaultBudget:           contextgov.DefaultBudget,
		MaxBudget:               contextgov.MaxBudget,
		ReservePercent:          contextgov.DefaultReservePercent,
		ProgressiveExpansion:    true,
		ConfidenceGated:         true,
		PreserveCachePrefix:     true,
		ReuseBeforeRetrieve:     true,
		DefaultTopK:             5,
		OperationalOutputTarget: outputgov.OperationalTarget,
		OperationalOutputMax:    outputgov.OperationalMax,
		StructuredOperational:   true,
		StopLimits:              contextgov.DefaultStopLimits(),
		RawTTL:                  72 * time.Hour,
	}
}

// Governor is the runtime.
type Governor struct {
	mu       sync.RWMutex
	cfg      Config
	planner  *contextgov.Planner
	sessions map[string]*contextgov.Session
	// sessionOrder tracks insertion order so the oldest idle session is
	// evicted first when the cap is reached.
	sessionOrder []string

	raw *rawstore.Store

	memory      MemoryHealthProvider
	housekeeper HousekeeperStatusProvider
	usage       UsageRecorder

	// loop accumulates the measured agentic-loop state per task, feeding the
	// stop conditions.
	loop map[string]*contextgov.LoopObservation
}

// maxSessions bounds memory use for a long-running daemon. Sessions are small
// (a few KB) but unbounded growth in a process that runs for weeks is a leak.
const maxSessions = 64

// New creates a governor. rawHome is the DWYT home directory; when it is empty
// the raw store is disabled and raw-related calls report that honestly instead
// of failing.
func New(cfg Config, rawHome string) *Governor {
	if cfg.DefaultBudget <= 0 {
		cfg = DefaultConfig()
	}
	g := &Governor{
		cfg:      cfg,
		planner:  contextgov.NewPlanner(),
		sessions: map[string]*contextgov.Session{},
		loop:     map[string]*contextgov.LoopObservation{},
	}
	if rawHome != "" {
		if store, err := rawstore.New(rawHome); err == nil {
			g.raw = store
		}
	}
	return g
}

// SetMemoryHealthProvider wires the Brain.
func (g *Governor) SetMemoryHealthProvider(p MemoryHealthProvider) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.memory = p
}

// SetHousekeeperStatusProvider wires the housekeeper.
func (g *Governor) SetHousekeeperStatusProvider(p HousekeeperStatusProvider) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.housekeeper = p
}

// SetUsageRecorder wires telemetry persistence.
func (g *Governor) SetUsageRecorder(r UsageRecorder) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.usage = r
}

// Config returns the active configuration.
func (g *Governor) Config() Config {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.cfg
}

// RawStore exposes the raw object store (nil when disabled).
func (g *Governor) RawStore() *rawstore.Store {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.raw
}

// session returns (creating if needed) the session for a task.
func (g *Governor) session(taskID string, budget contextgov.Budget) *contextgov.Session {
	if taskID == "" {
		taskID = "default"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if s, ok := g.sessions[taskID]; ok {
		return s
	}
	s := contextgov.NewSession(taskID, budget)
	g.sessions[taskID] = s
	g.sessionOrder = append(g.sessionOrder, taskID)
	for len(g.sessionOrder) > maxSessions {
		oldest := g.sessionOrder[0]
		g.sessionOrder = g.sessionOrder[1:]
		delete(g.sessions, oldest)
		delete(g.loop, oldest)
	}
	return s
}

// Session returns the session for a task without creating it.
func (g *Governor) Session(taskID string) (*contextgov.Session, bool) {
	if taskID == "" {
		taskID = "default"
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	s, ok := g.sessions[taskID]
	return s, ok
}

// PlanRequest is the governor's public plan input. It mirrors
// contextgov.PlanRequest plus the task identity, so the MCP layer has a single
// struct to decode into.
type PlanRequest struct {
	TaskID     string   `json:"task_id,omitempty"`
	Task       string   `json:"task"`
	Phase      string   `json:"phase,omitempty"`
	Complexity string   `json:"complexity,omitempty"`
	Hints      []string `json:"hints,omitempty"`

	Confidence     float64  `json:"confidence,omitempty"`
	MissingContext []string `json:"missing_context,omitempty"`
	AchievedLevel  string   `json:"achieved_level,omitempty"`

	ModelContextWindow   int  `json:"model_context_window,omitempty"`
	LongContextThreshold int  `json:"long_context_threshold,omitempty"`
	AllowLongContext     bool `json:"allow_long_context,omitempty"`
}

// PlanResponse is the compact answer returned to the agent.
type PlanResponse struct {
	TaskID string                  `json:"task_id"`
	Plan   contextgov.Plan         `json:"plan"`
	Stop   contextgov.StopDecision `json:"stop"`
	// PolicyVersion lets a client detect that the governing rules changed.
	PolicyVersion string `json:"policy_version"`
}

// ContextPlan is the primary governor entry point (`dwyt_context_plan`).
func (g *Governor) ContextPlan(req PlanRequest) PlanResponse {
	cfg := g.Config()

	phase := contextgov.ParsePhase(req.Phase)
	complexity := contextgov.ParseComplexity(req.Complexity)

	profile := contextgov.BudgetProfile{
		Phase:                phase,
		Complexity:           complexity,
		DefaultBudget:        cfg.DefaultBudget,
		MaxBudget:            cfg.MaxBudget,
		ReservePercent:       cfg.ReservePercent,
		ModelContextWindow:   req.ModelContextWindow,
		LongContextThreshold: req.LongContextThreshold,
		AllowLongContext:     req.AllowLongContext,
	}

	taskID := req.TaskID
	if taskID == "" {
		taskID = "default"
	}
	sess := g.session(taskID, contextgov.ComputeBudget(profile))
	sess.SetObjective(req.Task, phase)

	inner := contextgov.PlanRequest{
		Task:           req.Task,
		Phase:          phase,
		Complexity:     complexity,
		Hints:          req.Hints,
		Confidence:     req.Confidence,
		MissingContext: req.MissingContext,
		BudgetProfile:  profile,
	}
	if req.AchievedLevel != "" {
		level := contextgov.ParseRetrievalLevel(req.AchievedLevel)
		inner.AchievedLevel = &level
	}
	if !cfg.ConfidenceGated {
		// Confidence gating disabled: never short-circuit retrieval on the
		// agent's self-report.
		inner.Confidence = 0
	}

	plan := g.planner.Plan(inner, sess)
	if !cfg.PreserveCachePrefix {
		plan.Cache.PreservePrefix = false
	}
	if !cfg.ReuseBeforeRetrieve {
		plan.Reused = nil
	}
	if plan.Output.TargetTokens > 0 && cfg.OperationalOutputTarget > 0 &&
		phase != contextgov.PhaseReview && phase != contextgov.PhasePlan {
		// Honour a configured operational target over the phase default when
		// the operator tightened it.
		if cfg.OperationalOutputTarget < plan.Output.TargetTokens {
			plan.Output.TargetTokens = cfg.OperationalOutputTarget
		}
	}
	plan.Output.Structured = plan.Output.Structured && cfg.StructuredOperational

	return PlanResponse{
		TaskID:        taskID,
		Plan:          plan,
		Stop:          g.evaluateStop(taskID),
		PolicyVersion: PolicyVersion,
	}
}

// StatusResponse is the answer to `dwyt_context_status`.
type StatusResponse struct {
	TaskID string                  `json:"task_id"`
	State  contextgov.SessionState `json:"state"`
	Budget contextgov.Budget       `json:"budget"`
	// Registered is how many candidates the session has been told about.
	Registered int `json:"registered"`
	// TokensRegistered is their total estimated size.
	TokensRegistered int `json:"tokens_registered"`
	// Reusable lists symbol keys whose content is unchanged since last seen.
	Reusable      []string                `json:"reusable,omitempty"`
	Expansions    int                     `json:"expansions"`
	Stop          contextgov.StopDecision `json:"stop"`
	PolicyVersion string                  `json:"policy_version"`
}

// ContextStatus reports the session state (`dwyt_context_status`).
func (g *Governor) ContextStatus(taskID string) StatusResponse {
	if taskID == "" {
		taskID = "default"
	}
	sess, ok := g.Session(taskID)
	if !ok {
		return StatusResponse{
			TaskID:        taskID,
			Budget:        contextgov.ComputeBudget(contextgov.BudgetProfile{}),
			Stop:          g.evaluateStop(taskID),
			PolicyVersion: PolicyVersion,
		}
	}
	symbols := sess.Symbols()
	var reusable []string
	tokens := 0
	for _, s := range symbols {
		tokens += s.Tokens
		if s.ChangedRange == "" && s.ContentHash != "" {
			reusable = append(reusable, s.Key())
		}
	}
	sort.Strings(reusable)
	return StatusResponse{
		TaskID:           taskID,
		State:            sess.State(),
		Budget:           sess.Budget(),
		Registered:       len(symbols),
		TokensRegistered: tokens,
		Reusable:         reusable,
		Expansions:       sess.Expansions(),
		Stop:             g.evaluateStop(taskID),
		PolicyVersion:    PolicyVersion,
	}
}

// RegisterItem is one context block the agent reports having obtained.
type RegisterItem struct {
	ID           string  `json:"id,omitempty"`
	Kind         string  `json:"kind,omitempty"`
	Title        string  `json:"title,omitempty"`
	Tokens       int     `json:"tokens,omitempty"`
	Content      string  `json:"content,omitempty"`
	ContentRef   string  `json:"content_ref,omitempty"`
	ContentHash  string  `json:"content_hash,omitempty"`
	Symbol       string  `json:"symbol,omitempty"`
	Path         string  `json:"path,omitempty"`
	State        string  `json:"state,omitempty"`
	CacheClass   string  `json:"cache_class,omitempty"`
	Source       string  `json:"source,omitempty"`
	Relevance    float64 `json:"relevance,omitempty"`
	Confidence   float64 `json:"confidence,omitempty"`
	Pinned       bool    `json:"pinned,omitempty"`
	ChangedRange string  `json:"changed_range,omitempty"`
}

// RegisterRequest is the input to `dwyt_register_context`.
type RegisterRequest struct {
	TaskID string         `json:"task_id,omitempty"`
	Items  []RegisterItem `json:"items"`
}

// RegisterDecision is the governor's verdict for one registered item.
type RegisterDecision struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
	Tokens   int    `json:"tokens"`
	Reason   string `json:"reason,omitempty"`
}

// RegisterResponse reports what to keep, what to drop and what to reuse.
type RegisterResponse struct {
	TaskID        string             `json:"task_id"`
	Decisions     []RegisterDecision `json:"decisions"`
	TokensKept    int                `json:"tokens_kept"`
	TokensDropped int                `json:"tokens_dropped"`
	TokensReused  int                `json:"tokens_reused"`
	Budget        contextgov.Budget  `json:"budget"`
	// Order is the cache-safe assembly order of the kept items.
	Order         []string `json:"order,omitempty"`
	PolicyVersion string   `json:"policy_version"`
}

// RegisterContext takes the context the agent actually obtained, ranks it,
// fits it into the budget and returns keep/drop/reuse decisions
// (`dwyt_register_context`). This is where "reuse before retrieve" and the
// context GC actually bite.
func (g *Governor) RegisterContext(req RegisterRequest) RegisterResponse {
	taskID := req.TaskID
	if taskID == "" {
		taskID = "default"
	}
	cfg := g.Config()
	sess := g.session(taskID, contextgov.ComputeBudget(contextgov.BudgetProfile{
		DefaultBudget:  cfg.DefaultBudget,
		MaxBudget:      cfg.MaxBudget,
		ReservePercent: cfg.ReservePercent,
	}))

	candidates := make([]contextgov.ContextCandidate, 0, len(req.Items))
	deltaReason := map[string]string{}

	for _, item := range req.Items {
		c := contextgov.ContextCandidate{
			ID:          item.ID,
			Kind:        contextgov.ParseKind(item.Kind),
			Title:       item.Title,
			Tokens:      item.Tokens,
			Relevance:   item.Relevance,
			Confidence:  item.Confidence,
			State:       contextgov.ParseState(item.State),
			ContentRef:  firstNonEmpty(item.ContentRef, item.Path),
			ContentHash: item.ContentHash,
			Source:      item.Source,
			Pinned:      item.Pinned,
		}
		if item.CacheClass != "" {
			c.CacheClass = contextgov.ParseCacheClass(item.CacheClass)
		}
		if c.ContentHash == "" && item.Content != "" {
			c.ContentHash = contextgov.HashContent(item.Content)
		}
		if c.Tokens == 0 && item.Content != "" {
			c.Tokens = contextgov.EstimateTokens(item.Content)
		}
		c.Normalize()

		// Feed the delta store so the next turn can reuse instead of retrieve.
		if item.Path != "" {
			decision, _ := sess.ObserveSymbol(contextgov.SymbolState{
				Path:         item.Path,
				Symbol:       item.Symbol,
				ContentHash:  c.ContentHash,
				Tokens:       c.Tokens,
				ChangedRange: item.ChangedRange,
			})
			if decision == contextgov.DeltaReuse {
				deltaReason[c.ID] = "unchanged since last seen"
			}
		}
		candidates = append(candidates, c)
	}

	ranked := contextgov.Rank(candidates, sess.RankInput(contextgov.DefaultCostModel()))
	sel := contextgov.Select(ranked, sess.Budget())

	resp := RegisterResponse{
		TaskID:        taskID,
		Budget:        sess.Budget(),
		PolicyVersion: PolicyVersion,
	}
	for _, sc := range sel.Included {
		resp.Decisions = append(resp.Decisions, RegisterDecision{
			ID: sc.Candidate.ID, Decision: "keep", Tokens: sc.Candidate.Tokens,
		})
		resp.Order = append(resp.Order, sc.Candidate.ID)
	}
	for _, sc := range sel.Reused {
		reason := deltaReason[sc.Candidate.ID]
		if reason == "" {
			reason = "already delivered in this session"
		}
		resp.Decisions = append(resp.Decisions, RegisterDecision{
			ID: sc.Candidate.ID, Decision: "reuse", Tokens: sc.Candidate.Tokens, Reason: reason,
		})
	}
	for _, sc := range sel.Excluded {
		resp.Decisions = append(resp.Decisions, RegisterDecision{
			ID: sc.Candidate.ID, Decision: "drop", Tokens: sc.Candidate.Tokens,
			Reason: "over budget; lowest token ROI",
		})
	}
	resp.TokensKept = sel.TokensIncluded
	resp.TokensDropped = sel.TokensExcluded
	for _, sc := range sel.Reused {
		resp.TokensReused += sc.Candidate.Tokens
	}

	// Record what was actually delivered so the next turn treats it as seen.
	delivered := make([]contextgov.ContextCandidate, 0, len(sel.Included))
	for _, sc := range sel.Included {
		delivered = append(delivered, sc.Candidate)
	}
	sess.RegisterDelivered(delivered)

	// Progressive expansion: when the drop was caused purely by the budget and
	// expansion is enabled, grant more room rather than silently losing
	// context the agent already paid to retrieve.
	if cfg.ProgressiveExpansion && resp.TokensDropped > 0 {
		if _, granted := sess.ExpandBudget(resp.TokensDropped); granted > 0 {
			resp.Budget = sess.Budget()
			resp.Decisions = append(resp.Decisions, RegisterDecision{
				ID: "", Decision: "budget_expanded", Tokens: granted,
				Reason: "progressive expansion granted for dropped high-ROI context",
			})
		}
	}
	return resp
}

// OutputProfile answers `dwyt_output_profile`.
func (g *Governor) OutputProfile(taskType, phase string) outputgov.Profile {
	cfg := g.Config()
	p := outputgov.ProfileForTaskType(taskType, contextgov.Phase(normalizePhase(phase)))
	if !cfg.StructuredOperational {
		p.Structured = false
	}
	if !p.ArtifactException && cfg.OperationalOutputMax > 0 && p.MaxTokens > cfg.OperationalOutputMax &&
		p.Phase != contextgov.PhaseReview && p.Phase != contextgov.PhasePlan {
		p.MaxTokens = cfg.OperationalOutputMax
	}
	return p
}

// normalizePhase returns "" for an empty string so ProfileForTaskType falls
// back to inferring the phase from the task type.
func normalizePhase(phase string) string {
	if phase == "" {
		return ""
	}
	return string(contextgov.ParsePhase(phase))
}

// CompactRequest is the input to `dwyt_compact_tool_output`.
type CompactRequest struct {
	TaskID  string `json:"task_id,omitempty"`
	Content string `json:"content,omitempty"`
	RawRef  string `json:"raw_ref,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Label   string `json:"label,omitempty"`
	// AlreadyCompact signals the producer (RTK) already reduced the output.
	AlreadyCompact bool `json:"already_compact,omitempty"`
}

// CompactToolOutput reduces tool output and persists the raw bytes by
// reference (`dwyt_compact_tool_output`).
//
// It never fails the caller: if the raw store is unavailable the compaction
// still happens, and the response says so, because "we could not archive the
// raw bytes" must not turn into "you get no tool output".
func (g *Governor) CompactToolOutput(req CompactRequest) (toolgov.Compacted, error) {
	content := req.Content
	store := g.RawStore()

	if content == "" && req.RawRef != "" {
		if store == nil {
			return toolgov.Compacted{}, fmt.Errorf("raw store unavailable: cannot resolve %s", req.RawRef)
		}
		resolved, _, err := store.Get(req.RawRef)
		if err != nil {
			return toolgov.Compacted{}, err
		}
		content = resolved
	}
	if content == "" {
		return toolgov.Compacted{}, fmt.Errorf("content or raw_ref is required")
	}

	compacted := toolgov.Compact(content, toolgov.Options{AlreadyCompact: req.AlreadyCompact})

	if store != nil {
		kind := req.Kind
		if kind == "" {
			kind = "tool_output"
		}
		meta, err := store.Put(content, rawstore.PutOptions{
			Kind:  kind,
			Label: req.Label,
			TTL:   g.Config().RawTTL,
		})
		if err == nil {
			compacted.RawRef = meta.Ref()
			if req.TaskID != "" {
				if sess, ok := g.Session(req.TaskID); ok {
					sess.AddRawRef(meta.Ref())
				}
			}
		}
	}
	// Recompute the sent estimate: the raw reference is part of what is sent.
	compacted.SentTokensEst = contextgov.EstimateTokens(compacted.Render())
	if compacted.RawTokensEst > 0 {
		saved := compacted.RawTokensEst - compacted.SentTokensEst
		if saved < 0 {
			saved = 0
		}
		compacted.CompressionPct = float64(saved) / float64(compacted.RawTokensEst) * 100
	}
	return compacted, nil
}

// GetRaw resolves a raw reference (`dwyt_get_raw`).
func (g *Governor) GetRaw(ref string) (string, rawstore.Meta, error) {
	store := g.RawStore()
	if store == nil {
		return "", rawstore.Meta{}, fmt.Errorf("raw store unavailable")
	}
	return store.Get(ref)
}

// PutRaw archives content and returns its reference. Used by the tool proxy and
// by the brain when a note needs to shed a large payload.
func (g *Governor) PutRaw(content, kind, label string) (rawstore.Meta, error) {
	store := g.RawStore()
	if store == nil {
		return rawstore.Meta{}, fmt.Errorf("raw store unavailable")
	}
	return store.Put(content, rawstore.PutOptions{Kind: kind, Label: label, TTL: g.Config().RawTTL})
}

// Usage is a normalized per-request usage report (spec §52). Fields that a
// provider did not report stay nil so "unknown" is distinguishable from zero —
// reporting an unknown cached-token count as 0 would understate cache
// effectiveness and overstate cost.
type Usage struct {
	TaskID   string `json:"task_id,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Phase    string `json:"phase,omitempty"`

	InputTokens         *int `json:"input_tokens,omitempty"`
	UncachedInputTokens *int `json:"uncached_input_tokens,omitempty"`
	CachedInputTokens   *int `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens    *int `json:"cache_write_tokens,omitempty"`
	OutputTokens        *int `json:"output_tokens,omitempty"`
	ReasoningTokens     *int `json:"reasoning_tokens,omitempty"`
	ToolTokens          *int `json:"tool_tokens,omitempty"`

	ContextBeforeDWYT *int `json:"context_before_dwyt,omitempty"`
	ContextAfterDWYT  *int `json:"context_after_dwyt,omitempty"`

	EstimatedCostUSD *float64 `json:"estimated_cost_usd,omitempty"`
	ActualCostUSD    *float64 `json:"actual_cost_usd,omitempty"`

	LatencyMS *int `json:"latency_ms,omitempty"`

	CacheKeyHash string `json:"cache_key_hash,omitempty"`
	PrefixHash   string `json:"prefix_hash,omitempty"`

	// Observed is true when the numbers came from the provider rather than
	// from a DWYT estimate (spec §39, §52). Never set it for an estimate.
	Observed bool `json:"observed"`

	// CachedHashes are the prefix hashes the provider confirmed as cache
	// reads; they feed the ranker's cost model.
	CachedHashes []string `json:"cached_hashes,omitempty"`

	// Loop counters, when the caller tracks them.
	ToolIterations int  `json:"tool_iterations,omitempty"`
	BuildFailed    bool `json:"build_failed,omitempty"`

	Timestamp time.Time `json:"timestamp,omitempty"`
}

// UsageResponse is the answer to `dwyt_report_usage`.
type UsageResponse struct {
	Accepted bool                    `json:"accepted"`
	Stop     contextgov.StopDecision `json:"stop"`
	// Recorded is false when no telemetry sink is wired; the governor still
	// uses the report to update its own state.
	Recorded bool   `json:"recorded"`
	Note     string `json:"note,omitempty"`
}

// ReportUsage ingests a usage report (`dwyt_report_usage`). It updates the
// session's observed cache set and the loop counters, then re-evaluates the
// stop conditions.
func (g *Governor) ReportUsage(u Usage) UsageResponse {
	if u.Timestamp.IsZero() {
		u.Timestamp = time.Now()
	}
	taskID := u.TaskID
	if taskID == "" {
		taskID = "default"
	}

	if sess, ok := g.Session(taskID); ok && u.Observed && len(u.CachedHashes) > 0 {
		sess.MarkCached(u.CachedHashes)
	}

	g.mu.Lock()
	obs := g.loop[taskID]
	if obs == nil {
		obs = &contextgov.LoopObservation{}
		g.loop[taskID] = obs
	}
	if u.ToolIterations > 0 {
		obs.ToolIterations += u.ToolIterations
	}
	if u.BuildFailed {
		obs.FailedBuilds++
	}
	if u.InputTokens != nil {
		obs.TokensUsed += *u.InputTokens
	}
	if u.OutputTokens != nil {
		obs.TokensUsed += *u.OutputTokens
	}
	switch {
	case u.ActualCostUSD != nil:
		obs.CostUSD += *u.ActualCostUSD
	case u.EstimatedCostUSD != nil:
		obs.CostUSD += *u.EstimatedCostUSD
	}
	recorder := g.usage
	g.mu.Unlock()

	resp := UsageResponse{Accepted: true, Stop: g.evaluateStop(taskID)}
	if recorder == nil {
		resp.Note = "no telemetry sink configured; report used for governance only"
		return resp
	}
	if err := recorder.RecordUsage(u); err != nil {
		// Telemetry is observability, not correctness. A failed write is
		// reported but never rejects the report.
		resp.Note = "telemetry write failed: " + err.Error()
		return resp
	}
	resp.Recorded = true
	return resp
}

// evaluateStop applies the configured stop limits to a task's measured loop.
func (g *Governor) evaluateStop(taskID string) contextgov.StopDecision {
	g.mu.RLock()
	limits := g.cfg.StopLimits
	obs := contextgov.LoopObservation{}
	if o, ok := g.loop[taskID]; ok {
		obs = *o
	}
	sess := g.sessions[taskID]
	g.mu.RUnlock()
	if sess != nil {
		obs.Expansions = sess.Expansions()
		if maxSame := maxErrorCount(sess); maxSame > obs.MaxSameError {
			obs.MaxSameError = maxSame
		}
	}
	return contextgov.EvaluateStop(limits, obs)
}

func maxErrorCount(sess *contextgov.Session) int {
	max := 0
	for _, e := range sess.State().ActiveErrors {
		if e.Count > max {
			max = e.Count
		}
	}
	return max
}

// HousekeeperStatus answers `dwyt_housekeeper_status`.
func (g *Governor) HousekeeperStatus() map[string]interface{} {
	g.mu.RLock()
	provider := g.housekeeper
	g.mu.RUnlock()
	if provider == nil {
		return map[string]interface{}{"enabled": false, "reason": "housekeeper not wired"}
	}
	return provider.HousekeeperStatus()
}

// MemoryHealth answers `dwyt_memory_health`.
func (g *Governor) MemoryHealth() map[string]interface{} {
	g.mu.RLock()
	provider := g.memory
	g.mu.RUnlock()
	if provider == nil {
		return map[string]interface{}{"available": false, "reason": "brain not wired"}
	}
	return provider.MemoryHealth()
}

// RawUsage reports the raw object store footprint for the dashboard.
func (g *Governor) RawUsage() map[string]interface{} {
	store := g.RawStore()
	if store == nil {
		return map[string]interface{}{"enabled": false}
	}
	count, bytes, err := store.Usage()
	if err != nil {
		return map[string]interface{}{"enabled": true, "error": err.Error()}
	}
	return map[string]interface{}{
		"enabled": true,
		"objects": count,
		"bytes":   bytes,
		"dir":     store.Dir(),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// CacheGuidanceResponse is the answer to `dwyt_cache_guidance` (spec §37–§39).
//
// The structural half — the assembly order and the "never put this before the
// prefix" list — is provider-independent and always authoritative. The
// capability half depends on the provider and is reported with an explicit
// state so DWYT never claims control it does not have.
type CacheGuidanceResponse struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`

	Order          []contextgov.CacheClass `json:"order"`
	PreservePrefix bool                    `json:"preserve_prefix"`
	// NeverBeforePrefix lists the content that must never precede the stable
	// prefix, because doing so destroys cache reuse for the whole request
	// (spec §41).
	NeverBeforePrefix []string `json:"never_before_prefix"`
	// TrimOrder is the order in which content must be removed when the context
	// has to shrink, so a productive cached prefix is the last casualty
	// (spec §43).
	TrimOrder []string `json:"trim_order"`

	// CapabilityState is one of enforced, advised, observed, unsupported,
	// unknown (spec §39).
	CapabilityState string `json:"capability_state"`
	// Note explains the capability state in one line.
	Note string `json:"note,omitempty"`

	PolicyVersion string `json:"policy_version"`
}

// CacheGuidance returns the prompt-assembly guidance for a provider/model.
//
// In v5.0.0 DWYT does not own the transport for third-party clients (the IDE or
// harness sends the request), so the honest capability state is "advised": DWYT
// shapes the prompt but does not set provider cache headers. Reporting
// "enforced" here would be exactly the false claim the spec forbids (§39).
func (g *Governor) CacheGuidance(provider, model string) CacheGuidanceResponse {
	resp := CacheGuidanceResponse{
		Provider:       provider,
		Model:          model,
		Order:          contextgov.CacheClasses(),
		PreservePrefix: g.Config().PreserveCachePrefix,
		NeverBeforePrefix: []string{
			"timestamps", "request_ids", "nonces", "build_ids",
			"latest_user_message", "latest_tool_result", "volatile_metadata",
		},
		TrimOrder: []string{
			"discardable", "stale", "resolved_raw", "duplicate",
			"volatile_excess", "low_roi", "stable_last_resort",
		},
		CapabilityState: CapabilityAdvised,
		Note: "DWYT shapes prompt order and content; it does not control the " +
			"provider request for third-party clients. Cache hits are only " +
			"reported when the provider observes them.",
		PolicyVersion: PolicyVersion,
	}
	return resp
}

// Capability states (spec §39). Exported so the dashboard and the provider
// adapters share one vocabulary.
const (
	CapabilityEnforced    = "enforced"
	CapabilityAdvised     = "advised"
	CapabilityObserved    = "observed"
	CapabilityUnsupported = "unsupported"
	CapabilityUnknown     = "unknown"
)
