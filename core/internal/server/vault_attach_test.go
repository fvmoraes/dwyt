package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/db"
)

// The registration gate is what stops daemon start directories from growing
// ghost vaults: unregistered project → no vault, with a reason that tells the
// user what to do.
func TestVaultAttachErrorRequiresRegistration(t *testing.T) {
	dwytHome := t.TempDir()
	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	registered := t.TempDir()
	if err := store.TouchProject(registered); err != nil {
		t.Fatal(err)
	}
	softRemoved := t.TempDir()
	if err := store.TouchProject(softRemoved); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveProject(softRemoved); err != nil {
		t.Fatal(err)
	}

	if err := vaultAttachError(store, ""); err == nil {
		t.Fatal("empty project must not attach")
	}
	if err := vaultAttachError(nil, registered); err != nil {
		t.Fatalf("without a registry the legacy permissive behavior holds: %v", err)
	}
	if err := vaultAttachError(store, registered); err != nil {
		t.Fatalf("a registered project attaches: %v", err)
	}
	err = vaultAttachError(store, t.TempDir())
	if err == nil {
		t.Fatal("an unregistered project must not attach")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("the reason must guide the user: %v", err)
	}
	err = vaultAttachError(store, softRemoved)
	if err == nil {
		t.Fatal("a soft-removed project must not attach until re-added")
	}
}
