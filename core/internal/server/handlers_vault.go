package server

import (
	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/gin-gonic/gin"
)

// apiVaultMigrationReport returns what the migration WOULD do right now,
// without touching the filesystem (dry run). The dashboard uses it to show
// pending vaults; the actual rename happens on POST /api/vault/migrate or
// at server startup.
func (ds *DashboardServer) apiVaultMigrationReport(c *gin.Context) {
	opts := ds.vaultMigrationOpts()
	opts.DryRun = true
	report, err := brain.MigrateVaultsToNamedLayout(ds.DwytHome, opts)
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "report": report})
}

// apiVaultMigrate forces an immediate migration pass. The dashboard exposes
// this as a "Rename legacy vaults" action so the user can confirm a manual
// rename after pointing DWYT at a project whose vault directory could not be
// resolved automatically.
func (ds *DashboardServer) apiVaultMigrate(c *gin.Context) {
	report, err := brain.MigrateVaultsToNamedLayout(ds.DwytHome, ds.vaultMigrationOpts())
	if err != nil {
		log.Warn("vault migration: manual run failed", log.Fields{"error": err.Error()})
		c.JSON(500, gin.H{"status": "error", "error": err.Error()})
		return
	}
	log.Info("vault migration: manual run",
		log.Fields{"migrated": report.Migrated, "unidentifiable": report.Unidentifiable})
	c.JSON(200, gin.H{"status": "ok", "report": report})
}

// vaultMigrationOpts returns the standard MigrationOptions the server uses
// to resolve project names: DB first, then runtime state. Active project is
// not used here because the migration pass runs against every known vault,
// not just the currently active one — the active project hint only helps
// for the single-project case handled inside NewProjectObsidian.
func (ds *DashboardServer) vaultMigrationOpts() brain.MigrationOptions {
	return brain.MigrationOptions{
		ProjectPathResolver: func(hash string) (string, string, bool) {
			if ds.Store == nil {
				return "", "", false
			}
			p, err := ds.Store.GetProject(hash)
			if err != nil || p == nil {
				return "", "", false
			}
			return p.Path, p.Name, true
		},
	}
}
