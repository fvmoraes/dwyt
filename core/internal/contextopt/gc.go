package contextopt

import "sort"

// Context Garbage Collector (spec §13, §43).
//
// The GC never cuts by score alone. It cuts in a fixed preference order so a
// budget overrun is absorbed by noise before it is absorbed by knowledge, and
// so a cacheable prefix that is currently producing cache hits is the *last*
// thing to be sacrificed (spec §43: "minimum expected cost, not minimum raw
// token count at any cost").

// TrimReason explains why the GC dropped a candidate. Surfacing it keeps the
// optimizer auditable: a user who loses context can see which rule removed it.
type TrimReason string

const (
	ReasonDiscardable      TrimReason = "discardable"
	ReasonStale            TrimReason = "stale"
	ReasonResolvedRaw      TrimReason = "resolved_raw"
	ReasonDuplicate        TrimReason = "duplicate"
	ReasonVolatileExcess   TrimReason = "volatile_excess"
	ReasonLowROI           TrimReason = "low_roi"
	ReasonStableLastResort TrimReason = "stable_last_resort"
)

// Trimmed records one removal.
type Trimmed struct {
	Candidate ContextCandidate `json:"candidate"`
	Reason    TrimReason       `json:"reason"`
	Tokens    int              `json:"tokens"`
}

// GCResult is the outcome of a trim pass.
type GCResult struct {
	Kept    []ContextCandidate `json:"kept"`
	Removed []Trimmed          `json:"removed"`
	// Compacted are non-critical candidates that were not dropped but replaced
	// by a one-line reference (spec §13: resolved context remains useful).
	Compacted     []ContextCandidate `json:"compacted,omitempty"`
	TokensBefore  int                `json:"tokens_before"`
	TokensAfter   int                `json:"tokens_after"`
	TokensRemoved int                `json:"tokens_removed"`
	TargetMet     bool               `json:"target_met"`
}

// trimTier is one pass of the cut order. Candidates matching the predicate are
// eligible for removal in this tier, cheapest-value-first.
type trimTier struct {
	reason TrimReason
	match  func(ContextCandidate) bool
}

// trimOrder is the normative cut order from Fase 6. Exact duplicates are
// removed only after resolved/raw content, so GCResult exposes the reason and
// sequence required to explain a budget decision.
func trimOrder() []trimTier {
	return []trimTier{
		{ReasonDiscardable, func(c ContextCandidate) bool {
			return c.State == StateDiscardable
		}},
		{ReasonStale, func(c ContextCandidate) bool {
			return c.State == StateStale
		}},
		{ReasonResolvedRaw, func(c ContextCandidate) bool {
			// Resolved content, and any raw log regardless of state: raw is
			// recoverable by reference and must not occupy the window.
			return c.State == StateResolved || c.Kind == KindRawLog
		}},
		{ReasonDuplicate, nil},
		{ReasonVolatileExcess, func(c ContextCandidate) bool {
			return c.CacheClass == CacheVolatile
		}},
		{ReasonLowROI, func(c ContextCandidate) bool {
			// WARM/COLD content that is neither immutable nor long-lived.
			return c.Temperature != Hot &&
				c.CacheClass != CacheImmutable &&
				c.CacheClass != CacheLongLived
		}},
		{ReasonStableLastResort, func(c ContextCandidate) bool {
			// Everything else, including cacheable content. Reaching this
			// tier means the budget genuinely cannot hold the stable prefix.
			return true
		}},
	}
}

// Trim reduces a candidate set until its token total fits target, following
// the normative cut order. Critical evidence (errors, constraints, and pinned
// context) is never removed or compacted, even if that leaves TargetMet false.
func Trim(candidates []ContextCandidate, target int, in RankInput) GCResult {
	res := GCResult{}
	working := make([]ContextCandidate, 0, len(candidates))
	for _, c := range candidates {
		c.Normalize()
		working = append(working, c)
		res.TokensBefore += c.Tokens
	}

	total := tokenSum(working)
	if total <= target {
		res.Kept = working
		res.TokensAfter = total
		res.TargetMet = true
		return res
	}

	for _, tier := range trimOrder() {
		if total <= target {
			break
		}
		// Within a tier, drop the worst ROI first so the cut is defensible.
		var idx []int
		if tier.reason == ReasonDuplicate {
			idx = duplicateIndexes(working)
		} else {
			idx = eligibleIndexes(working, tier.match)
		}
		sort.SliceStable(idx, func(a, b int) bool {
			sa := Score(working[idx[a]], in)
			sb := Score(working[idx[b]], in)
			if sa.Score != sb.Score {
				return sa.Score < sb.Score
			}
			// Larger candidates first among equals: fewer removals to reach
			// the target.
			return working[idx[a]].Tokens > working[idx[b]].Tokens
		})

		drop := make(map[int]bool, len(idx))
		for _, i := range idx {
			if total <= target {
				break
			}
			c := working[i]
			// A resolved candidate becomes a one-line reference instead of
			// vanishing, so the agent still knows the problem was handled.
			if tier.reason == ReasonResolvedRaw && c.State == StateResolved && c.Tokens > compactedLineTokens {
				compact := c
				compact.Tokens = compactedLineTokens
				compact.State = StateReference
				compact.Kind = KindSessionState
				working[i] = compact
				res.Compacted = append(res.Compacted, compact)
				total -= c.Tokens - compactedLineTokens
				res.TokensRemoved += c.Tokens - compactedLineTokens
				continue
			}
			drop[i] = true
			total -= c.Tokens
			res.TokensRemoved += c.Tokens
			res.Removed = append(res.Removed, Trimmed{Candidate: c, Reason: tier.reason, Tokens: c.Tokens})
		}
		if len(drop) > 0 {
			next := make([]ContextCandidate, 0, len(working)-len(drop))
			for i, c := range working {
				if !drop[i] {
					next = append(next, c)
				}
			}
			working = next
		}
	}

	res.Kept = working
	res.TokensAfter = tokenSum(working)
	res.TargetMet = res.TokensAfter <= target
	return res
}

// compactedLineTokens is the size a resolved item is compacted down to: enough
// for "resolved: TS2345 in ResourceTable" and no more.
const compactedLineTokens = 12

// duplicateIndexes finds redundant non-critical candidates at the duplicate
// tier. Critical evidence is intentionally not entered into seenHash: a
// matching non-critical candidate may carry a different useful representation.
func duplicateIndexes(candidates []ContextCandidate) []int {
	seenHash := make(map[string]bool, len(candidates))
	var out []int
	for i, c := range candidates {
		if c.ContentHash == "" || isCriticalEvidence(c) {
			continue
		}
		if seenHash[c.ContentHash] {
			out = append(out, i)
			continue
		}
		seenHash[c.ContentHash] = true
	}
	return out
}

func eligibleIndexes(candidates []ContextCandidate, match func(ContextCandidate) bool) []int {
	var out []int
	for i, c := range candidates {
		if isCriticalEvidence(c) {
			continue
		}
		if match(c) {
			out = append(out, i)
		}
	}
	return out
}

// isCriticalEvidence implements Optimizer Law 7 with the current candidate
// taxonomy. There is no KindFailure yet; failures must arrive as KindError or
// be pinned until a more precise kind is introduced.
func isCriticalEvidence(c ContextCandidate) bool {
	return c.Pinned || c.Kind == KindError || c.Kind == KindConstraint
}

func tokenSum(candidates []ContextCandidate) int {
	total := 0
	for _, c := range candidates {
		total += c.Tokens
	}
	return total
}

// InvalidationEvent names something that happened in the session and makes
// previously-collected context obsolete (spec §13).
type InvalidationEvent string

const (
	EventBuild       InvalidationEvent = "build"
	EventTestRun     InvalidationEvent = "test_run"
	EventErrorFixed  InvalidationEvent = "error_resolved"
	EventFileChanged InvalidationEvent = "file_changed"
	EventCommit      InvalidationEvent = "commit"
	EventPhaseEnd    InvalidationEvent = "phase_end"
)

// Invalidate applies an event's invalidation rules to a candidate set,
// returning the updated states. It changes states rather than deleting, so the
// GC (and the caller's diagnostics) can still see what happened and why.
//
// scope narrows the effect: for EventFileChanged it is the changed path, so
// only summaries derived from that file go stale. An empty scope means the
// event applies to every candidate of the affected kinds.
func Invalidate(candidates []ContextCandidate, event InvalidationEvent, scope string) []ContextCandidate {
	out := make([]ContextCandidate, len(candidates))
	copy(out, candidates)
	for i := range out {
		c := &out[i]
		if c.Pinned {
			continue
		}
		switch event {
		case EventBuild, EventTestRun:
			// A newer run supersedes the previous equivalent output.
			if c.Kind == KindToolOutput || c.Kind == KindRawLog {
				if scope == "" || c.Source == scope || c.ContentRef == scope {
					c.State = StateStale
				}
			}
		case EventErrorFixed:
			if c.Kind == KindError && (scope == "" || c.ID == scope || c.Title == scope) {
				c.State = StateResolved
			}
		case EventFileChanged:
			// Summaries derived from a changed file can no longer be trusted.
			if scope != "" && c.ContentRef != scope {
				continue
			}
			switch c.Kind {
			case KindFileSummary, KindModuleSummary, KindSymbol, KindRange, KindFullFile:
				c.State = StateStale
			}
		case EventCommit:
			if c.Kind == KindDiff {
				c.State = StateStale
			}
		case EventPhaseEnd:
			// Everything volatile from the finished phase is superseded by the
			// compact snapshot the phase produced.
			if c.CacheClass == CacheVolatile && c.State == StateActive {
				c.State = StateResolved
			}
		}
		c.Temperature = DefaultTemperature(c.Kind, c.State)
	}
	return out
}
