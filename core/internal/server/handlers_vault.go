package server

import (
	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/gin-gonic/gin"
)

// apiVaultMigrationReport returns what the migration and the ghost-vault sweep
// WOULD do right now, without touching the filesystem (dry run). The dashboard
// uses it to show pending vaults; the actual rename and sweep happen on
// POST /api/vault/migrate or at server startup.
func (ds *DashboardServer) apiVaultMigrationReport(c *gin.Context) {
	opts := ds.vaultMigrationOpts()
	opts.DryRun = true
	report, err := brain.MigrateVaultsToNamedLayout(ds.DwytHome, opts)
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "error": err.Error()})
		return
	}
	gc := brain.GCSweepVaults(ds.DwytHome, ds.vaultGCOptions(true))
	c.JSON(200, gin.H{"status": "ok", "report": report, "gc": gc})
}

// apiVaultMigrate forces an immediate migration pass plus the ghost-vault
// sweep. The dashboard exposes this as a "Rename legacy vaults" action so the
// user can confirm a manual rename after pointing DWYT at a project whose
// vault directory could not be resolved automatically.
func (ds *DashboardServer) apiVaultMigrate(c *gin.Context) {
	report, err := brain.MigrateVaultsToNamedLayout(ds.DwytHome, ds.vaultMigrationOpts())
	if err != nil {
		log.Warn("vault migration: manual run failed", log.Fields{"error": err.Error()})
		c.JSON(500, gin.H{"status": "error", "error": err.Error()})
		return
	}
	gc := brain.GCSweepVaults(ds.DwytHome, ds.vaultGCOptions(false))
	log.Info("vault migration: manual run",
		log.Fields{"migrated": report.Migrated, "unidentifiable": report.Unidentifiable,
			"gc_removed": gc.Removed, "gc_kept": gc.KeptWithContent})
	c.JSON(200, gin.H{"status": "ok", "report": report, "gc": gc})
}

// vaultGCOptions builds the sweep options: hashes DWYT knows (active or
// soft-removed projects) are never swept — their vault is the migration's
// business. A dry run only reports.
func (ds *DashboardServer) vaultGCOptions(dryRun bool) brain.VaultGCOptions {
	return brain.VaultGCOptions{
		DryRun: dryRun,
		KnownHash: func(hash string) bool {
			if ds.Store == nil {
				return false
			}
			_, err := ds.Store.GetProject(hash)
			return err == nil
		},
	}
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
			p, err := ds.Store.GetActiveProject(hash)
			if err != nil || p == nil {
				return "", "", false
			}
			return p.Path, p.Name, true
		},
		IgnoreHash: ds.ignoreRemovedVault,
	}
}

// ignoreRemovedVault keeps a soft-removed project's vault intact without
// surfacing it as pending migration work. Re-adding the project restores it
// to the active registry, where it becomes eligible again automatically.
func (ds *DashboardServer) ignoreRemovedVault(hash string) bool {
	if ds.Store == nil {
		return false
	}
	if _, err := ds.Store.GetProject(hash); err != nil {
		return false
	}
	_, err := ds.Store.GetActiveProject(hash)
	return err != nil
}
