// Package dwytconfig loads the consolidated DWYT v5 configuration (spec §57).
//
// Everything the spec makes configurable lives in one file at
// ~/.dwyt/config/dwyt.json, and every value has a working default. Three
// properties matter:
//
//  1. **A missing file is normal.** DWYT must work out of the box, so absence
//     yields the recommended defaults rather than an error.
//
//  2. **A malformed file is an error.** Silently falling back to defaults would
//     leave a user convinced their tuning was applied when it was not.
//
//  3. **Partial files are merged, not replaced.** JSON decoding into a
//     pre-populated struct leaves absent fields untouched, so a file that sets
//     one value does not reset the other forty.
package dwytconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/contextgov"
	"github.com/fvmoraes/dwyt/internal/governor"
	"github.com/fvmoraes/dwyt/internal/housekeeper"
	"github.com/fvmoraes/dwyt/internal/mcpproxy"
)

// Version is the configuration schema version.
const Version = "5.0.0"

// Duration wraps time.Duration with human-readable JSON ("6h", "180d", "72h").
//
// The spec writes TTLs as "180d" and "72h"; Go's time.ParseDuration does not
// understand days, so this type adds that one extension rather than forcing the
// user to write 4320h.
type Duration time.Duration

// UnmarshalJSON accepts a duration string or a number of seconds.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		parsed, err := ParseDuration(asString)
		if err != nil {
			return err
		}
		*d = Duration(parsed)
		return nil
	}
	var seconds float64
	if err := json.Unmarshal(data, &seconds); err != nil {
		return fmt.Errorf("duration must be a string like \"6h\" or a number of seconds")
	}
	*d = Duration(time.Duration(seconds) * time.Second)
	return nil
}

// MarshalJSON writes the duration back in its readable form.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(FormatDuration(time.Duration(d)))
}

// Std returns the standard library duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// ParseDuration parses a duration, additionally supporting a "d" (days) suffix.
func ParseDuration(s string) (time.Duration, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, nil
	}
	// "session" is the spec's marker for "lives only as long as the session",
	// which is a zero TTL as far as the housekeeper is concerned.
	if strings.EqualFold(trimmed, "session") {
		return 0, nil
	}
	if strings.HasSuffix(trimmed, "d") {
		var days float64
		if _, err := fmt.Sscanf(strings.TrimSuffix(trimmed, "d"), "%f", &days); err == nil {
			return time.Duration(days * 24 * float64(time.Hour)), nil
		}
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: use forms like \"72h\", \"6h\", \"180d\"", s)
	}
	return parsed, nil
}

// FormatDuration renders a duration using days when it divides evenly, which
// keeps a round-tripped config as readable as the one the user wrote.
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int64(d/(24*time.Hour)))
	}
	return d.String()
}

// ContextConfig mirrors the `context` block of spec §57.
type ContextConfig struct {
	Mode                 string `json:"mode"`
	DefaultBudget        int    `json:"default_budget"`
	MaxBudget            int    `json:"max_budget"`
	ReservePercent       int    `json:"reserve_percent"`
	ProgressiveExpansion bool   `json:"progressive_expansion"`
	ConfidenceGated      bool   `json:"confidence_gated"`
	PreserveCachePrefix  bool   `json:"preserve_cache_prefix"`
	ReuseBeforeRetrieve  bool   `json:"reuse_before_retrieve"`
}

// RetrievalConfig mirrors the `retrieval` block.
type RetrievalConfig struct {
	Strategy         string `json:"strategy"`
	DefaultTopK      int    `json:"default_top_k"`
	PreferProjectMap bool   `json:"prefer_project_map"`
	PreferSymbols    bool   `json:"prefer_symbols"`
	PreferRanges     bool   `json:"prefer_ranges"`
	PreferDeltas     bool   `json:"prefer_deltas"`
	FullFile         string `json:"full_file"`
	SemanticDedup    bool   `json:"semantic_dedup"`
}

// MemoryConfig mirrors the `memory` block.
type MemoryConfig struct {
	Brain                       string `json:"brain"`
	CanonicalMemory             bool   `json:"canonical_memory"`
	HierarchicalSummaries       bool   `json:"hierarchical_summaries"`
	CompactSnapshots            bool   `json:"compact_snapshots"`
	SnapshotStateHash           bool   `json:"snapshot_state_hash"`
	HotWarmCold                 bool   `json:"hot_warm_cold"`
	RawExternalStore            bool   `json:"raw_external_store"`
	ExcludeRawFromDefaultSearch bool   `json:"exclude_raw_from_default_search"`
	LifecycleGC                 bool   `json:"lifecycle_gc"`
	TTLEnabled                  bool   `json:"ttl_enabled"`
}

// HousekeeperConfig mirrors the `housekeeper` block.
type HousekeeperConfig struct {
	Enabled  bool `json:"enabled"`
	Sessions struct {
		KeepLatest int    `json:"keep_latest"`
		OrderBy    string `json:"order_by"`
	} `json:"sessions"`
	Run struct {
		OnSessionClose bool     `json:"on_session_close"`
		OnStartup      bool     `json:"on_startup"`
		Interval       Duration `json:"interval"`
	} `json:"run"`
	BeforeDelete struct {
		ExtractReusableKnowledge bool `json:"extract_reusable_knowledge"`
		PromoteToCanonicalMemory bool `json:"promote_to_canonical_memory"`
	} `json:"before_delete"`
	StaleDetection struct {
		SourceHash bool `json:"source_hash"`
	} `json:"stale_detection"`
	UsageMetadata struct {
		LastAccessed bool `json:"last_accessed"`
		AccessCount  bool `json:"access_count"`
	} `json:"usage_metadata"`
	RawStorage struct {
		PruneOrphans bool `json:"prune_orphans"`
	} `json:"raw_storage"`
}

// RetentionConfig mirrors the `retention` block. Permanent classes are strings
// ("permanent"); everything else is a duration.
type RetentionConfig struct {
	Project         string   `json:"project"`
	Architecture    string   `json:"architecture"`
	Decisions       string   `json:"decisions"`
	Conventions     string   `json:"conventions"`
	Constraints     string   `json:"constraints"`
	Lessons         Duration `json:"lessons"`
	KnownIssues     Duration `json:"known_issues"`
	TaskContext     Duration `json:"task_context"`
	ResolvedErrors  Duration `json:"resolved_errors"`
	BuildResults    Duration `json:"build_results"`
	TestResults     Duration `json:"test_results"`
	OperationalLogs Duration `json:"operational_logs"`
	ToolOutputs     Duration `json:"tool_outputs"`
	TemporarySearch Duration `json:"temporary_search"`
	DebugDump       Duration `json:"debug_dump"`
}

// ToolsConfig mirrors the `tools` block.
type ToolsConfig struct {
	RTKEnabled               bool `json:"rtk_enabled"`
	CompressOutput           bool `json:"compress_output"`
	SummaryFirst             bool `json:"summary_first"`
	RawOnDemand              bool `json:"raw_on_demand"`
	DeterministicCompression bool `json:"deterministic_compression"`
}

// MCPProxyConfig mirrors the `mcp_proxy` block.
type MCPProxyConfig struct {
	DefaultMode  string `json:"default_mode"`
	GovernedMode string `json:"governed_mode"`
	SafeBypass   bool   `json:"safe_bypass"`
}

// OutputConfig mirrors the `output` block.
type OutputConfig struct {
	Profile                    string `json:"profile"`
	TargetOperationalTokens    int    `json:"target_operational_tokens"`
	OperationalMax             int    `json:"operational_max"`
	StructuredOperational      bool   `json:"structured_operational"`
	SuppressReasoningNarration bool   `json:"suppress_reasoning_narration"`
	SuppressRepetition         bool   `json:"suppress_repetition"`
	ArtifactException          bool   `json:"artifact_exception"`
}

// CacheConfig mirrors the `cache` block.
type CacheConfig struct {
	Enabled                  bool     `json:"enabled"`
	StablePrefix             bool     `json:"stable_prefix"`
	Order                    []string `json:"order"`
	ProviderCapabilities     string   `json:"provider_capabilities"`
	VerifyUsage              bool     `json:"verify_usage"`
	DiagnosePrefixHashes     bool     `json:"diagnose_prefix_hashes"`
	NeverClaimUnobservedHits bool     `json:"never_claim_unobserved_hits"`
	CacheKeyHashOnly         bool     `json:"cache_key_hash_only"`
}

// TelemetryConfig mirrors the `telemetry` block.
type TelemetryConfig struct {
	Enabled                      bool `json:"enabled"`
	RequestLedger                bool `json:"request_ledger"`
	TaskLedger                   bool `json:"task_ledger"`
	CostPerTask                  bool `json:"cost_per_task"`
	DistinguishEstimatedObserved bool `json:"distinguish_estimated_observed"`
	OpenTelemetry                bool `json:"opentelemetry"`
	// RetainRequestEvents bounds the request ledger.
	RetainRequestEvents Duration `json:"retain_request_events"`
}

// Config is the consolidated DWYT v5 configuration.
type Config struct {
	Version string `json:"version"`
	MCPs    struct {
		Enabled []string `json:"enabled"`
	} `json:"mcps"`
	Context     ContextConfig            `json:"context"`
	Retrieval   RetrievalConfig          `json:"retrieval"`
	Memory      MemoryConfig             `json:"memory"`
	Housekeeper HousekeeperConfig        `json:"housekeeper"`
	Retention   RetentionConfig          `json:"retention"`
	Tools       ToolsConfig              `json:"tools"`
	MCPProxy    MCPProxyConfig           `json:"mcp_proxy"`
	Output      OutputConfig             `json:"output"`
	Cache       CacheConfig              `json:"cache"`
	Routing     contextgov.RoutingConfig `json:"routing"`
	Limits      contextgov.StopLimits    `json:"limits"`
	Telemetry   TelemetryConfig          `json:"telemetry"`

	// source records where the config came from, for the dashboard.
	source string `json:"-"`
}

// Default returns the recommended configuration from spec §57.
func Default() Config {
	c := Config{Version: Version, source: "default"}
	c.MCPs.Enabled = []string{"dwyt", "obsidian", "codebase"}

	c.Context = ContextConfig{
		Mode:                 "adaptive",
		DefaultBudget:        contextgov.DefaultBudget,
		MaxBudget:            contextgov.MaxBudget,
		ReservePercent:       contextgov.DefaultReservePercent,
		ProgressiveExpansion: true,
		ConfidenceGated:      true,
		PreserveCachePrefix:  true,
		ReuseBeforeRetrieve:  true,
	}
	c.Retrieval = RetrievalConfig{
		Strategy: "progressive", DefaultTopK: 5,
		PreferProjectMap: true, PreferSymbols: true, PreferRanges: true,
		PreferDeltas: true, FullFile: "exceptional", SemanticDedup: true,
	}
	c.Memory = MemoryConfig{
		Brain: "obsidian", CanonicalMemory: true, HierarchicalSummaries: true,
		CompactSnapshots: true, SnapshotStateHash: true, HotWarmCold: true,
		RawExternalStore: true, ExcludeRawFromDefaultSearch: true,
		LifecycleGC: true, TTLEnabled: true,
	}

	hk := housekeeper.DefaultConfig()
	c.Housekeeper.Enabled = hk.Enabled
	c.Housekeeper.Sessions.KeepLatest = hk.KeepLatestSessions
	c.Housekeeper.Sessions.OrderBy = "last_activity"
	c.Housekeeper.Run.OnSessionClose = hk.RunOnSessionClose
	c.Housekeeper.Run.OnStartup = hk.RunOnStartup
	c.Housekeeper.Run.Interval = Duration(hk.Interval)
	c.Housekeeper.BeforeDelete.ExtractReusableKnowledge = hk.ExtractReusableKnowledge
	c.Housekeeper.BeforeDelete.PromoteToCanonicalMemory = hk.PromoteToCanonicalMemory
	c.Housekeeper.StaleDetection.SourceHash = hk.StaleDetectionBySourceHash
	c.Housekeeper.UsageMetadata.LastAccessed = true
	c.Housekeeper.UsageMetadata.AccessCount = true
	c.Housekeeper.RawStorage.PruneOrphans = hk.PruneRawOrphans

	day := Duration(24 * time.Hour)
	c.Retention = RetentionConfig{
		Project: "permanent", Architecture: "permanent", Decisions: "permanent",
		Conventions: "permanent", Constraints: "permanent",
		Lessons: 180 * day, KnownIssues: 180 * day,
		TaskContext: 14 * day, ResolvedErrors: 30 * day,
		BuildResults: 7 * day, TestResults: 7 * day,
		OperationalLogs: 3 * day, ToolOutputs: Duration(72 * time.Hour),
		TemporarySearch: day, DebugDump: day,
	}
	c.Tools = ToolsConfig{
		RTKEnabled: true, CompressOutput: true, SummaryFirst: true,
		RawOnDemand: true, DeterministicCompression: true,
	}
	c.MCPProxy = MCPProxyConfig{
		DefaultMode:  string(mcpproxy.ModeTransparent),
		GovernedMode: "opt_in", SafeBypass: true,
	}
	c.Output = OutputConfig{
		Profile: "adaptive", TargetOperationalTokens: 300, OperationalMax: 500,
		StructuredOperational: true, SuppressReasoningNarration: true,
		SuppressRepetition: true, ArtifactException: true,
	}
	c.Cache = CacheConfig{
		Enabled: true, StablePrefix: true,
		Order:                []string{"immutable", "long_lived", "session", "volatile"},
		ProviderCapabilities: "auto", VerifyUsage: true,
		DiagnosePrefixHashes: true, NeverClaimUnobservedHits: true,
		CacheKeyHashOnly: true,
	}
	c.Routing = contextgov.DefaultRoutingConfig()
	c.Limits = contextgov.DefaultStopLimits()
	c.Telemetry = TelemetryConfig{
		Enabled: true, RequestLedger: true, TaskLedger: true, CostPerTask: true,
		DistinguishEstimatedObserved: true, OpenTelemetry: true,
		RetainRequestEvents: 30 * day,
	}
	return c
}

// Path is the on-disk location of the consolidated config.
func Path(dwytHome string) string {
	return filepath.Join(dwytHome, "config", "dwyt.json")
}

// Load reads the configuration, merging the file over the defaults.
//
// Absence is not an error; malformed content is. Returning the defaults *and* the
// error lets a caller keep running on defaults while surfacing the problem, which
// is better than either crashing or hiding it.
func Load(dwytHome string) (Config, error) {
	cfg := Default()
	path := Path(dwytHome)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}
	// Decoding into the already-populated struct is what makes a partial file a
	// merge instead of a replacement.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("config: %s is not valid JSON: %w", path, err)
	}
	cfg.source = path
	cfg.normalize()
	return cfg, nil
}

// normalize repairs values that would otherwise be nonsensical, rather than
// failing the load. An out-of-range number in a config file is a typo, and the
// useful response is to use the default and keep working.
func (c *Config) normalize() {
	if c.Version == "" {
		c.Version = Version
	}
	d := Default()
	if c.Context.DefaultBudget <= 0 {
		c.Context.DefaultBudget = d.Context.DefaultBudget
	}
	if c.Context.MaxBudget <= 0 {
		c.Context.MaxBudget = d.Context.MaxBudget
	}
	if c.Context.MaxBudget < c.Context.DefaultBudget {
		// A ceiling below the default is almost certainly a mistake, and honouring
		// it literally would clamp every task to the ceiling. Keep the ceiling
		// (the user did set it) but lower the default to fit under it.
		c.Context.DefaultBudget = c.Context.MaxBudget
	}
	if c.Context.ReservePercent <= 0 || c.Context.ReservePercent >= 100 {
		c.Context.ReservePercent = d.Context.ReservePercent
	}
	if c.Retrieval.DefaultTopK <= 0 {
		c.Retrieval.DefaultTopK = d.Retrieval.DefaultTopK
	}
	if c.Housekeeper.Sessions.KeepLatest <= 0 {
		c.Housekeeper.Sessions.KeepLatest = d.Housekeeper.Sessions.KeepLatest
	}
	if c.Housekeeper.Run.Interval <= 0 {
		c.Housekeeper.Run.Interval = d.Housekeeper.Run.Interval
	}
	if c.Output.TargetOperationalTokens <= 0 {
		c.Output.TargetOperationalTokens = d.Output.TargetOperationalTokens
	}
	if c.Output.OperationalMax < c.Output.TargetOperationalTokens {
		c.Output.OperationalMax = c.Output.TargetOperationalTokens
	}
	if len(c.MCPs.Enabled) == 0 {
		c.MCPs.Enabled = d.MCPs.Enabled
	}
	if len(c.Cache.Order) == 0 {
		c.Cache.Order = d.Cache.Order
	}
}

// Source reports where the config came from.
func (c Config) Source() string {
	if c.source == "" {
		return "default"
	}
	return c.source
}

// Save writes the configuration, creating the directory as needed. Used by
// `dwyt config init` so a user has a complete file to edit rather than a blank
// one they must reconstruct from the docs.
func Save(dwytHome string, cfg Config) error {
	path := Path(dwytHome)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("config: create dir: %w", err)
	}
	data, err := json.MarshalIndent(&cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// GovernorConfig projects the consolidated config onto the Governor's own.
func (c Config) GovernorConfig() governor.Config {
	g := governor.DefaultConfig()
	g.DefaultBudget = c.Context.DefaultBudget
	g.MaxBudget = c.Context.MaxBudget
	g.ReservePercent = c.Context.ReservePercent
	g.ProgressiveExpansion = c.Context.ProgressiveExpansion
	g.ConfidenceGated = c.Context.ConfidenceGated
	g.PreserveCachePrefix = c.Context.PreserveCachePrefix
	g.ReuseBeforeRetrieve = c.Context.ReuseBeforeRetrieve
	g.DefaultTopK = c.Retrieval.DefaultTopK
	g.OperationalOutputTarget = c.Output.TargetOperationalTokens
	g.OperationalOutputMax = c.Output.OperationalMax
	g.StructuredOperational = c.Output.StructuredOperational
	g.StopLimits = c.Limits
	g.Routing = c.Routing
	if ttl := c.Retention.ToolOutputs.Std(); ttl > 0 {
		g.RawTTL = ttl
	}
	return g
}

// HousekeeperConfig projects the consolidated config onto the housekeeper's own.
func (c Config) HousekeeperConfig() housekeeper.Config {
	h := housekeeper.DefaultConfig()
	h.Enabled = c.Housekeeper.Enabled
	h.KeepLatestSessions = c.Housekeeper.Sessions.KeepLatest
	h.RunOnSessionClose = c.Housekeeper.Run.OnSessionClose
	h.RunOnStartup = c.Housekeeper.Run.OnStartup
	h.Interval = c.Housekeeper.Run.Interval.Std()
	h.ExtractReusableKnowledge = c.Housekeeper.BeforeDelete.ExtractReusableKnowledge
	h.PromoteToCanonicalMemory = c.Housekeeper.BeforeDelete.PromoteToCanonicalMemory
	h.StaleDetectionBySourceHash = c.Housekeeper.StaleDetection.SourceHash
	h.PruneRawOrphans = c.Housekeeper.RawStorage.PruneOrphans

	// Retention overrides, keyed by the note types the brain uses. Permanent
	// classes are absent from the map so the brain's own classification applies.
	if !c.Memory.TTLEnabled {
		// TTLs disabled: override every expiring type with zero, which the
		// housekeeper reads as "never expire".
		h.TTLOverrides = map[string]time.Duration{
			"context": 0, "task_context": 0, "task": 0,
			"knowledge": 0, "lessons": 0, "known_issues": 0,
			"log": 0, "command": 0, "debug": 0, "session": 0,
		}
		return h
	}
	h.TTLOverrides = map[string]time.Duration{
		"lessons":          c.Retention.Lessons.Std(),
		"known_issues":     c.Retention.KnownIssues.Std(),
		"task_context":     c.Retention.TaskContext.Std(),
		"context":          c.Retention.TaskContext.Std(),
		"task":             c.Retention.TaskContext.Std(),
		"resolved_errors":  c.Retention.ResolvedErrors.Std(),
		"build_results":    c.Retention.BuildResults.Std(),
		"test_results":     c.Retention.TestResults.Std(),
		"operational_logs": c.Retention.OperationalLogs.Std(),
		"log":              c.Retention.OperationalLogs.Std(),
		"command":          c.Retention.OperationalLogs.Std(),
		"tool_outputs":     c.Retention.ToolOutputs.Std(),
		"temporary_search": c.Retention.TemporarySearch.Std(),
		"debug_dump":       c.Retention.DebugDump.Std(),
		"debug":            c.Retention.DebugDump.Std(),
	}
	return h
}

// ProxyMode projects the configured default proxy mode.
func (c Config) ProxyMode() mcpproxy.Mode {
	return mcpproxy.ParseMode(c.MCPProxy.DefaultMode)
}
