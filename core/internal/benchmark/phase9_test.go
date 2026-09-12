package benchmark

import (
	"reflect"
	"testing"

	"github.com/fvmoraes/dwyt/internal/telemetry"
)

func TestBenchmarkScenariosDeterministic(t *testing.T) {
	first := Scenarios()
	second := Scenarios()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("scenario fixtures changed between deterministic reads")
	}
	for _, scenario := range first {
		if scenario.ID == "A" || scenario.ID == "I" || scenario.ID == "K" {
			continue
		}
		if scenario.ToolOutput == "" {
			t.Fatalf("%s must carry its deterministic testdata payload", scenario.ID)
		}
	}
}

func TestBenchmarkScenarioNegativeCompressionChoosesPassthrough(t *testing.T) {
	scenario, ok := scenarioByID("J")
	if !ok {
		t.Fatal("mandatory negative scenario J is missing")
	}
	result := RunScenario(scenario)
	if !result.Compression.Attempted {
		t.Fatal("negative fixture did not invoke the compression path")
	}
	if result.Compression.Expected != CompressionExpectedPassthrough {
		t.Fatalf("expectation = %q, want passthrough", result.Compression.Expected)
	}
	if !result.Compression.PassedThrough {
		t.Fatalf("tiny fixture was compressed: %+v", result.Compression)
	}
	if result.Compression.RawTokens != result.Compression.SentTokens {
		t.Fatalf("passthrough changed payload cost: raw=%d sent=%d", result.Compression.RawTokens, result.Compression.SentTokens)
	}
	if result.Compression.MetadataTokens != 0 {
		t.Fatalf("passthrough must not report compression metadata, got %d", result.Compression.MetadataTokens)
	}
}

func TestBenchmarkMeasurementsCarryMetricProvenance(t *testing.T) {
	for _, result := range Run().Results {
		for _, measurement := range result.Arms {
			if got := measurement.Provenance.For(telemetry.MetricInputTokens); got != telemetry.ProvenanceBenchmarkCounterfactual {
				t.Errorf("%s/%s input provenance = %q", result.Scenario, measurement.Arm, got)
			}
			if got := measurement.Provenance.For(telemetry.MetricCompressionMetadataTokens); got != telemetry.ProvenanceBenchmarkCounterfactual {
				t.Errorf("%s/%s metadata provenance = %q", result.Scenario, measurement.Arm, got)
			}
			for _, unavailable := range []string{telemetry.MetricOutputTokens, metricMCPCalls, telemetry.MetricLatencyMS, metricTaskPassed, metricTokensPerCompletedTask} {
				if got := measurement.Provenance.For(unavailable); got != telemetry.ProvenanceUnsupported {
					t.Errorf("%s/%s %s provenance = %q, want unsupported", result.Scenario, measurement.Arm, unavailable, got)
				}
			}
			if measurement.OutputTokens != nil || measurement.MCPCalls != nil || measurement.LatencyMS != nil || measurement.TaskPassed != nil || measurement.TokensPerCompletedTask != nil {
				t.Errorf("%s/%s exposed an unavailable live metric", result.Scenario, measurement.Arm)
			}
			if measurement.RetrievalTokens != measurement.CodeTokens+measurement.MemoryTokens {
				t.Errorf("%s/%s retrieval=%d, want code+memory=%d", result.Scenario, measurement.Arm, measurement.RetrievalTokens, measurement.CodeTokens+measurement.MemoryTokens)
			}
		}
	}
}
