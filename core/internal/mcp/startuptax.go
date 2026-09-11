package mcp

import (
	"encoding/json"

	"github.com/fvmoraes/dwyt/internal/contextopt"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
)

// MCP Startup Tax / Tool Schema Tax (Fine-Tuning §8): every canonical DWYT MCP
// charges the agent a fixed token cost at startup — tool definitions,
// descriptions, JSON schemas. DWYT must be able to quantify that overhead
// before optimizing it, and the numbers must carry their provenance
// (estimated, never provider-observed).

const (
	// MCPAvailabilityMeasured identifies catalogs DWYT can serialize locally.
	MCPAvailabilityMeasured = "measured"
	// MCPAvailabilityUnknown identifies configured MCPs whose catalog is not
	// available to this process, so their cost cannot be fabricated.
	MCPAvailabilityUnknown = "unknown"
)

// ToolTax is one tool's contribution to the startup tax.
type ToolTax struct {
	Name            string `json:"name"`
	Bytes           int    `json:"bytes"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

// MCPTaxReport is one canonical MCP server's measured or unknown overhead.
type MCPTaxReport struct {
	Name         string `json:"name"`
	Availability string `json:"availability"`
	// UnavailableReason explains why an unknown catalog has no fabricated data.
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	Tools             int    `json:"tools"`
	// Serialized is the schema-bearing JSON value placed in the JSON-RPC
	// tools/list result. It intentionally excludes the transport envelope,
	// whose request ID and newline are client-specific.
	Serialized      string    `json:"tools_list_result"`
	SerializedBytes int       `json:"tools_list_result_bytes"`
	EstimatedTokens int       `json:"estimated_tokens"`
	PerTool         []ToolTax `json:"per_tool,omitempty"`
}

// StartupTaxCoverage makes it explicit when a report total excludes an MCP
// whose tool catalog is not owned by this process.
type StartupTaxCoverage struct {
	TotalMCPs    int `json:"total_mcps"`
	MeasuredMCPs int `json:"measured_mcps"`
	UnknownMCPs  int `json:"unknown_mcps"`
}

// StartupTaxReport aggregates every canonical MCP plus the managed instruction
// block injected into client files. TotalEstimatedTokens includes only measured
// MCP catalogs and the optional instruction block; Coverage identifies any
// catalog intentionally excluded as unknown.
type StartupTaxReport struct {
	MCPs                     []MCPTaxReport     `json:"mcps"`
	Coverage                 StartupTaxCoverage `json:"coverage"`
	TotalEstimatedTokens     int                `json:"total_estimated_tokens"`
	ManagedInstructionBytes  int                `json:"managed_instruction_bytes,omitempty"`
	ManagedInstructionTokens int                `json:"managed_instruction_tokens,omitempty"`
	// Provenance labels every number in this report as an estimate (Law 14).
	Provenance string `json:"provenance"`
}

// MeasureStartupTax deterministically measures local MCP tool schemas plus,
// optionally, the managed instruction block. It constructs in-memory catalogs
// only: no environment, filesystem, network, or provider usage is observed.
func MeasureStartupTax(instruction []byte) StartupTaxReport {
	names := mcpregistry.CanonicalNames()
	report := StartupTaxReport{
		MCPs:       make([]MCPTaxReport, 0, len(names)),
		Provenance: "estimated",
	}
	for _, name := range names {
		mcpReport := measureCatalog(name)
		report.MCPs = append(report.MCPs, mcpReport)
		report.Coverage.TotalMCPs++
		if mcpReport.Availability == MCPAvailabilityMeasured {
			report.Coverage.MeasuredMCPs++
			report.TotalEstimatedTokens += mcpReport.EstimatedTokens
			continue
		}
		report.Coverage.UnknownMCPs++
	}
	if instruction != nil {
		report.ManagedInstructionBytes = len(instruction)
		report.ManagedInstructionTokens = contextopt.EstimateTokens(string(instruction))
		report.TotalEstimatedTokens += report.ManagedInstructionTokens
	}
	return report
}

func measureCatalog(name string) MCPTaxReport {
	register, ok := localCatalogRegistration(name)
	if !ok {
		return unavailableMCPReport(name, "")
	}

	server := newSchemaServer(name, "0.0.0")
	register(server)
	return measureServer(name, server)
}

func localCatalogRegistration(name string) (func(*Server), bool) {
	switch name {
	case mcpregistry.ServerOptimizer:
		return RegisterOptimizerTools, true
	case mcpregistry.ServerObsidian:
		return RegisterObsidianTools, true
	default:
		return nil, false
	}
}

func unavailableMCPReport(name, reason string) MCPTaxReport {
	if reason == "" {
		reason = "no local tool catalog is registered for this MCP"
		if name == mcpregistry.ServerCodebase {
			reason = "tool catalog belongs to the configured external Codebase target"
		}
	}
	return MCPTaxReport{
		Name:              name,
		Availability:      MCPAvailabilityUnknown,
		UnavailableReason: reason,
		PerTool:           []ToolTax{},
	}
}

func measureServer(name string, server *Server) MCPTaxReport {
	serialized := server.ToolsListJSON()
	report := MCPTaxReport{
		Name:            name,
		Availability:    MCPAvailabilityMeasured,
		Tools:           server.ToolCount(),
		Serialized:      string(serialized),
		SerializedBytes: len(serialized),
		PerTool:         make([]ToolTax, 0, server.ToolCount()),
	}
	report.EstimatedTokens = contextopt.EstimateTokens(report.Serialized)
	for _, tool := range server.ToolPayloads() {
		b, err := json.Marshal(tool)
		if err != nil {
			return unavailableMCPReport(name, "tool schema could not be serialized")
		}
		report.PerTool = append(report.PerTool, ToolTax{
			Name:            tool.Name,
			Bytes:           len(b),
			EstimatedTokens: contextopt.EstimateTokens(string(b)),
		})
	}
	return report
}
