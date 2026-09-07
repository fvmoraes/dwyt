// Package cacheintel builds cache-friendly prompts and diagnoses cache misses
// (spec §37–§43).
//
// The single most valuable thing DWYT can do for prompt caching is boring:
// assemble context in a fixed order, IMMUTABLE → LONG_LIVED → SESSION →
// VOLATILE, and never let a timestamp or request id in front of the stable
// prefix. That is worth more than any clever compression, because a broken
// prefix invalidates the cache for everything after it.
//
// The second most valuable thing is refusing to lie: a cache hit is only real
// when the provider reports one (spec §39).
package cacheintel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/fvmoraes/dwyt/internal/contextopt"
)

// Block is one span of prompt content with its cache class.
type Block struct {
	ID    string                `json:"id"`
	Class contextopt.CacheClass `json:"class"`
	// Content is the text of the block.
	Content string `json:"content,omitempty"`
	// Hash identifies the content. Computed from Content when empty.
	Hash string `json:"hash,omitempty"`
	// Tokens is the estimated size.
	Tokens int `json:"tokens,omitempty"`
}

// normalize fills in derived fields.
func (b *Block) normalize() {
	if b.Class == "" {
		// Unknown class means volatile: that keeps it *behind* the stable prefix,
		// which is the safe direction to be wrong in.
		b.Class = contextopt.CacheVolatile
	}
	if b.Hash == "" && b.Content != "" {
		b.Hash = contextopt.HashContent(b.Content)
	}
	if b.Tokens == 0 && b.Content != "" {
		b.Tokens = contextopt.EstimateTokens(b.Content)
	}
}

// volatileMarkers are substrings whose presence makes content unfit for a stable
// prefix (spec §41). They are the things that change on every request.
var volatileMarkers = []string{
	"timestamp", "request_id", "requestid", "request-id",
	"nonce", "build_id", "buildid", "correlation_id", "trace_id",
	"generated_at", "now:", "current time",
}

// Violation is a prompt-assembly problem found by the builder.
type Violation struct {
	BlockID string `json:"block_id"`
	Reason  string `json:"reason"`
	// Fixed reports whether the builder corrected it automatically.
	Fixed bool `json:"fixed"`
}

// Prompt is an assembled, cache-ordered prompt.
type Prompt struct {
	// Blocks in canonical assembly order.
	Blocks []Block `json:"blocks"`
	// StablePrefix is the concatenation of the immutable and long-lived blocks:
	// the span a provider can reuse across requests.
	StablePrefix string `json:"-"`
	// PrefixHash identifies the stable prefix, for miss diagnostics.
	PrefixHash string `json:"prefix_hash"`
	// ClassHashes carries one hash per cache class, so a diagnostic can point at
	// *which* class changed.
	ClassHashes map[contextopt.CacheClass]string `json:"class_hashes"`
	// PrefixTokens is the size of the reusable prefix.
	PrefixTokens int `json:"prefix_tokens"`
	// TotalTokens is the size of the whole prompt.
	TotalTokens int `json:"total_tokens"`
	// Violations lists ordering problems that were found (and fixed).
	Violations []Violation `json:"violations,omitempty"`
}

// Build assembles blocks in canonical cache order.
//
// Blocks that claim a stable class but carry volatile markers are demoted to
// volatile and reported. Demoting is the right correction: leaving a timestamp in
// the "immutable" span would silently destroy the cache for every request, while
// moving it to the tail costs nothing.
func Build(blocks []Block) Prompt {
	p := Prompt{ClassHashes: map[contextopt.CacheClass]string{}}

	working := make([]Block, 0, len(blocks))
	for _, b := range blocks {
		b.normalize()
		if isStable(b.Class) && hasVolatileMarker(b.Content) {
			p.Violations = append(p.Violations, Violation{
				BlockID: b.ID,
				Reason:  "volatile metadata in a stable block; demoted to volatile so the prefix stays reusable",
				Fixed:   true,
			})
			b.Class = contextopt.CacheVolatile
		}
		working = append(working, b)
	}

	// Stable sort by class rank preserves the caller's order inside a class,
	// which matters: an immutable prefix must be byte-identical across requests,
	// and reordering within a class would break it just as surely as reordering
	// across classes.
	sort.SliceStable(working, func(i, j int) bool {
		return contextopt.CacheClassRank(working[i].Class) < contextopt.CacheClassRank(working[j].Class)
	})
	p.Blocks = working

	var stable strings.Builder
	perClass := map[contextopt.CacheClass]*strings.Builder{}
	for _, b := range working {
		p.TotalTokens += b.Tokens
		if perClass[b.Class] == nil {
			perClass[b.Class] = &strings.Builder{}
		}
		perClass[b.Class].WriteString(b.Hash)
		perClass[b.Class].WriteByte(0)
		if isStable(b.Class) {
			stable.WriteString(b.Content)
			p.PrefixTokens += b.Tokens
		}
	}
	for class, builder := range perClass {
		p.ClassHashes[class] = shortHash(builder.String())
	}
	p.StablePrefix = stable.String()
	p.PrefixHash = shortHash(p.StablePrefix)
	return p
}

func isStable(c contextopt.CacheClass) bool {
	return c == contextopt.CacheImmutable || c == contextopt.CacheLongLived
}

func hasVolatileMarker(content string) bool {
	if content == "" {
		return false
	}
	lower := strings.ToLower(content)
	for _, marker := range volatileMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// CacheIdentity is the stable, non-sensitive identity of a cacheable prompt
// (spec §42).
//
// It deliberately excludes absolute paths, user names, timestamps and random
// ids: a cache key is sent to a third party, so it must not carry anything that
// identifies the machine or the person.
type CacheIdentity struct {
	Provider string `json:"provider,omitempty"`
	// Key is the value to send as a provider cache key, when supported.
	Key string `json:"key"`
	// PrefixHash, SystemHash, ToolSchemaHash and ProjectContextHash let a miss
	// be attributed to a specific span.
	PrefixHash         string `json:"prefix_hash,omitempty"`
	SystemHash         string `json:"system_hash,omitempty"`
	ToolSchemaHash     string `json:"tool_schema_hash,omitempty"`
	ProjectContextHash string `json:"project_context_hash,omitempty"`
	SessionPrefixHash  string `json:"session_prefix_hash,omitempty"`
}

// IdentityInput carries the raw material for a cache identity.
type IdentityInput struct {
	Provider string
	// ProjectHash is DWYT's project id (already a hash, never a path).
	ProjectHash string
	// PolicyVersion changes when the optimizer's rules change, because a prefix
	// built under different rules is not interchangeable.
	PolicyVersion  string
	System         string
	ToolSchema     string
	ProjectContext string
	SessionPrefix  string
}

// NewIdentity derives the cache identity.
func NewIdentity(in IdentityInput) CacheIdentity {
	id := CacheIdentity{
		Provider:           in.Provider,
		SystemHash:         optionalHash(in.System),
		ToolSchemaHash:     optionalHash(in.ToolSchema),
		ProjectContextHash: optionalHash(in.ProjectContext),
		SessionPrefixHash:  optionalHash(in.SessionPrefix),
	}
	id.PrefixHash = shortHash(strings.Join([]string{
		in.System, in.ToolSchema, in.ProjectContext,
	}, "\x00"))

	project := in.ProjectHash
	if project == "" {
		project = "unknown"
	}
	policy := in.PolicyVersion
	if policy == "" {
		policy = "unversioned"
	}
	// Format matches spec §42: dwyt:<project_hash>:<policy_version>:<tool_schema_hash>
	id.Key = fmt.Sprintf("dwyt:%s:%s:%s", project, policy, optionalHash(in.ToolSchema))
	return id
}

func optionalHash(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return shortHash(content)
}

// shortHash is a 16-hex-character digest: long enough to be collision-free for
// this purpose, short enough to read in a diagnostic.
func shortHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:16]
}

// Diagnosis explains a cache outcome (spec §39, §43).
type Diagnosis struct {
	// State is the honest capability/observation state.
	State string `json:"state"`
	// HitRatio is cached input over total input. Only set when observed.
	HitRatio *float64 `json:"hit_ratio,omitempty"`
	// PrefixStable reports whether the stable prefix is unchanged since the
	// previous request.
	PrefixStable bool `json:"prefix_stable"`
	// ChangedClasses names the cache classes whose hash changed.
	ChangedClasses []string `json:"changed_classes,omitempty"`
	// Findings are human-readable explanations, newest first.
	Findings []string `json:"findings,omitempty"`
}

// Observation is what happened on a request, as reported by the provider.
type Observation struct {
	// Reported is false when the provider gave no cache numbers. Everything
	// downstream must then say "unknown" rather than "0% hit".
	Reported          bool
	InputTokens       int
	CachedInputTokens int
	CacheWriteTokens  int
}

// Diagnose compares the current prompt with the previous one and folds in what
// the provider reported.
//
// previous may be the zero Prompt for the first request of a session.
func Diagnose(previous, current Prompt, obs Observation) Diagnosis {
	d := Diagnosis{State: "unknown"}

	if previous.PrefixHash != "" {
		d.PrefixStable = previous.PrefixHash == current.PrefixHash
		if !d.PrefixStable {
			d.Findings = append(d.Findings,
				"stable prefix changed: a cache miss on the reusable span is expected")
		}
		for _, class := range contextopt.CacheClasses() {
			if previous.ClassHashes[class] != current.ClassHashes[class] {
				d.ChangedClasses = append(d.ChangedClasses, string(class))
			}
		}
	} else {
		d.Findings = append(d.Findings, "first request of the session: nothing to reuse yet")
	}

	for _, v := range current.Violations {
		d.Findings = append(d.Findings, "ordering: "+v.Reason)
	}

	switch {
	case obs.Reported && obs.InputTokens > 0:
		ratio := float64(obs.CachedInputTokens) / float64(obs.InputTokens)
		d.HitRatio = &ratio
		d.State = "observed"
		if obs.CachedInputTokens == 0 && d.PrefixStable {
			// The prefix was byte-identical yet nothing was reused. That is a
			// provider- or transport-level cause, not a DWYT ordering problem,
			// and saying so prevents a pointless hunt through the prompt.
			d.Findings = append(d.Findings,
				"prefix unchanged but the provider reported no cached tokens: "+
					"caching may be unsupported for this model, below its minimum "+
					"cacheable size, or disabled by the client")
		}
	case obs.Reported:
		d.State = "observed"
		d.Findings = append(d.Findings, "provider reported usage without input tokens")
	default:
		d.State = "unknown"
		d.Findings = append(d.Findings,
			"provider did not report cache usage: DWYT will not claim a hit it cannot observe")
	}
	return d
}

// TrimOrder is the normative order in which content must be dropped when a
// prompt has to shrink (spec §43). Exported so the dashboard and the MCP share
// one vocabulary with the context optimizer.
func TrimOrder() []string {
	return []string{
		"discardable",
		"stale",
		"resolved_raw",
		"duplicate",
		"volatile_excess",
		"low_roi_warm_cold",
		"stable_last_resort",
	}
}

// LongContextPlan describes how to get under a pricing threshold (spec §44).
type LongContextPlan struct {
	Current   int `json:"current_tokens"`
	Threshold int `json:"threshold"`
	// NeedToRemove is zero when the prompt is already under the threshold.
	NeedToRemove int `json:"need_to_remove"`
	// Candidates are the blocks to drop, in the normative trim order, until the
	// requirement is met.
	Candidates []Block `json:"candidates,omitempty"`
	// Achievable is false when even dropping every eligible block is not enough,
	// so the caller must decide to cross the threshold knowingly.
	Achievable bool `json:"achievable"`
}

// PlanLongContext selects blocks to drop to get under a threshold.
//
// Only VOLATILE and SESSION blocks are offered: dropping the stable prefix to
// dodge a multiplier usually costs more than the multiplier itself, because it
// also forfeits every future cache hit (spec §43).
func PlanLongContext(p Prompt, threshold int) LongContextPlan {
	plan := LongContextPlan{Current: p.TotalTokens, Threshold: threshold}
	if threshold <= 0 || p.TotalTokens <= threshold {
		plan.Achievable = true
		return plan
	}
	plan.NeedToRemove = p.TotalTokens - threshold

	eligible := make([]Block, 0, len(p.Blocks))
	for _, b := range p.Blocks {
		if b.Class == contextopt.CacheVolatile || b.Class == contextopt.CacheSession {
			eligible = append(eligible, b)
		}
	}
	// Largest first: fewest blocks removed to reach the target.
	sort.SliceStable(eligible, func(i, j int) bool {
		ri := contextopt.CacheClassRank(eligible[i].Class)
		rj := contextopt.CacheClassRank(eligible[j].Class)
		if ri != rj {
			// Volatile (rank 3) before session (rank 2): drop the least reusable
			// content first.
			return ri > rj
		}
		return eligible[i].Tokens > eligible[j].Tokens
	})

	removed := 0
	for _, b := range eligible {
		if removed >= plan.NeedToRemove {
			break
		}
		plan.Candidates = append(plan.Candidates, b)
		removed += b.Tokens
	}
	plan.Achievable = removed >= plan.NeedToRemove
	return plan
}
