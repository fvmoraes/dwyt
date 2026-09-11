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
