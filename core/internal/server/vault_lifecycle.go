package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// vaultLeaseGuard keeps structural startup/manual migrations from racing HTTP
// and MCP operations against the same filesystem. /api/health and unrelated
// service endpoints remain available throughout Dashboard-first startup.
func vaultLeaseGuard(ds *DashboardServer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !vaultDependentPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		if vaultStructuralRequest(c.Request.Method, c.Request.URL.Path) {
			ds.vaultMigrating.Store(true)
			ds.vaultMigrationMu.Lock()
			ds.vaultMigrating.Store(true)
			defer func() {
				ds.vaultMigrating.Store(false)
				ds.vaultMigrationMu.Unlock()
			}()
			c.Next()
			return
		}

		// Fail fast while a writer is pending. The second check closes the race
		// where migration marks itself after this request's first load but
		// before it acquires the shared lease.
		if ds.vaultMigrating.Load() {
			vaultMigratingResponse(c)
			return
		}
		ds.vaultMigrationMu.RLock()
		if ds.vaultMigrating.Load() {
			ds.vaultMigrationMu.RUnlock()
			vaultMigratingResponse(c)
			return
		}
		defer ds.vaultMigrationMu.RUnlock()
		c.Next()
	}
}

func vaultMigratingResponse(c *gin.Context) {
	c.Header("Retry-After", "1")
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error": "project vault migration in progress", "state": "migrating", "retryable": true,
	})
}

func vaultDependentPath(path string) bool {
	for _, prefix := range []string{
		"/api/obsidian", "/api/memory", "/api/vault",
		"/api/project", "/api/context",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return path == "/api/setup/save" || path == "/api/tool-details"
}

func vaultStructuralRequest(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	switch path {
	case "/api/setup/save", "/api/project/switch", "/api/project/remove",
		"/api/vault/migrate", "/api/memory/migrate-v5":
		return true
	default:
		return false
	}
}

func (ds *DashboardServer) withVaultMigration(ctx context.Context, run func(context.Context) error) error {
	ds.vaultMigrating.Store(true)
	ds.vaultMigrationMu.Lock()
	ds.vaultMigrating.Store(true)
	defer func() {
		ds.vaultMigrating.Store(false)
		ds.vaultMigrationMu.Unlock()
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	return run(ctx)
}
