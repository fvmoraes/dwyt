package mcp

import (
	"encoding/json"

	"github.com/fvmoraes/dwyt/internal/contextopt"
)

// MCP Startup Tax / Tool Schema Tax (Fine-Tuning §8): every first-party MCP
// charges the agent a fixed token cost at startup — tool definitions,
// descriptions, JSON schemas. DWYT must be able to quantify that overhead
// before optimizing it, and the numbers must carry their provenance
// (estimated, never provider-observed).

// ToolTax is one tool's contribution to the startup tax.
type ToolTax struct {
	Name            string `json:"name"`
	Bytes           int    `json:"bytes"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

// MCPTaxReport is one MCP server's measured overhead.
type MCPTaxReport struct {
	Name            string    `json:"name"`
	Tools           int       `json:"tools"`
	Serialized      string    `json:"serialized"`
	SerializedBytes int       `json:"serialized_bytes"`
	EstimatedTokens int       `json:"estimated_tokens"`
	PerTool         []ToolTax `json:"per_tool,omitempty"`
}

// StartupTaxReport aggregates every first-party server plus the managed
// instruction block injected into client files.
type StartupTaxReport struct {
	MCPs                     []MCPTaxReport `json:"mcps"`
	TotalEstimatedTokens     int            `json:"total_estimated_tokens"`
	ManagedInstructionBytes  int            `json:"managed_instruction_bytes,omitempty"`
	ManagedInstructionTokens int            `json:"managed_instruction_tokens,omitempty"`
	// Provenance labels every number in this report as an estimate (Law 14).
	Provenance string `json:"provenance"`
}

// MeasureStartupTax measures the first-party MCP tool schemas (optimizer and
// obsidian — codebase is a third-party binary whose catalog is not owned by
// this repository) plus, optionally, the managed instruction block. nil
// instruction skips that line of the report.
func MeasureStartupTax(instruction []byte) StartupTaxReport {
	report := StartupTaxReport{Provenance: "estimated"}
	for _, spec := range []struct {
		name     string
		register func(*Server)
	}{
		{"dwyt_optimizer", RegisterOptimizerTools},
		{"dwyt_obsidian", RegisterObsidianTools},
	} {
		server := NewServer(spec.name, "0.0.0")
		spec.register(server)
		report.MCPs = append(report.MCPs, measureServer(spec.name, server))
	}
	for _, m := range report.MCPs {
		report.TotalEstimatedTokens += m.EstimatedTokens
	}
	if instruction != nil {
		report.ManagedInstructionBytes = len(instruction)
		report.ManagedInstructionTokens = contextopt.EstimateTokens(string(instruction))
		report.TotalEstimatedTokens += report.ManagedInstructionTokens
	}
	return report
}

func measureServer(name string, server *Server) MCPTaxReport {
	serialized := server.ToolsListJSON()
	report := MCPTaxReport{
		Name:            name,
		Tools:           server.ToolCount(),
		Serialized:      string(serialized),
		SerializedBytes: len(serialized),
	}
	report.EstimatedTokens = contextopt.EstimateTokens(report.Serialized)
	for _, tool := range server.ToolPayloads() {
		b, err := json.Marshal(tool)
		if err != nil {
			continue
		}
		report.PerTool = append(report.PerTool, ToolTax{
			Name:            tool.Name,
			Bytes:           len(b),
			EstimatedTokens: contextopt.EstimateTokens(string(b)),
		})
	}
	return report
}
