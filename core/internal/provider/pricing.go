package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Pricing catalog (spec §45).
//
// Prices change constantly, so this file contains *no* prices. It contains the
// shape of a price record, a versioned loader, and an honest "unknown" answer.
// The catalog is populated from a JSON file under ~/.dwyt so a user can update
// it without waiting for a DWYT release, and every cost figure DWYT reports
// carries whether it was computed from real data or is unavailable.

// Pricing is the cost model for one provider/model, in USD per million tokens.
type Pricing struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`

	InputPerMTok       float64 `json:"input_per_mtok,omitempty"`
	CachedInputPerMTok float64 `json:"cached_input_per_mtok,omitempty"`
	CacheWritePerMTok  float64 `json:"cache_write_per_mtok,omitempty"`
	OutputPerMTok      float64 `json:"output_per_mtok,omitempty"`
	// CacheStoragePerHour is charged by providers that persist an explicit cache.
	CacheStoragePerHour float64 `json:"cache_storage_per_hour,omitempty"`
	// ReasoningBilledAsOutput is true when thinking tokens are charged at the
	// output rate. When false, reasoning tokens are excluded from the estimate
	// rather than guessed at.
	ReasoningBilledAsOutput bool `json:"reasoning_billed_as_output,omitempty"`

	// Modes are multipliers for batch/flex/fast execution.
	BatchMultiplier float64 `json:"batch_multiplier,omitempty"`
	FlexMultiplier  float64 `json:"flex_multiplier,omitempty"`
	FastMultiplier  float64 `json:"fast_multiplier,omitempty"`

	// LongContext mirrors the capability threshold so a cost estimate can apply
	// the multiplier without a second lookup.
	LongContext LongContext `json:"long_context,omitempty"`
}

// Priced reports whether enough data exists to compute a cost.
func (p Pricing) Priced() bool {
	return p.InputPerMTok > 0 || p.OutputPerMTok > 0
}

// PricingCatalog holds versioned pricing data.
type PricingCatalog struct {
	mu         sync.RWMutex
	byModel    map[string]Pricing
	byProvider map[string]Pricing
	version    string
	loadedAt   time.Time
	source     string
}

// NewPricingCatalog returns an empty catalog. Empty is the correct default:
// DWYT must report "cost unknown" rather than a stale invented number.
func NewPricingCatalog() *PricingCatalog {
	return &PricingCatalog{
		byModel:    map[string]Pricing{},
		byProvider: map[string]Pricing{},
		version:    "empty",
		source:     "none",
	}
}

// pricingFile is the on-disk document format.
type pricingFile struct {
	Version string    `json:"version"`
	Updated string    `json:"updated,omitempty"`
	Entries []Pricing `json:"entries"`
}

// PricingPath is the conventional location of the user-updatable catalog.
func PricingPath(dwytHome string) string {
	return filepath.Join(dwytHome, "config", "pricing.json")
}

// LoadPricing reads the catalog from dwytHome.
//
// A missing file is not an error: it yields an empty catalog whose cost answers
// are "unknown". A malformed file *is* an error, because silently ignoring it
// would leave the user believing their prices were applied.
func LoadPricing(dwytHome string) (*PricingCatalog, error) {
	c := NewPricingCatalog()
	path := PricingPath(dwytHome)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, fmt.Errorf("pricing: read %s: %w", path, err)
	}
	var doc pricingFile
	if err := json.Unmarshal(data, &doc); err != nil {
		return c, fmt.Errorf("pricing: %s is not valid JSON: %w", path, err)
	}
	c.replace(doc, path)
	return c, nil
}

func (c *PricingCatalog) replace(doc pricingFile, source string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byModel = map[string]Pricing{}
	c.byProvider = map[string]Pricing{}
	for _, entry := range doc.Entries {
		entry.Provider = normalize(entry.Provider)
		if entry.Model == "" {
			c.byProvider[entry.Provider] = entry
			continue
		}
		c.byModel[key(entry.Provider, entry.Model)] = entry
	}
	c.version = doc.Version
	if c.version == "" {
		c.version = "unversioned"
	}
	c.source = source
	c.loadedAt = time.Now()
}

// Register adds or overrides one pricing entry at runtime.
func (c *PricingCatalog) Register(p Pricing) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p.Provider = normalize(p.Provider)
	if p.Model == "" {
		c.byProvider[p.Provider] = p
		return
	}
	c.byModel[key(p.Provider, p.Model)] = p
}

// Lookup resolves pricing for a provider/model, most specific first.
func (c *PricingCatalog) Lookup(providerName, model string) (Pricing, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p := normalize(providerName)
	if entry, ok := c.byModel[key(p, model)]; ok {
		return entry, true
	}
	// Try a prefix match so "gpt-x-2026-01-01" resolves against a "gpt-x" entry.
	lower := strings.ToLower(strings.TrimSpace(model))
	for k, entry := range c.byModel {
		provider, entryModel, found := strings.Cut(k, "\x00")
		if !found || provider != p || entryModel == "" {
			continue
		}
		if strings.HasPrefix(lower, entryModel) {
			return entry, true
		}
	}
	if entry, ok := c.byProvider[p]; ok {
		return entry, true
	}
	return Pricing{}, false
}

// Meta describes the loaded catalog, for the dashboard and telemetry.
func (c *PricingCatalog) Meta() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	meta := map[string]interface{}{
		"version":          c.version,
		"source":           c.source,
		"model_entries":    len(c.byModel),
		"provider_entries": len(c.byProvider),
	}
	if !c.loadedAt.IsZero() {
		meta["loaded_at"] = c.loadedAt.Format(time.RFC3339)
	}
	return meta
}

// Usage is the token breakdown of one request. Nil fields mean "not reported",
// which is distinct from zero and must stay distinct all the way into the cost
// estimate (spec §52).
type Usage struct {
	InputTokens         *int
	UncachedInputTokens *int
	CachedInputTokens   *int
	CacheWriteTokens    *int
	OutputTokens        *int
	ReasoningTokens     *int
}

// CostEstimate is the outcome of a cost computation.
type CostEstimate struct {
	USD float64 `json:"usd"`
	// Known is false when there was not enough pricing or usage data. A caller
	// must not display an unknown estimate as $0.00.
	Known bool `json:"known"`
	// PricingVersion records which catalog produced the figure.
	PricingVersion string `json:"pricing_version,omitempty"`
	// Missing lists what prevented a complete estimate.
	Missing []string `json:"missing,omitempty"`
	// LongContextApplied reports whether the threshold multiplier was used.
	LongContextApplied bool `json:"long_context_applied,omitempty"`
}

// EstimateCost computes the USD cost of a request.
//
// Uncached input is derived when the provider reported only totals: uncached =
// input - cached. Fields the provider did not report are omitted from the sum and
// listed in Missing, so an incomplete estimate is visibly incomplete.
func (c *PricingCatalog) EstimateCost(providerName, model string, u Usage) CostEstimate {
	est := CostEstimate{PricingVersion: c.versionSnapshot()}
	pricing, ok := c.Lookup(providerName, model)
	if !ok || !pricing.Priced() {
		est.Missing = append(est.Missing, "pricing")
		return est
	}

	perMTok := func(tokens int, rate float64) float64 {
		return float64(tokens) / 1_000_000 * rate
	}

	cached := valueOr(u.CachedInputTokens, 0)
	uncached := 0
	switch {
	case u.UncachedInputTokens != nil:
		uncached = *u.UncachedInputTokens
	case u.InputTokens != nil:
		uncached = *u.InputTokens - cached
		if uncached < 0 {
			uncached = 0
		}
	default:
		est.Missing = append(est.Missing, "input_tokens")
	}

	inputRate := pricing.InputPerMTok
	cachedRate := pricing.CachedInputPerMTok
	outputRate := pricing.OutputPerMTok

	// Long-context multipliers apply to the whole request once the threshold is
	// crossed (spec §44).
	totalInput := uncached + cached
	if pricing.LongContext.Active() && totalInput > pricing.LongContext.Threshold {
		est.LongContextApplied = true
		if m := pricing.LongContext.InputMultiplier; m > 0 {
			inputRate *= m
		}
		if m := pricing.LongContext.CacheMultiplier; m > 0 {
			cachedRate *= m
		}
		if m := pricing.LongContext.OutputMultiplier; m > 0 {
			outputRate *= m
		}
	}

	total := perMTok(uncached, inputRate)
	if cached > 0 {
		if cachedRate > 0 {
			total += perMTok(cached, cachedRate)
		} else {
			// No cached-input price on record: charging it at the full input
			// rate would overstate the cost, and charging zero would understate
			// it. Say so instead.
			est.Missing = append(est.Missing, "cached_input_price")
		}
	}
	if u.CacheWriteTokens != nil && *u.CacheWriteTokens > 0 {
		if pricing.CacheWritePerMTok > 0 {
			total += perMTok(*u.CacheWriteTokens, pricing.CacheWritePerMTok)
		} else {
			est.Missing = append(est.Missing, "cache_write_price")
		}
	}
	if u.OutputTokens != nil {
		total += perMTok(*u.OutputTokens, outputRate)
	} else {
		est.Missing = append(est.Missing, "output_tokens")
	}
	if u.ReasoningTokens != nil && *u.ReasoningTokens > 0 {
		if pricing.ReasoningBilledAsOutput {
			total += perMTok(*u.ReasoningTokens, outputRate)
		} else {
			// Whether reasoning tokens are billed is provider-specific; without
			// that flag DWYT does not guess.
			est.Missing = append(est.Missing, "reasoning_billing_unknown")
		}
	}

	est.USD = total
	est.Known = total > 0 || (u.InputTokens != nil || u.OutputTokens != nil)
	return est
}

func (c *PricingCatalog) versionSnapshot() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

func valueOr(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}
