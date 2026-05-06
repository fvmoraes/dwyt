package server

import (
	"github.com/fvmoraes/dwyt/internal/kiropow"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiKiroPowerStatus(c *gin.Context) {
	c.JSON(200, kiropow.Status(ds.DwytHome, ds.DwytBin))
}

func (ds *DashboardServer) apiKiroPowerRefresh(c *gin.Context) {
	st, err := kiropow.EnsurePower(ds.DwytHome, ds.DwytBin, ds.DefaultProject)
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "error": err.Error(), "power": st})
		return
	}
	c.JSON(200, st)
}
