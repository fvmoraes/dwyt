package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fvmoraes/dwyt/internal/state"
)

// A stale or mistyped project path must be refused: switching to a directory
// that is not there would phantom-project the daemon (vault, Kiro power,
// indexing) until the next restart.
func TestProjectSwitchRefusesNonexistentPath(t *testing.T) {
	ds := brainServer(t)
	ghost := filepath.Join(t.TempDir(), "does-not-exist")

	body := `{"path":"` + ghost + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/project/switch", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec, _ := do(t, ds, ds.apiProjectSwitch, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing project path, got %d: %s", rec.Code, rec.Body.String())
	}
	if ds.DefaultProject == ghost {
		t.Fatal("the daemon must not adopt a nonexistent project")
	}
}

func TestProjectSwitchAcceptsRealDirectory(t *testing.T) {
	ds := brainServer(t)
	dwytHome := ds.DwytHome
	ds.RuntimeState = state.Init(dwytHome)

	dir := t.TempDir()
	body := `{"path":"` + dir + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/project/switch", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec, _ := do(t, ds, ds.apiProjectSwitch, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a real directory, got %d: %s", rec.Code, rec.Body.String())
	}
	if ds.DefaultProject != dir {
		t.Fatalf("expected the project to switch to %q, got %q", dir, ds.DefaultProject)
	}
}
