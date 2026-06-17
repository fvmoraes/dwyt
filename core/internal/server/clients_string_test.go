package server

import (
	"path/filepath"
	"testing"

	"github.com/fvmoraes/dwyt/internal/db"
)

// clientsString must reflect exactly the user's saved selection and never fall
// back to "all clients" — that fallback was a source of over-provisioning.
func TestClientsStringReflectsSavedSelection(t *testing.T) {
	store, err := db.New(filepath.Join(t.TempDir(), "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ds := &DashboardServer{Store: store}

	// No setup saved yet → empty selection, not all clients.
	if got := ds.clientsString(); got != "" {
		t.Fatalf("expected empty clients before setup, got %q", got)
	}

	// Saved setup with a single client → only that client.
	if err := store.SetConfig("setup", `{"ias":["kiro"]}`); err != nil {
		t.Fatal(err)
	}
	if got := ds.clientsString(); got != "kiro" {
		t.Fatalf("expected only the selected client, got %q", got)
	}
}

// A nil Store must also yield an empty selection rather than all clients.
func TestClientsStringNilStoreReturnsEmpty(t *testing.T) {
	ds := &DashboardServer{}
	if got := ds.clientsString(); got != "" {
		t.Fatalf("expected empty clients with nil store, got %q", got)
	}
}
