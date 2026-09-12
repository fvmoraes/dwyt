package server

import (
	"context"
	"fmt"
	"strconv"

	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) startHeadroom() (*procman.ServiceStatus, error) {
	return ds.startHeadroomContext(context.Background())
}

func (ds *DashboardServer) startHeadroomContext(ctx context.Context) (*procman.ServiceStatus, error) {
	status, err := ds.startManagedService(ctx, "headroom")
	if status != nil {
		ds.setHeadroomPort(status.Port)
	}
	return status, err
}

func (ds *DashboardServer) apiHeadroomStartPM(c *gin.Context) {
	status, err := ds.startHeadroomContext(c.Request.Context())
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

	ds.configureHeadroomClients(ds.DefaultProject)
	c.JSON(200, gin.H{"status": "started", "port": status.Port})
}

func (ds *DashboardServer) apiHeadroomStopPM(c *gin.Context) {
	if _, err := ds.stopManagedService(c.Request.Context(), "headroom"); err != nil {
		if ds.RuntimeState != nil {
			ds.RuntimeState.SetToolError("headroom", err.Error())
		}
		c.JSON(500, gin.H{"status": "error", "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "stopped"})
}

func (ds *DashboardServer) apiHeadroomStatusPM(c *gin.Context) {
	port := ds.headroomPort()
	// Owner-respecting status: derive online/healthy from ProcessManager plus
	// the reconciler's identity-validated projection, not from a bare HTTP 200.
	st := ds.observedServiceStatus("headroom", port)
	if !st.Healthy && isPortOpen(port) {
		st.Status = "port_open_no_health"
		st.State = "port_open_no_health"
		st.Running = false
		st.Healthy = false
		if st.Port == 0 {
			st.Port = port
		}
		if st.Error == "" {
			st.Error = "port open but healthcheck failed"
		}
	}
	c.JSON(200, st)
}

func (ds *DashboardServer) apiHeadroomLogsPM(c *gin.Context) {
	tail := 50
	if t := c.Query("tail"); t != "" {
		if parsed, err := strconv.Atoi(t); err == nil {
			tail = parsed
		}
	}
	logs := ds.ProcMan.Logs("headroom", tail)
	c.Data(200, "text/plain; charset=utf-8", []byte(logs))
}

func (ds *DashboardServer) apiHeadroomStatsURL(c *gin.Context) {
	port := ds.headroomPort()
	// Owner-respecting readiness: only skip starting when the reconciler already
	// considers Headroom healthy (real observation / identity-validated
	// adoption), not because some process answered HTTP 200 on the port.
	if observed := ds.observedServiceStatus("headroom", port); observed.Healthy {
		effectivePort := port
		if observed.Port > 0 {
			effectivePort = observed.Port
		}
		statsURL := fmt.Sprintf("http://127.0.0.1:%d/stats", effectivePort)
		c.JSON(200, gin.H{"url": statsURL, "started": false})
		return
	}

	status, err := ds.startHeadroomContext(c.Request.Context())
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

	statsURL := fmt.Sprintf("http://127.0.0.1:%d/stats", status.Port)
	c.JSON(200, gin.H{"url": statsURL, "started": true})
}
