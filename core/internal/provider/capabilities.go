// Package provider models what a given LLM provider/model can actually do
// (spec §38–§40, §44–§45).
//
// Two rules shape this package:
//
//  1. **Capability-driven, never hardcoded truth.** Model names and prices change
//     faster than releases, so the catalog is a *seed* plus dynamic discovery,
//     and an unknown provider gets a safe generic fallback rather than a guess.
//
//  2. **Honesty about control.** A capability DWYT cannot verify is reported as
//     `unknown`, and a cache hit is only ever `observed` when the provider said
//     so. Claiming enforcement DWYT does not have is the failure mode spec §39
//     singles out.
package provider

import (
	"strings"
	"time"
)

// CacheMode describes how a provider caches prompt prefixes.
type CacheMode string

const (
	// CacheNone: no prompt caching.
	CacheNone CacheMode = "none"
	// CacheImplicit: the provider caches identical prefixes automatically.
	CacheImplicit CacheMode = "implicit"
	// CacheExplicit: the caller marks cacheable spans (breakpoints, TTLs).
	CacheExplicit CacheMode = "explicit"
	// CacheHybrid: implicit by default, with explicit controls available.
	CacheHybrid CacheMode = "hybrid"
	// CacheUnknown: not determined.
	CacheUnknown CacheMode = "unknown"
)

// CapabilityState is DWYT's level of control over a capability (spec §39).
type CapabilityState string

const (
	// StateEnforced: DWYT controls the transport and applied real configuration.
	StateEnforced CapabilityState = "enforced"
	// StateAdvised: DWYT shaped the prompt but does not control the request.
	StateAdvised CapabilityState = "advised"
	// StateObserved: the provider or client returned metrics.
	StateObserved CapabilityState = "observed"
	// StateUnsupported: the capability is known to be unavailable.
	StateUnsupported CapabilityState = "unsupported"
	// StateUnknown: insufficient data.
	StateUnknown CapabilityState = "unknown"
)

// Capabilities is what a provider/model supports.
//
// Every boolean is "known to be supported". A false value therefore means
// "not supported *or* not known", which is why CacheState carries the
// distinction separately — a caller must be able to tell those apart.
type Capabilities struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`

	CacheMode           CacheMode       `json:"cache_mode"`
	CacheState          CapabilityState `json:"cache_state"`
	ExplicitBreakpoints bool            `json:"explicit_breakpoints"`
	CacheTTL            []time.Duration `json:"cache_ttl,omitempty"`
	CacheKey            bool            `json:"cache_key"`

	UsageCachedTokens bool `json:"usage_cached_tokens"`
	UsageCacheWrite   bool `json:"usage_cache_write"`
	UsageReasoning    bool `json:"usage_reasoning_tokens"`

	MaxOutputTokens  bool `json:"max_output_tokens"`
	TextVerbosity    bool `json:"text_verbosity"`
	ReasoningEffort  bool `json:"reasoning_effort"`
	StructuredOutput bool `json:"structured_output"`

	// ContextWindow is the model's total window, when known. Zero means unknown,
	// and the budgeter treats it as "no constraint from the model".
	ContextWindow int `json:"context_window,omitempty"`

	// LongContext describes the pricing cliff above a threshold (spec §44).
	LongContext LongContext `json:"long_context,omitempty"`

	// Source records where this record came from, so a surprising capability can
	// be traced to the seed catalog, discovery, or the fallback.
	Source string `json:"source"`
}

// LongContext is the pricing multiplier above a token threshold (spec §44).
type LongContext struct {
	// Threshold is the input-token count above which the multipliers apply.
	Threshold int `json:"threshold,omitempty"`
	// Multipliers are relative to the base price. 1.0 means no change.
	InputMultiplier  float64 `json:"input_multiplier,omitempty"`
	CacheMultiplier  float64 `json:"cache_multiplier,omitempty"`
	OutputMultiplier float64 `json:"output_multiplier,omitempty"`
}

// Active reports whether a long-context threshold is configured.
func (l LongContext) Active() bool { return l.Threshold > 0 }

// GenericFallback is the capability record for an unknown provider or model
// (spec §40).
//
// It claims nothing: caching is unknown rather than absent, and no control
// capability is asserted. Structural guidance (prefix ordering, budgeting,
// dedup, deltas) still applies, because it is provider-independent.
func GenericFallback(providerName, model string) Capabilities {
	return Capabilities{
		Provider:   normalize(providerName),
		Model:      strings.TrimSpace(model),
		CacheMode:  CacheUnknown,
		CacheState: StateUnknown,
		Source:     "fallback",
	}
}

// Catalog resolves capabilities for a provider/model.
//
// Lookup order:
//
//  1. a record learned at runtime (Observe), keyed by provider+model;
//  2. a seed record for the exact provider+model;
//  3. a seed record for the provider alone;
//  4. the generic fallback.
//
// Runtime records win because they reflect what the provider actually did on
// this machine, which outranks a compiled-in guess that may be a release behind.
type Catalog struct {
	seedProviders map[string]Capabilities
	seedModels    map[string]Capabilities
	learned       map[string]Capabilities
	// version identifies the seed data, so telemetry can record which catalog
	// produced a decision.
	version string
}

// SeedVersion identifies the compiled-in capability seed. Bump it when the seed
// data changes so a stored decision can be traced to the data behind it.
const SeedVersion = "2026-09-seed-1"

// NewCatalog builds a catalog from the compiled-in seed.
func NewCatalog() *Catalog {
	c := &Catalog{
		seedProviders: map[string]Capabilities{},
		seedModels:    map[string]Capabilities{},
		learned:       map[string]Capabilities{},
		version:       SeedVersion,
	}
	for name, caps := range seedProviders() {
		caps.Provider = name
		caps.Source = "seed:provider"
		c.seedProviders[name] = caps
	}
	return c
}

// Version returns the seed version.
func (c *Catalog) Version() string { return c.version }

// Lookup resolves capabilities, never failing.
func (c *Catalog) Lookup(providerName, model string) Capabilities {
	p := normalize(providerName)
	m := strings.TrimSpace(model)

	if caps, ok := c.learned[key(p, m)]; ok {
		return caps
	}
	if caps, ok := c.learned[key(p, "")]; ok {
		caps.Model = m
		return caps
	}
	if caps, ok := c.seedModels[key(p, m)]; ok {
		caps.Model = m
		return caps
	}
	if caps, ok := c.seedProviders[p]; ok {
		caps.Model = m
		return caps
	}
	// Providers are frequently addressed through a gateway with a prefixed
	// model name ("openai/gpt-...", "anthropic:claude-..."). Recover the
	// provider from the model string before giving up.
	if inferred := inferProvider(m); inferred != "" && inferred != p {
		if caps, ok := c.seedProviders[inferred]; ok {
			caps.Model = m
			caps.Source = "seed:inferred"
			return caps
		}
	}
	return GenericFallback(p, m)
}

// Observe records what a provider actually did, upgrading the stored record.
//
// It only ever *adds* knowledge: an observed capability is never downgraded to
// unsupported by a later request that happened not to exercise it. A single
// request without cached tokens is not evidence that caching is unavailable.
func (c *Catalog) Observe(providerName, model string, observed Observation) Capabilities {
	p := normalize(providerName)
	m := strings.TrimSpace(model)
	caps := c.Lookup(p, m)

	if observed.CachedTokensReported {
		caps.UsageCachedTokens = true
		caps.CacheState = StateObserved
		if caps.CacheMode == CacheUnknown || caps.CacheMode == CacheNone {
			caps.CacheMode = CacheImplicit
		}
	}
	if observed.CacheWriteReported {
		caps.UsageCacheWrite = true
		caps.CacheState = StateObserved
	}
	if observed.ReasoningTokensReported {
		caps.UsageReasoning = true
	}
	if observed.ContextWindow > caps.ContextWindow {
		caps.ContextWindow = observed.ContextWindow
	}
	caps.Provider = p
	caps.Model = m
	caps.Source = "observed"

	c.learned[key(p, m)] = caps
	return caps
}

// Observation is evidence gathered from a real provider response.
type Observation struct {
	CachedTokensReported    bool
	CacheWriteReported      bool
	ReasoningTokensReported bool
	ContextWindow           int
}

// Register installs or overrides a capability record, for a config file or a
// discovery probe.
func (c *Catalog) Register(caps Capabilities) {
	caps.Provider = normalize(caps.Provider)
	if caps.Source == "" {
		caps.Source = "registered"
	}
	if caps.Model == "" {
		c.seedProviders[caps.Provider] = caps
		return
	}
	c.seedModels[key(caps.Provider, caps.Model)] = caps
}

// Providers lists the known provider names.
func (c *Catalog) Providers() []string {
	out := make([]string, 0, len(c.seedProviders))
	for name := range c.seedProviders {
		out = append(out, name)
	}
	return sortedStrings(out)
}

// seedProviders is the compiled-in starting point (spec §40).
//
// It deliberately records *strategy* and *shape* rather than prices or exact
// model names: those change constantly, and a stale hardcoded price would be
// worse than no price at all. Cache state is `advised` because DWYT shapes the
// prompt but does not own the transport for third-party clients.
func seedProviders() map[string]Capabilities {
	hour := time.Hour
	fiveMin := 5 * time.Minute
	return map[string]Capabilities{
		"openai": {
			CacheMode: CacheImplicit, CacheState: StateAdvised,
			CacheKey: true, UsageCachedTokens: true, UsageReasoning: true,
			MaxOutputTokens: true, StructuredOutput: true, ReasoningEffort: true,
			TextVerbosity: true,
		},
		"anthropic": {
			CacheMode: CacheExplicit, CacheState: StateAdvised,
			ExplicitBreakpoints: true, CacheTTL: []time.Duration{fiveMin, hour},
			UsageCachedTokens: true, UsageCacheWrite: true,
			MaxOutputTokens: true, StructuredOutput: true, ReasoningEffort: true,
		},
		"gemini": {
			CacheMode: CacheHybrid, CacheState: StateAdvised,
			ExplicitBreakpoints: true, UsageCachedTokens: true,
			MaxOutputTokens: true, StructuredOutput: true,
		},
		"deepseek": {
			CacheMode: CacheImplicit, CacheState: StateAdvised,
			UsageCachedTokens: true, MaxOutputTokens: true,
		},
		"zai": {
			CacheMode: CacheUnknown, CacheState: StateUnknown,
			MaxOutputTokens: true,
		},
		"xai": {
			CacheMode: CacheImplicit, CacheState: StateAdvised,
			UsageCachedTokens: true, MaxOutputTokens: true, StructuredOutput: true,
		},
		"moonshot": {
			CacheMode: CacheExplicit, CacheState: StateAdvised,
			ExplicitBreakpoints: true, MaxOutputTokens: true,
		},
		"minimax": {
			CacheMode: CacheUnknown, CacheState: StateUnknown,
			MaxOutputTokens: true,
		},
		"mistral": {
			CacheMode: CacheUnknown, CacheState: StateUnknown,
			MaxOutputTokens: true, StructuredOutput: true,
		},
		"qwen": {
			CacheMode: CacheImplicit, CacheState: StateAdvised,
			UsageCachedTokens: true, MaxOutputTokens: true,
		},
	}
}

// inferProvider recovers a provider from a namespaced model identifier.
func inferProvider(model string) string {
	lower := strings.ToLower(model)
	if idx := strings.IndexAny(lower, "/:"); idx > 0 {
		candidate := normalize(lower[:idx])
		if _, ok := seedProviders()[candidate]; ok {
			return candidate
		}
	}
	// Common bare model prefixes.
	switch {
	case strings.HasPrefix(lower, "gpt-"), strings.HasPrefix(lower, "o1"), strings.HasPrefix(lower, "o3"):
		return "openai"
	case strings.HasPrefix(lower, "claude"):
		return "anthropic"
	case strings.HasPrefix(lower, "gemini"):
		return "gemini"
	case strings.HasPrefix(lower, "deepseek"):
		return "deepseek"
	case strings.HasPrefix(lower, "grok"):
		return "xai"
	case strings.HasPrefix(lower, "glm"):
		return "zai"
	case strings.HasPrefix(lower, "kimi"):
		return "moonshot"
	case strings.HasPrefix(lower, "qwen"):
		return "qwen"
	case strings.HasPrefix(lower, "mistral"), strings.HasPrefix(lower, "codestral"):
		return "mistral"
	}
	return ""
}

// normalize canonicalizes a provider name: lowercase, punctuation folded, and
// the common aliases mapped onto one key.
func normalize(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, " ", "")
	n = strings.ReplaceAll(n, "-", "")
	n = strings.ReplaceAll(n, "_", "")
	n = strings.ReplaceAll(n, ".", "")
	switch n {
	case "googlegemini", "google", "vertex", "vertexai", "googleai":
		return "gemini"
	case "zai", "glm", "zhipu", "zhipuai":
		return "zai"
	case "grok", "x", "xai":
		return "xai"
	case "kimi", "moonshotai":
		return "moonshot"
	case "alibaba", "dashscope", "tongyi":
		return "qwen"
	case "claude", "anthropicai":
		return "anthropic"
	case "openaiapi", "azureopenai", "azure":
		return "openai"
	}
	return n
}

func key(providerName, model string) string {
	return providerName + "\x00" + strings.ToLower(model)
}

func sortedStrings(in []string) []string {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
	return in
}
