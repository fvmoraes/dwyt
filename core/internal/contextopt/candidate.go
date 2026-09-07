// Package contextopt implements the DWYT v5 Context Optimizer: the component
// that decides how much context an agent may load, from which sources, in
// which order, and what must be dropped first when the budget is exceeded.
//
// The optimizer is deliberately deterministic. Every decision it makes is a
// pure function of the candidate metadata it is given, so the same inputs
// always produce the same plan. That property is what lets DWYT optimize
// context without spending an extra LLM call to do it (spec §46: "prefer
// deterministic classification when sufficient").
package contextopt

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Kind classifies where a context candidate came from. It is used by the
// ranker (importance priors) and by the retrieval planner (which MCP should
// be asked for it).
type Kind string

const (
	KindProjectIdentity Kind = "project_identity"
	KindArchitecture    Kind = "architecture"
	KindDecision        Kind = "decision"
	KindConvention      Kind = "convention"
	KindConstraint      Kind = "constraint"
	KindModuleSummary   Kind = "module_summary"
	KindFileSummary     Kind = "file_summary"
	KindSymbol          Kind = "symbol"
	KindRange           Kind = "range"
	KindFullFile        Kind = "full_file"
	KindTest            Kind = "test"
	KindDependency      Kind = "dependency"
	KindDiff            Kind = "diff"
	KindToolOutput      Kind = "tool_output"
	KindError           Kind = "error"
	KindSessionState    Kind = "session_state"
	KindSessionHistory  Kind = "session_history"
	KindRawLog          Kind = "raw_log"
	KindPolicy          Kind = "policy"
	KindToolSchema      Kind = "tool_schema"
	KindUnknown         Kind = "unknown"
)

// State is the lifecycle state of a candidate (spec §6.1, §13). The garbage
// collector drops candidates in a fixed order derived from this field, so a
// resolved error is always discarded before a still-relevant architecture
// note, no matter how the scores compare.
type State string

const (
	// StateActive is context needed for the current decision.
	StateActive State = "active"
	// StateReference is durable knowledge consulted by summary.
	StateReference State = "reference"
	// StateResolved is context whose problem no longer exists; it can be
	// compacted to a single line.
	StateResolved State = "resolved"
	// StateStale is context superseded by a newer observation.
	StateStale State = "stale"
	// StateDiscardable is noise: banners, progress bars, debug dumps.
	StateDiscardable State = "discardable"
)

// CacheClass describes how stable a candidate is across requests (spec §37).
// The prompt is assembled in class order so a provider's prefix cache sees an
// identical prefix for as long as possible.
type CacheClass string

const (
	// CacheImmutable never changes for a given DWYT/policy version:
	// system instructions, the DWYT contract, tool schemas.
	CacheImmutable CacheClass = "immutable"
	// CacheLongLived changes rarely: canonical project memory, architecture,
	// conventions, stable constraints.
	CacheLongLived CacheClass = "long_lived"
	// CacheSession is stable within a task: compact task state, the plan,
	// relevant module summaries.
	CacheSession CacheClass = "session"
	// CacheVolatile changes on every turn: the latest message, diff, tool
	// result, error.
	CacheVolatile CacheClass = "volatile"
)

// cacheOrder is the canonical prompt assembly order. Index 0 goes first.
var cacheOrder = []CacheClass{CacheImmutable, CacheLongLived, CacheSession, CacheVolatile}

// CacheClassRank returns the position of a cache class in the canonical
// prompt order. Unknown classes sort last so an unrecognised value can never
// be injected ahead of a stable prefix (spec §41: never place dynamic
// metadata before the reusable prefix).
func CacheClassRank(c CacheClass) int {
	for i, known := range cacheOrder {
		if known == c {
			return i
		}
	}
	return len(cacheOrder)
}

// CacheClasses returns the canonical prompt assembly order.
func CacheClasses() []CacheClass {
	out := make([]CacheClass, len(cacheOrder))
	copy(out, cacheOrder)
	return out
}

// Temperature is the HOT/WARM/COLD retrieval priority of memory (spec §17).
type Temperature string

const (
	Hot  Temperature = "hot"
	Warm Temperature = "warm"
	Cold Temperature = "cold"
)

// ContextCandidate is a block that *may* enter the context window, described
// only by metadata. The optimizer never needs the content itself to make a
// decision — ContentRef points at where the content can be fetched from, and
// ContentHash lets a later turn detect "unchanged, do not resend" (spec §11).
type ContextCandidate struct {
	ID    string `json:"id"`
	Kind  Kind   `json:"kind"`
	Title string `json:"title,omitempty"`

	// Tokens is the estimated token cost of including this candidate.
	Tokens int `json:"tokens"`

	// Scoring signals, all normalized to [0,1]. Zero means "no signal",
	// which the ranker treats as a neutral default rather than as a veto —
	// a candidate with no metadata should still be rankable.
	Relevance  float64 `json:"relevance,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Recency    float64 `json:"recency,omitempty"`
	Dependency float64 `json:"dependency,omitempty"`
	Importance float64 `json:"importance,omitempty"`

	CacheClass  CacheClass  `json:"cache_class,omitempty"`
	State       State       `json:"state,omitempty"`
	Temperature Temperature `json:"temperature,omitempty"`

	// ContentRef locates the content without carrying it: a file path, a
	// vault note, a dwyt://objects/<hash> raw reference.
	ContentRef string `json:"content_ref,omitempty"`
	// ContentHash identifies the exact bytes so an unchanged candidate can
	// be reused instead of re-retrieved.
	ContentHash string `json:"content_hash,omitempty"`

	// Source names the MCP or subsystem that can produce the content
	// ("obsidian", "codebase", "tool", "session").
	Source string `json:"source,omitempty"`

	// Pinned candidates are never dropped by the garbage collector. Use it
	// for context whose absence would make the task unsafe (spec §7: "do not
	// automatically cut critical context to fit the number").
	Pinned bool `json:"pinned,omitempty"`
}

// Normalize fills in defaults for a candidate that arrived with partial
// metadata. It is idempotent and never lowers a value the caller set
// explicitly, so callers may supply as much or as little as they know.
func (c *ContextCandidate) Normalize() {
	if c.Kind == "" {
		c.Kind = KindUnknown
	}
	if c.State == "" {
		c.State = StateActive
	}
	if c.CacheClass == "" {
		c.CacheClass = DefaultCacheClass(c.Kind)
	}
	if c.Temperature == "" {
		c.Temperature = DefaultTemperature(c.Kind, c.State)
	}
	if c.Importance == 0 {
		c.Importance = DefaultImportance(c.Kind)
	}
	if c.Relevance == 0 {
		c.Relevance = 0.5
	}
	if c.Confidence == 0 {
		c.Confidence = 0.7
	}
	if c.Recency == 0 {
		c.Recency = 0.5
	}
	if c.Dependency == 0 {
		c.Dependency = 0.5
	}
	if c.Tokens < 0 {
		c.Tokens = 0
	}
	if c.ID == "" {
		c.ID = DeriveID(c)
	}
}

// DeriveID builds a stable synthetic ID for a candidate that arrived without
// one, so dedup and delta reuse still work. It hashes the identifying fields
// rather than the content, which may not be loaded yet.
func DeriveID(c *ContextCandidate) string {
	h := sha256.New()
	h.Write([]byte(string(c.Kind)))
	h.Write([]byte{0})
	h.Write([]byte(c.Source))
	h.Write([]byte{0})
	h.Write([]byte(c.ContentRef))
	h.Write([]byte{0})
	h.Write([]byte(c.Title))
	return "cc_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// DefaultCacheClass maps a kind to how stable it usually is. Callers may
// override it; this is only the prior used when the caller says nothing.
func DefaultCacheClass(k Kind) CacheClass {
	switch k {
	case KindPolicy, KindToolSchema:
		return CacheImmutable
	case KindProjectIdentity, KindArchitecture, KindDecision, KindConvention, KindConstraint:
		return CacheLongLived
	case KindModuleSummary, KindFileSummary, KindSymbol, KindSessionState, KindTest, KindDependency:
		return CacheSession
	case KindDiff, KindToolOutput, KindError, KindRawLog, KindRange, KindFullFile:
		return CacheVolatile
	default:
		return CacheSession
	}
}

// DefaultTemperature maps a kind/state pair to a retrieval priority
// (spec §17). Resolved and stale content is always COLD regardless of kind:
// it may still be searchable, but it must never be loaded automatically.
func DefaultTemperature(k Kind, s State) Temperature {
	switch s {
	case StateResolved, StateStale, StateDiscardable:
		return Cold
	}
	switch k {
	case KindProjectIdentity, KindConstraint, KindSessionState, KindError, KindDiff:
		return Hot
	case KindArchitecture, KindDecision:
		return Hot
	case KindModuleSummary, KindConvention, KindFileSummary, KindSymbol, KindTest, KindDependency:
		return Warm
	case KindSessionHistory, KindRawLog, KindToolOutput:
		return Cold
	default:
		return Warm
	}
}

// DefaultImportance is the structural importance prior for a kind. Canonical
// knowledge scores high because losing it silently changes behaviour; raw
// logs score low because they are recoverable by reference.
func DefaultImportance(k Kind) float64 {
	switch k {
	case KindPolicy, KindToolSchema:
		return 1.0
	case KindConstraint, KindProjectIdentity:
		return 0.95
	case KindDecision, KindArchitecture:
		return 0.9
	case KindSessionState, KindError:
		return 0.85
	case KindConvention:
		return 0.8
	case KindSymbol, KindRange, KindDiff:
		return 0.75
	case KindModuleSummary, KindFileSummary:
		return 0.7
	case KindTest, KindDependency:
		return 0.6
	case KindFullFile:
		return 0.5
	case KindToolOutput:
		return 0.4
	case KindSessionHistory:
		return 0.3
	case KindRawLog:
		return 0.1
	default:
		return 0.5
	}
}

// ParseKind maps a free-form string to a Kind, tolerating the hyphen/space
// variants that arrive from MCP arguments. Unknown values map to KindUnknown
// rather than erroring: an unrecognised kind must not break a context plan.
func ParseKind(s string) Kind {
	normalized := strings.ToLower(strings.TrimSpace(s))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch Kind(normalized) {
	case KindProjectIdentity, KindArchitecture, KindDecision, KindConvention,
		KindConstraint, KindModuleSummary, KindFileSummary, KindSymbol,
		KindRange, KindFullFile, KindTest, KindDependency, KindDiff,
		KindToolOutput, KindError, KindSessionState, KindSessionHistory,
		KindRawLog, KindPolicy, KindToolSchema:
		return Kind(normalized)
	}
	// Common aliases used by agents and by the older instruction files.
	switch normalized {
	case "project", "identity":
		return KindProjectIdentity
	case "adr", "decisions":
		return KindDecision
	case "module", "modules":
		return KindModuleSummary
	case "file", "summary":
		return KindFileSummary
	case "snippet", "lines":
		return KindRange
	case "log", "logs":
		return KindRawLog
	case "tests":
		return KindTest
	case "task", "task_state", "state":
		return KindSessionState
	case "session", "sessions", "history":
		return KindSessionHistory
	}
	return KindUnknown
}

// ParseState maps a free-form string to a State, defaulting to StateActive.
func ParseState(s string) State {
	switch State(strings.ToLower(strings.TrimSpace(s))) {
	case StateActive:
		return StateActive
	case StateReference:
		return StateReference
	case StateResolved:
		return StateResolved
	case StateStale:
		return StateStale
	case StateDiscardable:
		return StateDiscardable
	}
	return StateActive
}

// ParseCacheClass maps a free-form string to a CacheClass. An unrecognised
// value becomes CacheVolatile — the safe answer, because treating unknown
// content as volatile keeps it out of the stable prefix.
func ParseCacheClass(s string) CacheClass {
	switch CacheClass(strings.ToLower(strings.TrimSpace(strings.ReplaceAll(s, "-", "_")))) {
	case CacheImmutable:
		return CacheImmutable
	case CacheLongLived:
		return CacheLongLived
	case CacheSession:
		return CacheSession
	case CacheVolatile:
		return CacheVolatile
	}
	return CacheVolatile
}

// HashContent returns the canonical content hash used across DWYT v5 for
// delta reuse and stale detection: "sha256:<hex>".
func HashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// EstimateTokens approximates the token cost of a string without a
// tokenizer dependency. It uses the widely-used ~4 bytes/token heuristic with
// a whitespace-aware floor, because code and JSON tokenize denser than prose
// and undercounting would let the budgeter overshoot.
//
// The result is intentionally an estimate; callers that report it to the user
// must label it as such (spec §20: measure, and mark observed vs estimated).
func EstimateTokens(content string) int {
	if content == "" {
		return 0
	}
	byChars := (len(content) + 3) / 4
	byWords := len(strings.Fields(content))
	if byWords > byChars {
		return byWords
	}
	return byChars
}
