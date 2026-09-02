package server

import (
	"fmt"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/gin-gonic/gin"
)

// startHeadroom is the sole process-manager entry point for the proxy. It
// publishes a fallback port selected by ProcessManager before callers build
// URLs or configure client wrappers.
func (ds *DashboardServer) startHeadroom() (*procman.ServiceStatus, error) {
	status, err := ds.ProcMan.Start("headroom")
	if status != nil {
		ds.setHeadroomPort(status.Port)
	}
	return status, err
}

func (ds *DashboardServer) apiHeadroomStartPM(c *gin.Context) {
	status, err := ds.startHeadroom()
	if err != nil || status == nil || !status.Healthy {
		errMsg := "headroom failed to start"
		if status != nil && status.Error != "" {
			errMsg = status.Error
		} else if err != nil {
			errMsg = err.Error()
		}
		c.JSON(500, gin.H{"status": "error", "error": errMsg})
		return
	}

	ds.RuntimeState.RegisterProcess("headroom", status.PID, status.Port)

	ds.configureHeadroomClients(ds.DefaultProject)

	c.JSON(200, gin.H{"status": "started", "port": status.Port})
}

func (ds *DashboardServer) apiHeadroomStopPM(c *gin.Context) {
	ds.ProcMan.Stop("headroom")
	ds.RuntimeState.RemoveProcess("headroom")

	c.JSON(200, gin.H{"status": "stopped"})
}

func (ds *DashboardServer) apiHeadroomStatusPM(c *gin.Context) {
	st := ds.ProcMan.Status("headroom")
	port := ds.headroomPort()
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	if health.ProbeURL(healthURL) {
		st.Status = "online"
		st.State = "online"
		st.Running = true
		st.Healthy = true
		st.Port = port
		st.Error = ""
	} else if isPortOpen(port) {
		st.Status = "port_open_no_health"
		st.State = "port_open_no_health"
		st.Running = false
		st.Healthy = false
		st.Port = port
		st.Error = "port open but healthcheck failed"
	}
	c.JSON(200, st)
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
	port := ds.headroomPort()
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	statsURL := fmt.Sprintf("http://127.0.0.1:%d/stats", port)
	if health.ProbeURL(healthURL) {
		c.JSON(200, gin.H{"url": statsURL, "started": false})
		return
	}

	status, err := ds.startHeadroom()
	if err != nil || status == nil || !status.Healthy {
		errMsg := "headroom failed to start"
		if status != nil && status.Error != "" {
			errMsg = status.Error
		} else if err != nil {
			errMsg = err.Error()
		}
		log.Error("failed to start headroom proxy", log.Fields{"error": errMsg})
		c.JSON(500, gin.H{"status": "error", "error": errMsg, "url": ""})
		return
	}

	statsURL = fmt.Sprintf("http://127.0.0.1:%d/stats", status.Port)
	c.JSON(200, gin.H{"url": statsURL, "started": true})
}
