package server

import (
	"fmt"
	"strings"

	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/mcp"
	"github.com/fvmoraes/dwyt/internal/telemetry"
	"github.com/gin-gonic/gin"
)

// apiStartupTax exposes the MCP Startup Tax report (Fine-Tuning §8): the
// measured schema overhead of the first-party MCPs plus the managed
// instruction block, all labeled estimated. Diagnostics-only by design —
// the main dashboard stays simple.
func (ds *DashboardServer) apiStartupTax(c *gin.Context) {
	c.JSON(200, mcp.MeasureStartupTax([]byte(integrate.InstructionBlock())))
}

// NetSavingsReport reasons about savings honestly (Fine-Tuning §12.4): gross
// avoided context minus every measured overhead that buys it. The startup tax
// coverage remains visible because a measured subtotal is not a total when an
// external MCP catalog is unavailable.
type NetSavingsReport struct {
	Window                      string                     `json:"window"`
	GrossAvoidedTokens          *int                       `json:"gross_avoided_tokens"`
	StartupSchemaTaxTokens      int                        `json:"startup_schema_tax_tokens"`
	ManagedInstructionTaxTokens int                        `json:"managed_instruction_tax_tokens"`
	CompressionMetadataTokens   *int                       `json:"compression_metadata_tokens"`
	NetEstimatedTokens          *int                       `json:"net_estimated_tokens"`
	CoverageObservedRequests    int                        `json:"coverage_observed_requests"`
	CoverageRequests            int                        `json:"coverage_requests"`
	CoverageContextRequests     int                        `json:"coverage_context_requests"`
	CoverageCompressionRequests int                        `json:"coverage_compression_metadata_requests"`
	StartupTaxCoverage          mcp.StartupTaxCoverage     `json:"startup_tax_coverage"`
	MetricProvenance            telemetry.MetricProvenance `json:"metric_provenance"`
	Provenance                  telemetry.Provenance       `json:"provenance"`
	Reason                      string                     `json:"reason,omitempty"`
}

func newNetSavingsReport(window string, tax mcp.StartupTaxReport) NetSavingsReport {
	schemaTax := tax.TotalEstimatedTokens - tax.ManagedInstructionTokens
	if schemaTax < 0 {
		schemaTax = 0
	}
	return NetSavingsReport{
		Window:                      window,
		StartupSchemaTaxTokens:      schemaTax,
		ManagedInstructionTaxTokens: tax.ManagedInstructionTokens,
		StartupTaxCoverage:          tax.Coverage,
		MetricProvenance: telemetry.MetricProvenance{
			telemetry.MetricAvoidedTokens:             telemetry.ProvenanceUnsupported,
			"startup_schema_tax_tokens":               telemetry.ProvenanceEstimated,
			"managed_instruction_tax_tokens":          telemetry.ProvenanceEstimated,
			telemetry.MetricCompressionMetadataTokens: telemetry.ProvenanceUnsupported,
			"net_estimated_tokens":                    telemetry.ProvenanceUnsupported,
		},
		Provenance: telemetry.ProvenanceUnsupported,
	}
}

// deriveNetSavings is deliberately strict: an incomplete request window, a
// counterfactual input, or an incomplete startup-tax catalog makes the net
// figure unsupported rather than allowing a partial or synthetic total to
// masquerade as an estimate.
func deriveNetSavings(window string, sum telemetry.Summary, tax mcp.StartupTaxReport) NetSavingsReport {
	report := newNetSavingsReport(window, tax)
	report.CoverageObservedRequests = sum.ObservedRequests
	report.CoverageRequests = sum.Requests
	report.CoverageContextRequests = sum.Coverage.ContextReported
	report.CoverageCompressionRequests = sum.Coverage.CompressionMetadataReported

	if sum.Requests == 0 {
		report.Reason = "no request telemetry in this window"
		return report
	}

	var reasons []string
	contextProvenance := sum.Provenance.For(telemetry.MetricAvoidedTokens)
	contextKnown := sum.Coverage.ContextReported == sum.Requests &&
		contextProvenance != telemetry.ProvenanceUnsupported
	contextEligible := netSavingsInputEligible(contextProvenance)
	if contextKnown {
		gross := sum.AvoidedTokens
		report.GrossAvoidedTokens = &gross
		report.MetricProvenance[telemetry.MetricAvoidedTokens] = contextProvenance
	} else {
		reasons = append(reasons, fmt.Sprintf("context before/after reported for %d of %d requests", sum.Coverage.ContextReported, sum.Requests))
	}
	if contextKnown && !contextEligible {
		reasons = append(reasons, "avoided context is benchmark_counterfactual")
	}

	metadataProvenance := sum.Provenance.For(telemetry.MetricCompressionMetadataTokens)
	metadataKnown := sum.Coverage.CompressionMetadataReported == sum.Requests &&
		metadataProvenance != telemetry.ProvenanceUnsupported
	metadataEligible := netSavingsInputEligible(metadataProvenance)
	if metadataKnown {
		metadata := sum.CompressionMetadataTokens
		report.CompressionMetadataTokens = &metadata
		report.MetricProvenance[telemetry.MetricCompressionMetadataTokens] = metadataProvenance
	} else {
		reasons = append(reasons, fmt.Sprintf("compression metadata reported for %d of %d requests", sum.Coverage.CompressionMetadataReported, sum.Requests))
	}
	if metadataKnown && !metadataEligible {
		reasons = append(reasons, "compression metadata is benchmark_counterfactual")
	}

	if tax.Coverage.UnknownMCPs > 0 {
		reasons = append(reasons, fmt.Sprintf("startup schema tax excludes %d of %d MCP catalogs", tax.Coverage.UnknownMCPs, tax.Coverage.TotalMCPs))
	}
	if !contextKnown || !metadataKnown || !contextEligible || !metadataEligible || tax.Coverage.UnknownMCPs > 0 {
		report.Reason = strings.Join(reasons, "; ")
		return report
	}

	net := *report.GrossAvoidedTokens - report.StartupSchemaTaxTokens - report.ManagedInstructionTaxTokens - *report.CompressionMetadataTokens
	report.NetEstimatedTokens = &net
	// Startup tax is estimated even if context values were provider-observed, so
	// the combined net value remains an estimate.
	report.MetricProvenance["net_estimated_tokens"] = telemetry.ProvenanceEstimated
	report.Provenance = telemetry.ProvenanceEstimated
	return report
}

func netSavingsInputEligible(provenance telemetry.Provenance) bool {
	return provenance == telemetry.ProvenanceObserved || provenance == telemetry.ProvenanceEstimated
}

// apiNetSavings derives net savings for a telemetry window. Unknown stays
// unsupported: no telemetry, missing compression metadata, counterfactual
// inputs, or a partial startup-tax catalog never becomes a fabricated zero.
func (ds *DashboardServer) apiNetSavings(c *gin.Context) {
	projectID, err := ds.telemetryProjectID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error(), "path": c.Query("path")})
		return
	}
	requestedWindow := c.DefaultQuery("window", "7d")
	since, window := windowFor(requestedWindow)
	tax := mcp.MeasureStartupTax([]byte(integrate.InstructionBlock()))
	if ds.Telemetry == nil {
		report := newNetSavingsReport(window, tax)
		report.Reason = "telemetry not initialized"
		c.JSON(200, report)
		return
	}

	sum, err := ds.Telemetry.Summarize(projectID, since, window)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, deriveNetSavings(window, sum, tax))
}
