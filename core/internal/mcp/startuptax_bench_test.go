package mcp

import (
	"testing"

	"github.com/fvmoraes/dwyt/internal/integrate"
)

var startupTaxBenchmarkSink StartupTaxReport

func BenchmarkMeasureStartupTax(b *testing.B) {
	cases := []struct {
		name        string
		instruction []byte
	}{
		{name: "schemas"},
		{name: "schemas_with_instruction", instruction: []byte(integrate.InstructionBlock())},
	}
	for _, benchmark := range cases {
		b.Run(benchmark.name, func(b *testing.B) {
			report := MeasureStartupTax(benchmark.instruction)
			schemaBytes, schemaTokens := startupTaxSchemaMetrics(report)
			b.ReportAllocs()

			for b.Loop() {
				startupTaxBenchmarkSink = MeasureStartupTax(benchmark.instruction)
			}

			b.ReportMetric(float64(schemaBytes), "schema_result_bytes")
			b.ReportMetric(float64(schemaTokens), "schema_result_est_tokens")
			b.ReportMetric(float64(report.ManagedInstructionBytes), "instruction_bytes")
			b.ReportMetric(float64(report.ManagedInstructionTokens), "instruction_est_tokens")
			for _, mcpReport := range report.MCPs {
				if mcpReport.Availability != MCPAvailabilityMeasured {
					continue
				}
				b.ReportMetric(float64(mcpReport.SerializedBytes), mcpReport.Name+"_bytes")
				b.ReportMetric(float64(mcpReport.EstimatedTokens), mcpReport.Name+"_est_tokens")
			}
		})
	}
}

func startupTaxSchemaMetrics(report StartupTaxReport) (int, int) {
	var bytes, tokens int
	for _, mcpReport := range report.MCPs {
		bytes += mcpReport.SerializedBytes
		tokens += mcpReport.EstimatedTokens
	}
	return bytes, tokens
}
