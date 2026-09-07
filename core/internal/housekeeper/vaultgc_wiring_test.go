package housekeeper

import (
	"testing"

	"github.com/fvmoraes/dwyt/internal/brain"
)

// The ghost-vault sweep is project-wide (it walks ~/.dwyt/projects), so the
// housekeeper receives it as an injected callback. A deep pass must run it and
// fold its counts into the report; a light pass must not.
func TestDeepPassRunsVaultGC(t *testing.T) {
	pb := testVault(t)
	cfg := DefaultConfig()

	gcCalls := 0
	gcDryRuns := 0
	cfg.VaultGC = func(dryRun bool) brain.VaultGCReport {
		gcCalls++
		if dryRun {
			gcDryRuns++
		}
		return brain.VaultGCReport{Scanned: 2, Removed: 1, KeptWithContent: 1}
	}
	h := New(cfg, pb, nil)

	report := h.Run(Deep)
	if gcCalls != 1 || gcDryRuns != 0 {
		t.Fatalf("deep pass must run the sweep exactly once, non-dry, got calls=%d dry=%d", gcCalls, gcDryRuns)
	}
	if report.VaultGhostsRemoved != 1 || report.VaultGhostsKept != 1 {
		t.Fatalf("gc counts missing from the report: %+v", report)
	}

	// A light pass never sweeps.
	light := New(cfg, pb, nil).Run(Light)
	if light.VaultGhostsRemoved != 0 || light.VaultGhostsKept != 0 {
		t.Fatalf("light pass must not sweep vaults: %+v", light)
	}
}

func TestDeepPassVaultGCCDryRunPropagates(t *testing.T) {
	pb := testVault(t)
	cfg := DefaultConfig()
	cfg.VaultGC = func(dryRun bool) brain.VaultGCReport {
		if !dryRun {
			t.Fatal("RunDry must propagate the dry-run flag to the sweep")
		}
		return brain.VaultGCReport{Removed: 3}
	}
	h := New(cfg, pb, nil)
	report := h.RunDry(Deep)
	if report.VaultGhostsRemoved != 3 {
		t.Fatalf("expected the dry-run sweep counts in the report: %+v", report)
	}
}
