package telemetry

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestTelemetryProvenanceFieldsRoundTrip(t *testing.T) {
	store := testStore(t)
	input, before, after, metadata := 1000, 1200, 400, 0
	event := RequestEvent{
		ProjectID:                 "p1",
		InputTokens:               &input,
		ContextBefore:             &before,
		ContextAfter:              &after,
		CompressionMetadataTokens: &metadata,
		Observed:                  true,
		Provenance: MetricProvenance{
			MetricInputTokens:               ProvenanceObserved,
			MetricContextBeforeDWYT:         ProvenanceEstimated,
			MetricContextAfterDWYT:          ProvenanceEstimated,
			MetricCompressionMetadataTokens: ProvenanceEstimated,
		},
	}
	if err := store.RecordRequest(event); err != nil {
		t.Fatal(err)
	}

	events, err := store.RecentRequests("p1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := events[0]
	if got.Provenance.For(MetricInputTokens) != ProvenanceObserved {
		t.Fatalf("input provenance = %q", got.Provenance.For(MetricInputTokens))
	}
	if got.Provenance.For(MetricContextBeforeDWYT) != ProvenanceEstimated || got.Provenance.For(MetricContextAfterDWYT) != ProvenanceEstimated {
		t.Fatalf("context provenance = %+v", got.Provenance)
	}
	if got.Provenance.For(MetricCompressionMetadataTokens) != ProvenanceEstimated {
		t.Fatalf("metadata provenance = %q", got.Provenance.For(MetricCompressionMetadataTokens))
	}
	if got.Provenance.For(MetricOutputTokens) != ProvenanceUnsupported {
		t.Fatalf("missing output provenance = %q, want unsupported", got.Provenance.For(MetricOutputTokens))
	}
	for _, metric := range RequestMetricNames() {
		if _, ok := got.Provenance[metric]; !ok {
			t.Errorf("round-trip omitted provenance for %s", metric)
		}
	}

	summary, err := store.Summarize("p1", time.Unix(0, 0), "all")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Provenance.For(MetricAvoidedTokens) != ProvenanceEstimated {
		t.Fatalf("avoided provenance = %q", summary.Provenance.For(MetricAvoidedTokens))
	}
	if summary.Provenance.For(MetricCompressionMetadataTokens) != ProvenanceEstimated {
		t.Fatalf("metadata summary provenance = %q", summary.Provenance.For(MetricCompressionMetadataTokens))
	}
	if summary.Coverage.CompressionMetadataReported != 1 || summary.CompressionMetadataTokens != 0 {
		t.Fatalf("metadata coverage/value = %+v / %d", summary.Coverage, summary.CompressionMetadataTokens)
	}
}

func TestTelemetryPartialContextRemainsUnsupported(t *testing.T) {
	store := testStore(t)
	before, after, metadata := 1000, 200, 0
	if err := store.RecordRequest(RequestEvent{
		ProjectID: "p1", ContextBefore: &before, ContextAfter: &after, CompressionMetadataTokens: &metadata,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRequest(RequestEvent{ProjectID: "p1", InputTokens: ptrInt(10)}); err != nil {
		t.Fatal(err)
	}

	summary, err := store.Summarize("p1", time.Unix(0, 0), "all")
	if err != nil {
		t.Fatal(err)
	}
	if summary.ContextReductionPct != nil {
		t.Fatal("partial context coverage must not produce a context reduction percentage")
	}
	if summary.Provenance.For(MetricAvoidedTokens) != ProvenanceUnsupported {
		t.Fatalf("partial avoided provenance = %q", summary.Provenance.For(MetricAvoidedTokens))
	}
	if summary.Provenance.For(MetricCompressionMetadataTokens) != ProvenanceUnsupported {
		t.Fatalf("partial metadata provenance = %q", summary.Provenance.For(MetricCompressionMetadataTokens))
	}
}

func TestMigrateAddsProvenanceColumnsToExistingDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE llm_request_events (
			id TEXT PRIMARY KEY, project_id TEXT NOT NULL, task_id TEXT, provider TEXT, model TEXT, phase TEXT,
			input_tokens INTEGER, uncached_input_tokens INTEGER, cached_input_tokens INTEGER,
			cache_write_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, tool_tokens INTEGER,
			context_before INTEGER, context_after INTEGER, estimated_cost_usd REAL, actual_cost_usd REAL,
			cache_key_hash TEXT, prefix_hash TEXT, latency_ms INTEGER, observed INTEGER NOT NULL DEFAULT 0, ts INTEGER NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	store, err := New(db)
	if err != nil {
		t.Fatalf("migrate legacy db: %v", err)
	}
	for _, column := range []string{"compression_metadata_tokens", "metric_provenance"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('llm_request_events') WHERE name = ?`, column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("legacy migration did not add %s", column)
		}
	}
	var migrations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM telemetry_schema_migrations WHERE version = 2`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 1 {
		t.Fatalf("migration v2 rows = %d", migrations)
	}
	zero := 0
	if err := store.RecordRequest(RequestEvent{ProjectID: "legacy", CompressionMetadataTokens: &zero}); err != nil {
		t.Fatalf("record against migrated db: %v", err)
	}
}
func TestInvalidRequestProvenanceStaysUnsupported(t *testing.T) {
	present := map[string]bool{MetricInputTokens: true}
	if got := normalizeRequestProvenance(nil, true, present).For(MetricInputTokens); got != ProvenanceObserved {
		t.Fatalf("missing legacy provenance = %q, want observed fallback", got)
	}
	if got := normalizeRequestProvenance(MetricProvenance{MetricInputTokens: Provenance("typo")}, true, present).For(MetricInputTokens); got != ProvenanceUnsupported {
		t.Fatalf("invalid provenance = %q, want unsupported", got)
	}
	if got := decodeRequestProvenance("{not-json", true, present).For(MetricInputTokens); got != ProvenanceUnsupported {
		t.Fatalf("malformed provenance = %q, want unsupported", got)
	}

	store := testStore(t)
	input := 1
	if err := store.RecordRequest(RequestEvent{
		ProjectID: "p1", InputTokens: &input, Observed: true,
		Provenance: MetricProvenance{MetricInputTokens: Provenance("typo")},
	}); err != nil {
		t.Fatal(err)
	}
	events, err := store.RecentRequests("p1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Provenance.For(MetricInputTokens) != ProvenanceUnsupported {
		t.Fatalf("invalid persisted provenance was promoted: %+v", events)
	}
}
func TestMixedCounterfactualProvenanceContaminatesAggregate(t *testing.T) {
	store := testStore(t)
	before, after, metadata := 100, 20, 0
	for _, event := range []RequestEvent{
		{
			ID: "counterfactual", ProjectID: "p1", ContextBefore: &before, ContextAfter: &after, CompressionMetadataTokens: &metadata,
			Provenance: MetricProvenance{
				MetricContextBeforeDWYT:         ProvenanceBenchmarkCounterfactual,
				MetricContextAfterDWYT:          ProvenanceBenchmarkCounterfactual,
				MetricCompressionMetadataTokens: ProvenanceBenchmarkCounterfactual,
			},
		},
		{
			ID: "estimated", ProjectID: "p1", ContextBefore: &before, ContextAfter: &after, CompressionMetadataTokens: &metadata,
			Provenance: MetricProvenance{
				MetricContextBeforeDWYT:         ProvenanceEstimated,
				MetricContextAfterDWYT:          ProvenanceEstimated,
				MetricCompressionMetadataTokens: ProvenanceEstimated,
			},
		},
	} {
		if err := store.RecordRequest(event); err != nil {
			t.Fatal(err)
		}
	}

	summary, err := store.Summarize("p1", time.Unix(0, 0), "all")
	if err != nil {
		t.Fatal(err)
	}
	if got := summary.Provenance.For(MetricAvoidedTokens); got != ProvenanceBenchmarkCounterfactual {
		t.Fatalf("mixed avoided provenance = %q, want benchmark_counterfactual", got)
	}
	if got := summary.Provenance.For(MetricCompressionMetadataTokens); got != ProvenanceBenchmarkCounterfactual {
		t.Fatalf("mixed metadata provenance = %q, want benchmark_counterfactual", got)
	}
}
