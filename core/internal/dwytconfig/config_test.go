package dwytconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultMatchesTheSpecRecommendations(t *testing.T) {
	c := Default()
	if c.Version != Version {
		t.Fatalf("version %q", c.Version)
	}
	if c.Context.DefaultBudget != 48000 || c.Context.MaxBudget != 140000 || c.Context.ReservePercent != 12 {
		t.Fatalf("context defaults drifted from the spec: %+v", c.Context)
	}
	if c.Retrieval.DefaultTopK != 5 {
		t.Fatalf("top-k default should be 5, got %d", c.Retrieval.DefaultTopK)
	}
	if c.Housekeeper.Sessions.KeepLatest != 100 {
		t.Fatalf("the session limit should be 100, got %d", c.Housekeeper.Sessions.KeepLatest)
	}
	if c.Output.TargetOperationalTokens != 300 || c.Output.OperationalMax != 500 {
		t.Fatalf("output defaults drifted: %+v", c.Output)
	}
	if len(c.MCPs.Enabled) != 3 {
		t.Fatalf("exactly three MCPs are official: %v", c.MCPs.Enabled)
	}
	// The honesty guarantees must default to on.
	if !c.Cache.NeverClaimUnobservedHits || !c.Telemetry.DistinguishEstimatedObserved {
		t.Fatalf("honesty defaults must be enabled: %+v %+v", c.Cache, c.Telemetry)
	}
	// Promote-before-delete must default to on, or expiry loses knowledge.
	if !c.Housekeeper.BeforeDelete.ExtractReusableKnowledge ||
		!c.Housekeeper.BeforeDelete.PromoteToCanonicalMemory {
		t.Fatal("knowledge must be promoted before deletion by default")
	}
}

func TestRetentionDefaultsFollowTheSpecTable(t *testing.T) {
	c := Default()
	cases := map[string]struct {
		got  Duration
		want time.Duration
	}{
		"lessons":          {c.Retention.Lessons, 180 * 24 * time.Hour},
		"known_issues":     {c.Retention.KnownIssues, 180 * 24 * time.Hour},
		"task_context":     {c.Retention.TaskContext, 14 * 24 * time.Hour},
		"resolved_errors":  {c.Retention.ResolvedErrors, 30 * 24 * time.Hour},
		"build_results":    {c.Retention.BuildResults, 7 * 24 * time.Hour},
		"operational_logs": {c.Retention.OperationalLogs, 3 * 24 * time.Hour},
		"tool_outputs":     {c.Retention.ToolOutputs, 72 * time.Hour},
		"debug_dump":       {c.Retention.DebugDump, 24 * time.Hour},
	}
	for name, tc := range cases {
		if tc.got.Std() != tc.want {
			t.Fatalf("%s = %v, want %v", name, tc.got.Std(), tc.want)
		}
	}
	for name, value := range map[string]string{
		"project": c.Retention.Project, "architecture": c.Retention.Architecture,
		"decisions": c.Retention.Decisions, "conventions": c.Retention.Conventions,
		"constraints": c.Retention.Constraints,
	} {
		if value != "permanent" {
			t.Fatalf("%s must be permanent, got %q", name, value)
		}
	}
}

func TestParseDurationSupportsDaysAndSession(t *testing.T) {
	cases := map[string]time.Duration{
		"72h":     72 * time.Hour,
		"6h":      6 * time.Hour,
		"180d":    180 * 24 * time.Hour,
		"1d":      24 * time.Hour,
		"session": 0,
		"SESSION": 0,
		"":        0,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil {
			t.Fatalf("ParseDuration(%q) failed: %v", in, err)
		}
		if got != want {
			t.Fatalf("ParseDuration(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseDuration("not a duration"); err == nil {
		t.Fatal("an invalid duration must be reported")
	}
}

func TestDurationJSONRoundTrip(t *testing.T) {
	type holder struct {
		D Duration `json:"d"`
	}
	// String form.
	var h holder
	if err := json.Unmarshal([]byte(`{"d":"180d"}`), &h); err != nil {
		t.Fatal(err)
	}
	if h.D.Std() != 180*24*time.Hour {
		t.Fatalf("parsed %v", h.D.Std())
	}
	out, _ := json.Marshal(h)
	if string(out) != `{"d":"180d"}` {
		t.Fatalf("round trip should stay readable: %s", string(out))
	}

	// Numeric seconds, for a caller that generates the file.
	if err := json.Unmarshal([]byte(`{"d":3600}`), &h); err != nil {
		t.Fatal(err)
	}
	if h.D.Std() != time.Hour {
		t.Fatalf("numeric seconds not honoured: %v", h.D.Std())
	}
	// Garbage must be reported, not silently zeroed.
	if err := json.Unmarshal([]byte(`{"d":{"nope":1}}`), &h); err == nil {
		t.Fatal("an invalid duration value must be rejected")
	}
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("a missing config must not be an error: %v", err)
	}
	if cfg.Source() != "default" {
		t.Fatalf("expected the default source, got %q", cfg.Source())
	}
	if cfg.Context.DefaultBudget != 48000 {
		t.Fatalf("defaults not applied: %+v", cfg.Context)
	}
}

// A malformed config must be reported, otherwise the user believes their tuning
// was applied when it was not.
func TestLoadMalformedFileIsReportedButUsable(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Dir(Path(home)), 0755)
	os.WriteFile(Path(home), []byte("{not json"), 0644)

	cfg, err := Load(home)
	if err == nil {
		t.Fatal("a malformed config must be reported")
	}
	// And the returned config must still be usable, so the daemon can run on
	// defaults instead of refusing to start.
	if cfg.Context.DefaultBudget != 48000 {
		t.Fatalf("the fallback config must be the defaults: %+v", cfg.Context)
	}
}

// A file that sets one value must not reset the other forty.
func TestPartialFileIsMergedNotReplaced(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Dir(Path(home)), 0755)
	os.WriteFile(Path(home), []byte(`{"context":{"default_budget":12000}}`), 0644)

	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Context.DefaultBudget != 12000 {
		t.Fatalf("the user's value should win: %d", cfg.Context.DefaultBudget)
	}
	if cfg.Context.MaxBudget != 140000 {
		t.Fatalf("an unset field must keep its default: %d", cfg.Context.MaxBudget)
	}
	if cfg.Housekeeper.Sessions.KeepLatest != 100 {
		t.Fatalf("an untouched block must keep its defaults: %d", cfg.Housekeeper.Sessions.KeepLatest)
	}
	if !cfg.Housekeeper.BeforeDelete.PromoteToCanonicalMemory {
		t.Fatal("a partial file must not silently disable promote-before-delete")
	}
}

// An out-of-range value in a config file is a typo; the useful response is to
// use the default and keep working.
func TestNormalizeRepairsNonsensicalValues(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Dir(Path(home)), 0755)
	os.WriteFile(Path(home), []byte(`{
		"context": {"default_budget": -5, "reserve_percent": 250},
		"retrieval": {"default_top_k": 0},
		"housekeeper": {"sessions": {"keep_latest": -1}, "run": {"interval": "0s"}},
		"output": {"target_operational_tokens": 0, "operational_max": 10},
		"mcps": {"enabled": []}
	}`), 0644)

	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Context.DefaultBudget <= 0 || cfg.Context.ReservePercent >= 100 {
		t.Fatalf("context not repaired: %+v", cfg.Context)
	}
	if cfg.Retrieval.DefaultTopK != 5 {
		t.Fatalf("top-k not repaired: %d", cfg.Retrieval.DefaultTopK)
	}
	if cfg.Housekeeper.Sessions.KeepLatest != 100 || cfg.Housekeeper.Run.Interval <= 0 {
		t.Fatalf("housekeeper not repaired: %+v", cfg.Housekeeper)
	}
	if cfg.Output.OperationalMax < cfg.Output.TargetOperationalTokens {
		t.Fatalf("an unreachable output max should be raised to the target: %+v", cfg.Output)
	}
	if len(cfg.MCPs.Enabled) != 3 {
		t.Fatalf("an empty MCP list must fall back to the three official ones: %v", cfg.MCPs.Enabled)
	}
}

// A ceiling below the default is a mistake, but the ceiling is what the user
// actually typed; lowering the default to fit is the honest repair.
func TestCeilingBelowDefaultLowersTheDefault(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Dir(Path(home)), 0755)
	os.WriteFile(Path(home), []byte(`{"context":{"max_budget":8000}}`), 0644)

	cfg, _ := Load(home)
	if cfg.Context.MaxBudget != 8000 {
		t.Fatalf("the user's ceiling must be kept: %d", cfg.Context.MaxBudget)
	}
	if cfg.Context.DefaultBudget > cfg.Context.MaxBudget {
		t.Fatalf("the default must fit under the ceiling: %d > %d",
			cfg.Context.DefaultBudget, cfg.Context.MaxBudget)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	home := t.TempDir()
	want := Default()
	want.Context.DefaultBudget = 24000
	want.Housekeeper.Sessions.KeepLatest = 50
	want.Retention.Lessons = Duration(90 * 24 * time.Hour)

	if err := Save(home, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if got.Context.DefaultBudget != 24000 ||
		got.Housekeeper.Sessions.KeepLatest != 50 ||
		got.Retention.Lessons.Std() != 90*24*time.Hour {
		t.Fatalf("round trip lost values: %+v", got)
	}
	if got.Source() == "default" {
		t.Fatal("a loaded file must not report itself as the default source")
	}
}

func TestOptimizerProjection(t *testing.T) {
	cfg := Default()
	cfg.Context.DefaultBudget = 20000
	cfg.Context.ConfidenceGated = false
	cfg.Retrieval.DefaultTopK = 3
	cfg.Output.TargetOperationalTokens = 150
	cfg.Retention.ToolOutputs = Duration(12 * time.Hour)

	g := cfg.OptimizerConfig()
	if g.DefaultBudget != 20000 || g.DefaultTopK != 3 || g.OperationalOutputTarget != 150 {
		t.Fatalf("projection lost values: %+v", g)
	}
	if g.ConfidenceGated {
		t.Fatal("a disabled gate must be projected")
	}
	if g.RawTTL != 12*time.Hour {
		t.Fatalf("the tool-output retention should drive the raw TTL: %v", g.RawTTL)
	}
}

func TestHousekeeperProjectionCarriesRetentionOverrides(t *testing.T) {
	cfg := Default()
	cfg.Retention.TaskContext = Duration(2 * 24 * time.Hour)
	h := cfg.HousekeeperConfig()

	if h.KeepLatestSessions != 100 {
		t.Fatalf("session limit not projected: %d", h.KeepLatestSessions)
	}
	if h.TTLOverrides["context"] != 2*24*time.Hour {
		t.Fatalf("the task-context TTL should drive the context override: %v", h.TTLOverrides["context"])
	}
	// Permanent classes must not appear as overrides: the brain's own
	// classification decides, and a zero here would be read as "never expire"
	// for a type that is already permanent — harmless, but misleading.
	for _, permanent := range []string{"decision", "architecture", "project"} {
		if _, ok := h.TTLOverrides[permanent]; ok {
			t.Fatalf("%s must not be listed as a TTL override", permanent)
		}
	}
}

// Turning TTLs off must actually stop expiry, not merely shorten it.
func TestDisablingTTLsOverridesEveryExpiringType(t *testing.T) {
	cfg := Default()
	cfg.Memory.TTLEnabled = false
	h := cfg.HousekeeperConfig()

	if len(h.TTLOverrides) == 0 {
		t.Fatal("disabling TTLs must produce explicit zero overrides")
	}
	for noteType, ttl := range h.TTLOverrides {
		if ttl != 0 {
			t.Fatalf("%s should never expire when TTLs are off, got %v", noteType, ttl)
		}
	}
}

func TestProxyModeDefaultsToTransparent(t *testing.T) {
	if got := Default().ProxyMode(); string(got) != "transparent" {
		t.Fatalf("the safe default must be transparent, got %s", got)
	}
	cfg := Default()
	cfg.MCPProxy.DefaultMode = "optimized"
	if string(cfg.ProxyMode()) != "optimized" {
		t.Fatal("an explicit optimized mode must be honoured")
	}
	cfg.MCPProxy.DefaultMode = "nonsense"
	if string(cfg.ProxyMode()) != "transparent" {
		t.Fatal("an unrecognised mode must degrade to transparent")
	}
}

func TestFormatDurationPrefersDays(t *testing.T) {
	cases := map[time.Duration]string{
		180 * 24 * time.Hour: "180d",
		24 * time.Hour:       "1d",
		72 * time.Hour:       "3d",
		90 * time.Minute:     "1h30m0s",
		0:                    "0s",
	}
	for in, want := range cases {
		if got := FormatDuration(in); got != want {
			t.Fatalf("FormatDuration(%v) = %q, want %q", in, got, want)
		}
	}
}
