package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/state"
)

// switchBody builds the request body through encoding/json so Windows paths
// (C:\Users\...) arrive as valid JSON escapes instead of bare backslashes.
func switchBody(t *testing.T, path string) *bytes.Reader {
	t.Helper()
	data, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(data)
}

// A stale or mistyped project path must be refused: switching to a directory
// that is not there would phantom-project the daemon (vault, Kiro power,
// indexing) until the next restart.
func TestProjectSwitchRefusesNonexistentPath(t *testing.T) {
	ds := brainServer(t)
	ghost := filepath.Join(t.TempDir(), "does-not-exist")

	req := httptest.NewRequest(http.MethodPost, "/api/project/switch", switchBody(t, ghost))
	req.Header.Set("Content-Type", "application/json")
	rec, payload := do(t, ds, ds.apiProjectSwitch, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing project path, got %d: %s", rec.Code, rec.Body.String())
	}
	if msg, _ := payload["error"].(string); !strings.Contains(msg, "does not exist") {
		// The refusal must come from the existence check, not from a JSON
		// binding error — before the body was properly escaped the two were
		// indistinguishable, which is exactly how this test passed on Linux
		// and failed on Windows.
		t.Fatalf("expected the existence check to refuse, got: %s", rec.Body.String())
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
	req := httptest.NewRequest(http.MethodPost, "/api/project/switch", switchBody(t, dir))
	req.Header.Set("Content-Type", "application/json")
	rec, _ := do(t, ds, ds.apiProjectSwitch, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a real directory, got %d: %s", rec.Code, rec.Body.String())
	}
	if ds.DefaultProject != dir {
		t.Fatalf("expected the project to switch to %q, got %q", dir, ds.DefaultProject)
	}
}
