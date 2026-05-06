package server

import (
	"fmt"
	"os"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiHeadroomStartPM(c *gin.Context) {
	status, err := ds.ProcMan.Start("headroom")
	if err != nil || !status.Healthy {
		c.JSON(500, gin.H{"error": status.Error})
		return
	}

	ds.RuntimeState.RegisterProcess("headroom", status.PID, status.Port)

	ds.runHeadroomWrap(ds.DefaultProject)

	c.JSON(200, gin.H{"status": "started", "port": status.Port})
}

func (ds *DashboardServer) apiHeadroomStopPM(c *gin.Context) {
	ds.runHeadroomUnwrap(ds.DefaultProject)

	ds.ProcMan.Stop("headroom")
	ds.RuntimeState.RemoveProcess("headroom")

	c.JSON(200, gin.H{"status": "stopped"})
}

func (ds *DashboardServer) apiHeadroomStatusPM(c *gin.Context) {
	status := ds.ProcMan.Status("headroom")
	c.JSON(200, status)
}

func (ds *DashboardServer) apiHeadroomLogsPM(c *gin.Context) {
	tail := 50
	if t := c.Query("tail"); t != "" {
		fmt.Sscanf(t, "%d", &tail)
	}
	logs := ds.ProcMan.Logs("headroom", tail)
	c.Data(200, "text/plain; charset=utf-8", []byte(logs))
}

func (ds *DashboardServer) apiHeadroomStatsURL(c *gin.Context) {
	proxyPort := fmt.Sprintf("%d", ds.HeadroomPort)
	healthURL := fmt.Sprintf("http://127.0.0.1:%s/health", proxyPort)
	statsURL  := fmt.Sprintf("http://127.0.0.1:%s/stats", proxyPort)

	bin := fmt.Sprintf("%s/headroom", ds.DwytBin)
	if _, err := os.Stat(bin); err != nil {
		c.JSON(404, gin.H{"error": "headroom not installed", "url": ""})
		return
	}

	if health.ProbeURL(healthURL) {
		c.JSON(200, gin.H{"url": statsURL, "started": false})
		return
	}

	check, err := health.StartService("headroom", bin, healthURL, "proxy", "--port", proxyPort)
	if err != nil || !check.Healthy {
		log.Error("failed to start headroom proxy", log.Fields{"error": check.Error})
		c.JSON(200, gin.H{"url": statsURL, "started": true, "note": "may still be starting"})
		return
	}

	c.JSON(200, gin.H{"url": statsURL, "started": true})
}
