package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLookupResolvesKnownProviders(t *testing.T) {
	c := NewCatalog()
	caps := c.Lookup("Anthropic", "claude-x")
	if caps.Provider != "anthropic" {
		t.Fatalf("provider not normalized: %s", caps.Provider)
	}
	if caps.CacheMode != CacheExplicit || !caps.ExplicitBreakpoints {
		t.Fatalf("expected explicit cache support: %+v", caps)
	}
	if len(caps.CacheTTL) == 0 {
		t.Fatal("expected TTL options for an explicit-cache provider")
	}
}

func TestLookupNormalizesAliases(t *testing.T) {
	c := NewCatalog()
	for _, alias := range []string{"google", "vertex-ai", "Google Gemini", "googleai"} {
		if got := c.Lookup(alias, "").Provider; got != "gemini" {
			t.Fatalf("alias %q resolved to %q, want gemini", alias, got)
		}
	}
	if got := c.Lookup("Azure OpenAI", "").Provider; got != "openai" {
		t.Fatalf("azure should map to openai, got %q", got)
	}
}

func TestLookupInfersProviderFromNamespacedModel(t *testing.T) {
	c := NewCatalog()
	caps := c.Lookup("openrouter", "anthropic/claude-x")
	if caps.Provider != "anthropic" {
		t.Fatalf("expected the provider to be inferred from the model, got %q", caps.Provider)
	}
	if caps.Source != "seed:inferred" {
		t.Fatalf("the inference should be visible in Source, got %q", caps.Source)
	}
}

func TestLookupInfersFromBareModelPrefix(t *testing.T) {
	c := NewCatalog()
	for model, want := range map[string]string{
		"gpt-5-mini":      "openai",
		"claude-sonnet-9": "anthropic",
		"gemini-3-pro":    "gemini",
		"grok-5":          "xai",
		"kimi-k3":         "moonshot",
	} {
		if got := c.Lookup("", model).Provider; got != want {
			t.Fatalf("model %q inferred %q, want %q", model, got, want)
		}
	}
}

// An unknown provider must get a fallback that claims nothing.
func TestUnknownProviderGetsHonestFallback(t *testing.T) {
	caps := NewCatalog().Lookup("some-new-vendor", "their-model")
	if caps.Source != "fallback" {
		t.Fatalf("expected the fallback, got %q", caps.Source)
	}
	if caps.CacheMode != CacheUnknown || caps.CacheState != StateUnknown {
		t.Fatalf("an unknown provider must not be reported as uncached: %+v", caps)
	}
	if caps.ExplicitBreakpoints || caps.CacheKey || caps.UsageCachedTokens {
		t.Fatalf("no capability may be asserted for an unknown provider: %+v", caps)
	}
}

func TestObserveUpgradesCapabilityToObserved(t *testing.T) {
	c := NewCatalog()
	before := c.Lookup("some-vendor", "m1")
	if before.CacheState == StateObserved {
		t.Fatal("nothing has been observed yet")
	}

	after := c.Observe("some-vendor", "m1", Observation{CachedTokensReported: true, ContextWindow: 200000})
	if after.CacheState != StateObserved {
		t.Fatalf("expected observed state, got %s", after.CacheState)
	}
	if !after.UsageCachedTokens {
		t.Fatal("cached-token reporting should be recorded")
	}
	if after.CacheMode != CacheImplicit {
		t.Fatalf("a provider that reports cached tokens caches something: %s", after.CacheMode)
	}
	if after.ContextWindow != 200000 {
		t.Fatalf("context window not recorded: %d", after.ContextWindow)
	}
	// The learned record must win on subsequent lookups.
	if c.Lookup("some-vendor", "m1").CacheState != StateObserved {
		t.Fatal("the learned record should take precedence over the seed")
	}
}

// A request that happened not to use the cache is not evidence that caching is
// unavailable.
func TestObserveNeverDowngradesKnowledge(t *testing.T) {
	c := NewCatalog()
	c.Observe("some-vendor", "m1", Observation{CachedTokensReported: true})
	after := c.Observe("some-vendor", "m1", Observation{})

	if !after.UsageCachedTokens || after.CacheState != StateObserved {
		t.Fatalf("a later quiet request must not erase what was observed: %+v", after)
	}
	if after.ContextWindow != 0 {
		t.Fatalf("no window was ever reported; none should be invented: %d", after.ContextWindow)
	}
}

func TestRegisterOverridesTheSeed(t *testing.T) {
	c := NewCatalog()
	c.Register(Capabilities{
		Provider: "openai", Model: "special-model",
		CacheMode: CacheNone, CacheState: StateUnsupported,
	})
	caps := c.Lookup("openai", "special-model")
	if caps.CacheMode != CacheNone || caps.CacheState != StateUnsupported {
		t.Fatalf("the registered override should win: %+v", caps)
	}
	// The provider-level seed must be unaffected.
	if c.Lookup("openai", "other-model").CacheMode != CacheImplicit {
		t.Fatal("a model override must not change the provider default")
	}
}

func TestLongContextActive(t *testing.T) {
	if (LongContext{}).Active() {
		t.Fatal("a zero threshold is not an active long-context cliff")
	}
	if !(LongContext{Threshold: 200000}).Active() {
		t.Fatal("a positive threshold is active")
	}
}

func TestEmptyPricingCatalogReportsUnknownNotZero(t *testing.T) {
	c := NewPricingCatalog()
	in, out := 1000, 500
	est := c.EstimateCost("openai", "gpt-x", Usage{InputTokens: &in, OutputTokens: &out})
	if est.Known {
		t.Fatal("with no pricing data the cost must be reported as unknown")
	}
	if est.USD != 0 {
		t.Fatalf("an unknown cost must not carry a number: %f", est.USD)
	}
	if len(est.Missing) == 0 || est.Missing[0] != "pricing" {
		t.Fatalf("the reason must be stated: %v", est.Missing)
	}
}

func TestEstimateCostUsesRegisteredPricing(t *testing.T) {
	c := NewPricingCatalog()
	c.Register(Pricing{
		Provider: "openai", Model: "gpt-x",
		InputPerMTok: 2, CachedInputPerMTok: 0.5, OutputPerMTok: 8,
	})

	input, cached, output := 1_000_000, 400_000, 100_000
	est := c.EstimateCost("openai", "gpt-x", Usage{
		InputTokens: &input, CachedInputTokens: &cached, OutputTokens: &output,
	})
	if !est.Known {
		t.Fatalf("expected a known estimate: %+v", est)
	}
	// 600k uncached @ $2 + 400k cached @ $0.50 + 100k output @ $8 = 1.2 + 0.2 + 0.8
	want := 0.6*2 + 0.4*0.5 + 0.1*8
	if diff := est.USD - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("cost %f, want %f", est.USD, want)
	}
}

func TestEstimateCostDerivesUncachedFromTotals(t *testing.T) {
	c := NewPricingCatalog()
	c.Register(Pricing{Provider: "p", InputPerMTok: 1, CachedInputPerMTok: 0, OutputPerMTok: 1})

	input, cached := 1_000_000, 1_000_000
	est := c.EstimateCost("p", "m", Usage{InputTokens: &input, CachedInputTokens: &cached})
	// All input was cached, and no cached price is on record: the estimate must
	// say so rather than charging the full rate or nothing.
	if est.USD != 0 {
		t.Fatalf("nothing should be charged for uncached tokens that do not exist: %f", est.USD)
	}
	if !containsString(est.Missing, "cached_input_price") {
		t.Fatalf("the missing cached price must be reported: %v", est.Missing)
	}
}

func TestEstimateCostAppliesLongContextMultiplier(t *testing.T) {
	c := NewPricingCatalog()
	c.Register(Pricing{
		Provider: "p", InputPerMTok: 1, OutputPerMTok: 1,
		LongContext: LongContext{Threshold: 100, InputMultiplier: 2, OutputMultiplier: 2},
	})

	small, output := 50, 10
	base := c.EstimateCost("p", "m", Usage{InputTokens: &small, OutputTokens: &output})
	if base.LongContextApplied {
		t.Fatal("below the threshold the multiplier must not apply")
	}

	large := 1000
	big := c.EstimateCost("p", "m", Usage{InputTokens: &large, OutputTokens: &output})
	if !big.LongContextApplied {
		t.Fatal("above the threshold the multiplier must apply")
	}
	if big.USD <= base.USD {
		t.Fatalf("the multiplier should raise the cost: %f vs %f", big.USD, base.USD)
	}
}

func TestEstimateCostDoesNotGuessReasoningBilling(t *testing.T) {
	c := NewPricingCatalog()
	c.Register(Pricing{Provider: "p", InputPerMTok: 1, OutputPerMTok: 10})

	input, output, reasoning := 1_000_000, 0, 1_000_000
	est := c.EstimateCost("p", "m", Usage{
		InputTokens: &input, OutputTokens: &output, ReasoningTokens: &reasoning,
	})
	if !containsString(est.Missing, "reasoning_billing_unknown") {
		t.Fatalf("without the flag, reasoning billing must be reported unknown: %v", est.Missing)
	}
	if est.USD != 1.0 {
		t.Fatalf("reasoning tokens must not be silently charged: %f", est.USD)
	}

	c.Register(Pricing{Provider: "p", InputPerMTok: 1, OutputPerMTok: 10, ReasoningBilledAsOutput: true})
	est = c.EstimateCost("p", "m", Usage{
		InputTokens: &input, OutputTokens: &output, ReasoningTokens: &reasoning,
	})
	if est.USD != 11.0 {
		t.Fatalf("with the flag set, reasoning is billed as output: %f", est.USD)
	}
}

func TestPricingLookupMatchesModelPrefix(t *testing.T) {
	c := NewPricingCatalog()
	c.Register(Pricing{Provider: "openai", Model: "gpt-x", InputPerMTok: 1, OutputPerMTok: 1})

	if _, ok := c.Lookup("openai", "gpt-x-2026-01-01"); !ok {
		t.Fatal("a dated model id should resolve against its family entry")
	}
	if _, ok := c.Lookup("openai", "something-else"); !ok {
		t.Skip("no provider-level entry registered; nothing to assert")
	}
}

func TestLoadPricingMissingFileIsNotAnError(t *testing.T) {
	c, err := LoadPricing(t.TempDir())
	if err != nil {
		t.Fatalf("a missing catalog must not be an error: %v", err)
	}
	if c == nil {
		t.Fatal("expected an empty catalog")
	}
	if meta := c.Meta(); meta["version"] != "empty" {
		t.Fatalf("expected the empty catalog version, got %v", meta["version"])
	}
}

// A malformed catalog must be reported: silently ignoring it would leave the
// user believing their prices were applied.
func TestLoadPricingMalformedFileIsAnError(t *testing.T) {
	home := t.TempDir()
	path := PricingPath(home)
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte("{not json"), 0644)

	if _, err := LoadPricing(home); err == nil {
		t.Fatal("a malformed pricing catalog must be reported")
	}
}

func TestLoadPricingReadsEntriesAndVersion(t *testing.T) {
	home := t.TempDir()
	path := PricingPath(home)
	os.MkdirAll(filepath.Dir(path), 0755)
	doc := pricingFile{
		Version: "2026-09-01",
		Entries: []Pricing{
			{Provider: "OpenAI", Model: "gpt-x", InputPerMTok: 3, OutputPerMTok: 12},
			{Provider: "anthropic", InputPerMTok: 4, OutputPerMTok: 20},
		},
	}
	data, _ := json.Marshal(doc)
	os.WriteFile(path, data, 0644)

	c, err := LoadPricing(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Meta()["version"]; got != "2026-09-01" {
		t.Fatalf("version not loaded: %v", got)
	}
	if entry, ok := c.Lookup("openai", "gpt-x"); !ok || entry.InputPerMTok != 3 {
		t.Fatalf("model entry not loaded: %+v", entry)
	}
	// The provider-level entry must be found for an unlisted model.
	if entry, ok := c.Lookup("anthropic", "claude-anything"); !ok || entry.OutputPerMTok != 20 {
		t.Fatalf("provider entry not loaded: %+v", entry)
	}
}

func TestCatalogVersionIsReported(t *testing.T) {
	if NewCatalog().Version() != SeedVersion {
		t.Fatal("the seed version must be reported so a decision can be traced")
	}
}

func TestProvidersListIsSorted(t *testing.T) {
	names := NewCatalog().Providers()
	if len(names) == 0 {
		t.Fatal("expected seeded providers")
	}
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Fatalf("provider list is not sorted: %v", names)
		}
	}
}

func containsString(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
