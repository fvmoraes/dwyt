package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// The startup tax report makes the MCP tool-schema overhead visible and
// auditable (Fine-Tuning §8): three first-party MCPs, per-tool sizes,
// estimated tokens clearly labeled estimated — never presented as
// provider-observed usage.

func TestStartupTaxReportShape(t *testing.T) {
	report := MeasureStartupTax(nil)
	if len(report.MCPs) != 2 {
		t.Fatalf("expected 2 first-party MCP servers (optimizer, obsidian), got %d", len(report.MCPs))
	}
	var optimizer, obsidian *MCPTaxReport
	for i := range report.MCPs {
		switch report.MCPs[i].Name {
		case "dwyt_optimizer":
			optimizer = &report.MCPs[i]
		case "dwyt_obsidian":
			obsidian = &report.MCPs[i]
		}
	}
	if optimizer == nil || obsidian == nil {
		t.Fatalf("missing MCPs in report: %+v", report.MCPs)
	}
	if optimizer.Tools != 12 {
		t.Errorf("optimizer tool count = %d, want 12", optimizer.Tools)
	}
	if obsidian.Tools != 9 {
		t.Errorf("obsidian tool count = %d, want 9", obsidian.Tools)
	}
	for _, m := range report.MCPs {
		if m.SerializedBytes <= 0 || m.EstimatedTokens <= 0 {
			t.Errorf("%s: bytes=%d tokens=%d, both must be measured", m.Name, m.SerializedBytes, m.EstimatedTokens)
		}
		if len(m.PerTool) != m.Tools {
			t.Errorf("%s: per_tool entries = %d, want %d", m.Name, len(m.PerTool), m.Tools)
		}
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
	for _, m := range report.MCPs {
		sum := 0
		for _, tt := range m.PerTool {
			if tt.Name == "" {
				t.Fatalf("%s: per-tool entry without a name", m.Name)
			}
			sum += tt.EstimatedTokens
		}
		// Array serialization adds separators/wrapping the per-object marshal
		// does not, so the sum is approximate — bounded drift, not exact.
		drift := sum - m.EstimatedTokens
		if drift < 0 {
			drift = -drift
		}
		if drift > 10 {
			t.Fatalf("%s: per-tool tokens sum to %d, total says %d (drift %d > 10)", m.Name, sum, m.EstimatedTokens, drift)
		}
	}
}

// TestStartupTaxSerializationIsHonest pins that the measured bytes are what
// the client actually receives from tools/list: a JSON object with a tools
// array that parses back into the reported tool count.
func TestStartupTaxSerializationIsHonest(t *testing.T) {
	report := MeasureStartupTax(nil)
	for _, m := range report.MCPs {
		var decoded struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.Unmarshal([]byte(m.Serialized), &decoded); err != nil {
			t.Fatalf("%s: serialized payload does not parse: %v", m.Name, err)
		}
		if len(decoded.Tools) != m.Tools {
			t.Fatalf("%s: serialized %d tools, report says %d", m.Name, len(decoded.Tools), m.Tools)
		}
	}
}

// TestStartupTaxGate pins the CI regression ceiling: tool schemas must not
// grow silently (Fine-Tuning §8.3, §28.8). The ceiling derives from the
// Fase 0/Fase 8 baseline with a deliberate margin; raising it is a decision
// that belongs in a commit message with numbers, not an accident.
func TestStartupTaxGate(t *testing.T) {
	const gateTokens = 3700 // measured baseline @ Fase 8: 3589 est. tokens (21 tools) + ~3% margin
	report := MeasureStartupTax(nil)
	if report.TotalEstimatedTokens > gateTokens {
		t.Fatalf("startup tax grew to %d est. tokens (gate %d). If this growth is justified, document the before/after table in the commit (Fine-Tuning §8.3); otherwise shorten descriptions (Law: no startup-token regression without evidence).",
			report.TotalEstimatedTokens, gateTokens)
	}
}

// TestManagedInstructionCounted keeps the instruction block inside the same
// audit: the Entry Contract size feeds the same startup-tax math.
func TestManagedInstructionCounted(t *testing.T) {
	instruction := []byte(strings.Repeat("# DWYT\n", 100))
	report := MeasureStartupTax(instruction)
	if report.ManagedInstructionBytes != len(instruction) {
		t.Fatalf("instruction bytes = %d, want %d", report.ManagedInstructionBytes, len(instruction))
	}
	if report.ManagedInstructionTokens <= 0 {
		t.Fatal("instruction tokens must be estimated")
	}
}
