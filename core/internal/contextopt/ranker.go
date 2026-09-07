package contextopt

import (
	"sort"
	"strings"
)

// Token ROI ranking (spec §8). The optimizer optimizes useful information per
// *effective* token cost, not raw token count: a large block that a provider
// already has cached can be cheaper than a small block that would break the
// cached prefix.

// CostModel expresses the relative price of a token depending on how it will
// be billed. Values are multipliers, not currency: the ranker only needs the
// ratios. The real currency figures live in the pricing catalog.
type CostModel struct {
	// Uncached is the baseline (1.0 by definition).
	Uncached float64
	// CacheRead is what a cache hit costs relative to uncached input.
	// Providers that expose caching charge a fraction of the input price.
	CacheRead float64
	// CacheWrite is the one-off premium for populating a cache entry.
	CacheWrite float64
	// PrefixBreakPenalty is applied to a candidate that would be inserted
	// into an already-stable prefix, because doing so invalidates the cache
	// for everything after it (spec §43).
	PrefixBreakPenalty float64
}

// DefaultCostModel is the provider-agnostic fallback. The ratios follow the
// common industry shape (cache reads much cheaper than uncached input, cache
// writes slightly more expensive) without claiming any specific provider's
// prices, which change too often to hardcode (spec §45).
func DefaultCostModel() CostModel {
	return CostModel{
		Uncached:           1.0,
		CacheRead:          0.15,
		CacheWrite:         1.25,
		PrefixBreakPenalty: 2.0,
	}
}

// RankInput carries the state the ranker needs beyond the candidates
// themselves.
type RankInput struct {
	Cost CostModel
	// CachedHashes are content hashes the provider is believed to already
	// have in a reusable prefix. Membership makes a candidate much cheaper.
	CachedHashes map[string]bool
	// SeenHashes are hashes already delivered in this session. A seen,
	// unchanged candidate should be referenced rather than resent (spec §11).
	SeenHashes map[string]bool
	// PrefixStable indicates a stable prefix is currently being reused, so
	// inserting new immutable/long-lived content carries a break penalty.
	PrefixStable bool
}

// ScoredCandidate is a candidate with its computed ROI score attached.
type ScoredCandidate struct {
	Candidate ContextCandidate `json:"candidate"`
	Score     float64          `json:"score"`
	// EffectiveTokens is the cost-adjusted token count used as the score
	// denominator. Exposed for diagnostics so a surprising ranking can be
	// explained without re-deriving it.
	EffectiveTokens float64 `json:"effective_tokens"`
	// AlreadySeen marks a candidate whose content the session already has.
	AlreadySeen bool `json:"already_seen,omitempty"`
}

// cacheStabilityBonus rewards content that is cheap to keep across turns.
// Immutable content is worth keeping even when large; volatile content has no
// reuse value at all.
func cacheStabilityBonus(c CacheClass) float64 {
	switch c {
	case CacheImmutable:
		return 1.35
	case CacheLongLived:
		return 1.2
	case CacheSession:
		return 1.05
	default:
		return 1.0
	}
}

// stateWeight collapses lifecycle state into the numerator. Discardable and
// stale content is worth ~nothing; the GC will drop it, and the ranker should
// not float it above live context in the meantime.
func stateWeight(s State) float64 {
	switch s {
	case StateActive:
		return 1.0
	case StateReference:
		return 0.85
	case StateResolved:
		return 0.2
	case StateStale:
		return 0.05
	case StateDiscardable:
		return 0.01
	default:
		return 0.5
	}
}

// temperatureWeight implements HOT before WARM before COLD (spec §17) as a
// ranking prior rather than a hard gate, so a highly relevant COLD note can
// still win against an irrelevant HOT one.
func temperatureWeight(t Temperature) float64 {
	switch t {
	case Hot:
		return 1.0
	case Warm:
		return 0.75
	case Cold:
		return 0.35
	default:
		return 0.6
	}
}

// EffectiveTokenCost computes the cost-adjusted token count for a candidate.
// It never returns less than 1 so the score stays finite for zero-token
// candidates (a pinned marker, for instance).
func EffectiveTokenCost(c ContextCandidate, in RankInput) float64 {
	cost := in.Cost
	if cost.Uncached == 0 {
		cost = DefaultCostModel()
	}

	tokens := float64(c.Tokens)
	if tokens < 1 {
		tokens = 1
	}

	multiplier := cost.Uncached
	switch {
	case c.ContentHash != "" && in.CachedHashes[c.ContentHash]:
		// Already in a reusable prefix: a cache read.
		multiplier = cost.CacheRead
	case c.CacheClass == CacheImmutable || c.CacheClass == CacheLongLived:
		// Stable content that is not cached yet pays a cache write once, but
		// amortizes over subsequent turns — hence the write price rather than
		// a penalty.
		multiplier = cost.CacheWrite
	}

	// Inserting fresh stable content while a prefix is being reused breaks
	// that prefix. Volatile content appended at the tail does not.
	if in.PrefixStable && c.ContentHash != "" && !in.CachedHashes[c.ContentHash] {
		if c.CacheClass == CacheImmutable || c.CacheClass == CacheLongLived {
			multiplier *= cost.PrefixBreakPenalty
		}
	}

	effective := tokens * multiplier
	if effective < 1 {
		effective = 1
	}
	return effective
}

// Score computes the Token ROI of a single candidate (spec §8).
func Score(c ContextCandidate, in RankInput) ScoredCandidate {
	c.Normalize()

	numerator := c.Relevance *
		c.Confidence *
		clamp01(0.4+0.6*c.Recency) *
		clamp01(0.4+0.6*c.Dependency) *
		c.Importance *
		stateWeight(c.State) *
		temperatureWeight(c.Temperature) *
		cacheStabilityBonus(c.CacheClass)

	if c.Pinned {
		// Pinned candidates must sort above everything the GC may drop. A
		// large additive boost (not a multiplier) guarantees this even when
		// every other signal is weak.
		numerator += 100
	}

	effective := EffectiveTokenCost(c, in)
	seen := c.ContentHash != "" && in.SeenHashes[c.ContentHash]
	if seen && !c.Pinned {
		// Already delivered: resending it buys almost nothing. It stays
		// rankable (the agent may have dropped it) but sorts below new
		// information of comparable value.
		numerator *= 0.1
	}

	return ScoredCandidate{
		Candidate:       c,
		Score:           numerator / effective * 1000,
		EffectiveTokens: effective,
		AlreadySeen:     seen,
	}
}

// Rank scores and sorts candidates best-first. Ties break on token count
// (cheaper first) and then on ID, so the ordering is fully deterministic and
// reproducible across runs.
func Rank(candidates []ContextCandidate, in RankInput) []ScoredCandidate {
	scored := make([]ScoredCandidate, 0, len(candidates))
	for _, c := range candidates {
		scored = append(scored, Score(c, in))
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].Candidate.Tokens != scored[j].Candidate.Tokens {
			return scored[i].Candidate.Tokens < scored[j].Candidate.Tokens
		}
		return scored[i].Candidate.ID < scored[j].Candidate.ID
	})
	return scored
}

// Selection is the outcome of fitting ranked candidates into a budget.
type Selection struct {
	Included []ScoredCandidate `json:"included"`
	Excluded []ScoredCandidate `json:"excluded"`
	// Reused are candidates whose content the session already has: the agent
	// should reference them by ID/hash instead of re-retrieving (spec §11).
	Reused []ScoredCandidate `json:"reused,omitempty"`

	TokensIncluded int `json:"tokens_included"`
	TokensExcluded int `json:"tokens_excluded"`
	BudgetTotal    int `json:"budget_total"`
	BudgetUsable   int `json:"budget_usable"`
}

// Select fits candidates into the retrievable part of a budget, honouring the
// cache-order rule: the returned Included slice is sorted by cache class so
// the caller can concatenate it directly into a cache-friendly prompt
// (spec §37).
//
// Pinned candidates are always included even if that overshoots the budget:
// silently dropping context the caller declared essential would trade
// correctness for tokens, which the spec forbids (§7, §70).
func Select(ranked []ScoredCandidate, b Budget) Selection {
	sel := Selection{
		BudgetTotal:  b.Total,
		BudgetUsable: b.Retrievable(),
	}
	remaining := sel.BudgetUsable

	for _, sc := range ranked {
		switch {
		case sc.Candidate.Pinned:
			sel.Included = append(sel.Included, sc)
			sel.TokensIncluded += sc.Candidate.Tokens
			remaining -= sc.Candidate.Tokens
		case sc.AlreadySeen:
			// Reuse instead of resend. It costs no budget.
			sel.Reused = append(sel.Reused, sc)
		case sc.Candidate.Tokens <= remaining:
			sel.Included = append(sel.Included, sc)
			sel.TokensIncluded += sc.Candidate.Tokens
			remaining -= sc.Candidate.Tokens
		default:
			sel.Excluded = append(sel.Excluded, sc)
			sel.TokensExcluded += sc.Candidate.Tokens
		}
	}

	SortByCacheOrder(sel.Included)
	return sel
}

// SortByCacheOrder arranges candidates immutable → long-lived → session →
// volatile, preserving the ROI order inside each class. This is the assembly
// order a cache-aware prompt builder must use.
func SortByCacheOrder(items []ScoredCandidate) {
	sort.SliceStable(items, func(i, j int) bool {
		ri := CacheClassRank(items[i].Candidate.CacheClass)
		rj := CacheClassRank(items[j].Candidate.CacheClass)
		if ri != rj {
			return ri < rj
		}
		return items[i].Score > items[j].Score
	})
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizeToken(s string) string {
	out := strings.ToLower(strings.TrimSpace(s))
	out = strings.ReplaceAll(out, "-", "_")
	out = strings.ReplaceAll(out, " ", "_")
	return out
}
