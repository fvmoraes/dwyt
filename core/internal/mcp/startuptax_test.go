package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
)

const startupTaxBaselinePath = "testdata/startup-tax-baseline.json"

type startupTaxBaseline struct {
	Version                   int                      `json:"version"`
	SchemaGateEstimatedTokens int                      `json:"schema_gate_estimated_tokens"`
	MCPs                      []startupTaxMCPBaseline  `json:"mcps"`
	ManagedInstruction        startupTaxBaselineMetric `json:"managed_instruction"`
}

type startupTaxMCPBaseline struct {
	Name            string `json:"name"`
	Availability    string `json:"availability"`
	Tools           int    `json:"tools"`
	SerializedBytes int    `json:"tools_list_result_bytes"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

type startupTaxBaselineMetric struct {
	Bytes           int `json:"bytes"`
	EstimatedTokens int `json:"estimated_tokens"`
}

func TestStartupTaxReportShape(t *testing.T) {
	report := MeasureStartupTax(nil)
	names := mcpregistry.CanonicalNames()
	if len(report.MCPs) != len(names) {
		t.Fatalf("MCP count = %d, want canonical count %d", len(report.MCPs), len(names))
	}
	for i, name := range names {
		if report.MCPs[i].Name != name {
			t.Fatalf("MCP %d = %q, want canonical %q", i, report.MCPs[i].Name, name)
		}
	}

	if report.Coverage.TotalMCPs != 3 || report.Coverage.MeasuredMCPs != 2 || report.Coverage.UnknownMCPs != 1 {
		t.Fatalf("coverage = %+v, want total=3 measured=2 unknown=1", report.Coverage)
	}

	optimizer := startupTaxMCP(t, report, mcpregistry.ServerOptimizer)
	obsidian := startupTaxMCP(t, report, mcpregistry.ServerObsidian)
	codebase := startupTaxMCP(t, report, mcpregistry.ServerCodebase)
	for _, mcpReport := range []MCPTaxReport{optimizer, obsidian} {
		if mcpReport.Availability != MCPAvailabilityMeasured {
			t.Errorf("%s availability = %q, want measured", mcpReport.Name, mcpReport.Availability)
		}
		if mcpReport.SerializedBytes <= 0 || mcpReport.EstimatedTokens <= 0 {
			t.Errorf("%s: bytes=%d tokens=%d, both must be measured", mcpReport.Name, mcpReport.SerializedBytes, mcpReport.EstimatedTokens)
		}
		if len(mcpReport.PerTool) != mcpReport.Tools {
			t.Errorf("%s: per_tool entries = %d, want %d", mcpReport.Name, len(mcpReport.PerTool), mcpReport.Tools)
		}
	}
	if codebase.Availability != MCPAvailabilityUnknown || codebase.UnavailableReason == "" {
		t.Fatalf("codebase catalog must be explicit unknown, got %+v", codebase)
	}
	if codebase.Tools != 0 || codebase.SerializedBytes != 0 || codebase.EstimatedTokens != 0 {
		t.Fatalf("unknown codebase catalog must not fabricate metrics, got %+v", codebase)
	}
	if report.TotalEstimatedTokens <= 0 {
		t.Error("total tokens must be positive")
	}
	if report.Provenance != "estimated" {
		t.Fatalf("provenance = %q, want estimated (Law 14: estimates are labeled)", report.Provenance)
	}
}

func TestStartupTaxPerToolSumsToTotal(t *testing.T) {
	report := MeasureStartupTax(nil)
	for _, mcpReport := range report.MCPs {
		if mcpReport.Availability != MCPAvailabilityMeasured {
			continue
		}
		sum := 0
		for _, tool := range mcpReport.PerTool {
			if tool.Name == "" {
				t.Fatalf("%s: per-tool entry without a name", mcpReport.Name)
			}
			sum += tool.EstimatedTokens
		}
		// Array serialization adds separators/wrapping the per-object marshal
		// does not, so the sum is approximate — bounded drift, not exact.
		drift := sum - mcpReport.EstimatedTokens
		if drift < 0 {
			drift = -drift
		}
		if drift > 10 {
			t.Fatalf("%s: per-tool tokens sum to %d, total says %d (drift %d > 10)", mcpReport.Name, sum, mcpReport.EstimatedTokens, drift)
		}
	}
}

func TestStartupTaxSerializationIsHonest(t *testing.T) {
	report := MeasureStartupTax(nil)
	for _, mcpReport := range report.MCPs {
		if mcpReport.Availability != MCPAvailabilityMeasured {
			continue
		}
		var decoded struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.Unmarshal([]byte(mcpReport.Serialized), &decoded); err != nil {
			t.Fatalf("%s: serialized payload does not parse: %v", mcpReport.Name, err)
		}
		if len(decoded.Tools) != mcpReport.Tools {
			t.Fatalf("%s: serialized %d tools, report says %d", mcpReport.Name, len(decoded.Tools), mcpReport.Tools)
		}
	}
}

func TestMeasureStartupTaxDoesNotCreateRuntimeFiles(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "logs", "startup-tax.log")
	t.Setenv("MCP_LOG", logPath)

	MeasureStartupTax(nil)

	_, err := os.Stat(filepath.Dir(logPath))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("measurement created runtime log directory: stat error = %v", err)
	}
}

func TestStartupTaxRegressionGate(t *testing.T) {
	baseline := loadStartupTaxBaseline(t)
	report := MeasureStartupTax(nil)
	if report.TotalEstimatedTokens > baseline.SchemaGateEstimatedTokens {
		t.Fatalf("startup tax grew to %d est. tokens (gate %d). If this growth is justified, document the before/after table in the commit; otherwise shorten descriptions.", report.TotalEstimatedTokens, baseline.SchemaGateEstimatedTokens)
	}
}

func TestStartupTaxBaseline(t *testing.T) {
	baseline := loadStartupTaxBaseline(t)
	if baseline.Version != 2 {
		t.Fatalf("baseline version = %d, want 2", baseline.Version)
	}

	report := MeasureStartupTax([]byte(integrate.InstructionBlock()))
	if len(report.MCPs) != len(baseline.MCPs) {
		t.Fatalf("report has %d MCPs, baseline has %d", len(report.MCPs), len(baseline.MCPs))
	}
	for _, expected := range baseline.MCPs {
		actual := startupTaxMCP(t, report, expected.Name)
		if actual.Availability != expected.Availability || actual.Tools != expected.Tools || actual.SerializedBytes != expected.SerializedBytes || actual.EstimatedTokens != expected.EstimatedTokens {
			t.Fatalf("%s differs from frozen baseline: got availability=%s tools=%d bytes=%d tokens=%d, want availability=%s tools=%d bytes=%d tokens=%d", actual.Name, actual.Availability, actual.Tools, actual.SerializedBytes, actual.EstimatedTokens, expected.Availability, expected.Tools, expected.SerializedBytes, expected.EstimatedTokens)
		}
	}
	if report.ManagedInstructionBytes != baseline.ManagedInstruction.Bytes || report.ManagedInstructionTokens != baseline.ManagedInstruction.EstimatedTokens {
		t.Fatalf("instruction block differs from frozen baseline: got bytes=%d tokens=%d, want bytes=%d tokens=%d", report.ManagedInstructionBytes, report.ManagedInstructionTokens, baseline.ManagedInstruction.Bytes, baseline.ManagedInstruction.EstimatedTokens)
	}
}

func TestInstructionBlockCounted(t *testing.T) {
	instruction := []byte(integrate.InstructionBlock())
	if len(instruction) == 0 {
		t.Fatal("managed instruction block must not be empty")
	}

	report := MeasureStartupTax(instruction)
	schemaOnly := MeasureStartupTax(nil)
	if report.ManagedInstructionBytes != len(instruction) {
		t.Fatalf("instruction bytes = %d, want %d", report.ManagedInstructionBytes, len(instruction))
	}
	if report.ManagedInstructionTokens <= 0 {
		t.Fatal("instruction tokens must be estimated")
	}
	if report.TotalEstimatedTokens != schemaOnly.TotalEstimatedTokens+report.ManagedInstructionTokens {
		t.Fatalf("total tokens = %d, want schema %d + instruction %d", report.TotalEstimatedTokens, schemaOnly.TotalEstimatedTokens, report.ManagedInstructionTokens)
	}
}

func startupTaxMCP(t *testing.T, report StartupTaxReport, name string) MCPTaxReport {
	t.Helper()
	for _, mcpReport := range report.MCPs {
		if mcpReport.Name == name {
			return mcpReport
		}
	}
	t.Fatalf("missing MCP %q from report: %+v", name, report.MCPs)
	return MCPTaxReport{}
}

func loadStartupTaxBaseline(t *testing.T) startupTaxBaseline {
	t.Helper()
	data, err := os.ReadFile(startupTaxBaselinePath)
	if err != nil {
		t.Fatalf("read startup-tax baseline: %v", err)
	}
	var baseline startupTaxBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatalf("decode startup-tax baseline: %v", err)
	}
	return baseline
}
