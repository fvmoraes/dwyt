package server

import (
	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/mcp"
	"github.com/gin-gonic/gin"
)

// apiStartupTax exposes the MCP Startup Tax report (Fine-Tuning §8): the
// measured schema overhead of the first-party MCPs plus the managed
// instruction block, all labeled estimated. Diagnostics-only by design —
// the main dashboard stays simple.
func (ds *DashboardServer) apiStartupTax(c *gin.Context) {
	c.JSON(200, mcp.MeasureStartupTax([]byte(integrate.InstructionBlock())))
}

// NetSavingsReport reasons about savings honestly (Fine-Tuning §12.4):
// gross avoided context minus the measurable overheads that buy it. Every
// figure is estimated unless provenance says otherwise; an empty telemetry
// window yields unknown, never zero (§28.6).
type NetSavingsReport struct {
	Window                      string `json:"window"`
	GrossAvoidedTokens          *int   `json:"gross_avoided_tokens"`
	StartupSchemaTaxTokens      int    `json:"startup_schema_tax_tokens"`
	ManagedInstructionTaxTokens int    `json:"managed_instruction_tax_tokens"`
	NetEstimatedTokens          *int   `json:"net_estimated_tokens"`
	CoverageObservedRequests    int    `json:"coverage_observed_requests,omitempty"`
	CoverageRequests            int    `json:"coverage_requests,omitempty"`
	Provenance                  string `json:"provenance"`
}

// apiNetSavings derives net savings for a telemetry window: gross avoided
// (telemetry) minus the startup tax measured by MeasureStartupTax. Unknown
// stays unknown: with no telemetry the report says so instead of claiming 0.
func (ds *DashboardServer) apiNetSavings(c *gin.Context) {
	window := c.DefaultQuery("window", "7d")
	report := NetSavingsReport{Window: window, Provenance: "unknown"}

	tax := mcp.MeasureStartupTax([]byte(integrate.InstructionBlock()))
	report.StartupSchemaTaxTokens = tax.TotalEstimatedTokens
	report.ManagedInstructionTaxTokens = tax.ManagedInstructionTokens

	if ds.Telemetry == nil {
		c.JSON(200, report)
		return
	}
	since, _ := windowFor(window)
	projectID := ds.currentProjectID()
	sum, err := ds.Telemetry.Summarize(projectID, since, window)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if sum.Requests == 0 {
		// No data in the window: honest unknown, never a fabricated zero.
		c.JSON(200, report)
		return
	}

	gross := sum.AvoidedTokens
	net := gross - report.StartupSchemaTaxTokens - report.ManagedInstructionTaxTokens
	report.GrossAvoidedTokens = &gross
	report.NetEstimatedTokens = &net
	report.CoverageObservedRequests = sum.ObservedRequests
	report.CoverageRequests = sum.Requests
	report.Provenance = "estimated"
	c.JSON(200, report)
}
