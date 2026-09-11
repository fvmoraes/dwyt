package benchmark

import "github.com/fvmoraes/dwyt/internal/telemetry"

// CompressionExpectation records the behavior a deterministic fixture is
// intended to exercise. The negative and no-op cases make the benchmark prove
// that it does not compress merely because compression exists.
type CompressionExpectation string

const (
	CompressionMayApply            CompressionExpectation = "may_apply"
	CompressionExpectedPassthrough CompressionExpectation = "passthrough"
	CompressionNotNeeded           CompressionExpectation = "not_needed"
)

// CompressionOutcome is the result of the deterministic compression path for a
// scenario. It is reported separately from arm totals because it is a behavior
// assertion, not a provider measurement.
type CompressionOutcome struct {
	Attempted      bool                   `json:"attempted"`
	Expected       CompressionExpectation `json:"expected"`
	PassedThrough  bool                   `json:"passed_through"`
	RawTokens      int                    `json:"raw_tokens"`
	SentTokens     int                    `json:"sent_tokens"`
	MetadataTokens int                    `json:"metadata_tokens"`
	Provenance     telemetry.Provenance   `json:"provenance"`
}

const (
	metricRetrievalTokens        = "retrieval_tokens"
	metricRawRecoveryCount       = "raw_recovery_count"
	metricFullFileReads          = "full_file_reads"
	metricMCPCalls               = "mcp_calls"
	metricTaskPassed             = "task_passed"
	metricTokensPerCompletedTask = "tokens_per_completed_task"
	metricOutputBudget           = "output_budget"
	metricCostUnits              = "cost_units"
)

func setBenchmarkMetricProvenance(m *Measurement) {
	m.RetrievalTokens = m.CodeTokens + m.MemoryTokens
	m.RawRecoveryCount = 0
	m.Provenance = telemetry.MetricProvenance{
		telemetry.MetricInputTokens:               telemetry.ProvenanceBenchmarkCounterfactual,
		telemetry.MetricOutputTokens:              telemetry.ProvenanceUnsupported,
		metricRetrievalTokens:                     telemetry.ProvenanceBenchmarkCounterfactual,
		telemetry.MetricCompressionMetadataTokens: telemetry.ProvenanceBenchmarkCounterfactual,
		metricRawRecoveryCount:                    telemetry.ProvenanceBenchmarkCounterfactual,
		metricFullFileReads:                       telemetry.ProvenanceBenchmarkCounterfactual,
		metricMCPCalls:                            telemetry.ProvenanceUnsupported,
		telemetry.MetricLatencyMS:                 telemetry.ProvenanceUnsupported,
		metricTaskPassed:                          telemetry.ProvenanceUnsupported,
		metricTokensPerCompletedTask:              telemetry.ProvenanceUnsupported,
		telemetry.MetricToolTokens:                telemetry.ProvenanceBenchmarkCounterfactual,
		metricOutputBudget:                        telemetry.ProvenanceBenchmarkCounterfactual,
		metricCostUnits:                           telemetry.ProvenanceBenchmarkCounterfactual,
	}
}
