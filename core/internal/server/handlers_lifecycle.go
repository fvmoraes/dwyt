package server

import (
	"context"
	"time"

	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/gin-gonic/gin"
)

// apiShutdown provides the cross-platform cooperative stop path. The response
// is flushed before shutdown runs because closing the listener from inside the
// request would otherwise race delivery of the acknowledgement.
func (ds *DashboardServer) apiShutdown(c *gin.Context) {
	c.JSON(202, gin.H{"status": "shutting_down"})
	c.Writer.Flush()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := ds.Shutdown(ctx); err != nil {
			log.Warn("HTTP-triggered graceful shutdown incomplete", log.Fields{"error": err.Error()})
		}
	}()
}
