package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/gin-gonic/gin"
)

// TestAPIMCPConfigureReturnsStructuredPayload exercises the /api/mcp/configure
// happy path and asserts the response carries the canonical command/args and
// the migrated flag.
func TestAPIMCPConfigureReturnsStructuredPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	dwytBin := filepath.Join(dwytHome, "bin")
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)

	touchExecutableForMCP(t, filepath.Join(dwytBin, "dwyt"))
	touchExecutableForMCP(t, filepath.Join(dwytBin, "codebase-memory-mcp"))

	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Close before t.TempDir cleanup: on Windows an open SQLite handle
	// (WAL files included) makes the directory undeletable and fails the
	// test with "file is being used by another process".
	defer store.Close()
	ds := &DashboardServer{
		DwytBin:        dwytBin,
		DwytHome:       dwytHome,
		DefaultProject: t.TempDir(),
		Store:          store,
		ProcMan:        procman.New(dwytHome),
	}
	if err := store.SetConfig("setup", `{"ias":["claude"],"tools":["obsidian","cbmcp"]}`); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/api/mcp/configure", bytes.NewReader([]byte(`{"name":"obsidian","project_path":"/tmp/proj"}`)))
	c.Request.Header.Set("Content-Type", "application/json")

	ds.apiMCPConfigure(c)

	if rec.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "configured" {
		t.Fatalf("expected status=configured, got %#v", payload["status"])
	}
	if payload["name"] != "obsidian" {
		t.Fatalf("expected name=obsidian, got %#v", payload["name"])
	}
	cmd, _ := payload["command"].(string)
	if filepath.Base(cmd) != "dwyt" && filepath.Base(cmd) != "dwyt.exe" {
		t.Fatalf("expected command to point at main dwyt, got %q", cmd)
	}
	args, _ := payload["args"].([]any)
	if len(args) != 1 || args[0] != "obsidian-mcp" {
		t.Fatalf("expected args=[obsidian-mcp], got %#v", args)
	}
	if _, ok := payload["clients"]; !ok {
		t.Fatal("expected clients list in response")
	}
	if _, ok := payload["migrated"]; !ok {
		t.Fatal("expected migrated flag in response")
	}
}

// TestAPIMCPConfigureInvalidBody asserts the handler returns a structured
// 400 error (not a silent 500 or empty body) when the JSON body is malformed.
func TestAPIMCPConfigureInvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)

	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Close before t.TempDir cleanup: on Windows an open SQLite handle
	// (WAL files included) makes the directory undeletable and fails the
	// test with "file is being used by another process".
	defer store.Close()
	ds := &DashboardServer{
		DwytBin:        filepath.Join(dwytHome, "bin"),
		DwytHome:       dwytHome,
		DefaultProject: t.TempDir(),
		Store:          store,
		ProcMan:        procman.New(dwytHome),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/api/mcp/configure", bytes.NewReader([]byte(`{not json`)))
	c.Request.Header.Set("Content-Type", "application/json")

	ds.apiMCPConfigure(c)

	if rec.Code != 400 {
		t.Fatalf("expected HTTP 400 for invalid body, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["stage"] != "validation" {
		t.Fatalf("expected stage=validation, got %#v", payload["stage"])
	}
	if payload["error"] == "" || payload["error"] == nil {
		t.Fatal("expected error message in payload")
	}
}

// TestAPIMCPConfigureMissingDwytBinary asserts the handler surfaces a clear
// "obsidian MCP not installed" error when the main dwyt binary cannot be
// provisioned — the bin path is a file, so neither the canonical binary nor
// the self-heal copy can exist there. (In normal installs a missing binary
// self-heals from the running executable.)
func TestAPIMCPConfigureMissingDwytBinary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	dwytBin := filepath.Join(dwytHome, "bin")
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)
	// The bin path is a FILE: ObsidianMCP cannot create the canonical
	// binary there, forcing the validation failure this test exercises.
	if err := os.MkdirAll(dwytHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dwytBin, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Close before t.TempDir cleanup: on Windows an open SQLite handle
	// (WAL files included) makes the directory undeletable and fails the
	// test with "file is being used by another process".
	defer store.Close()
	ds := &DashboardServer{
		DwytBin:        dwytBin,
		DwytHome:       dwytHome,
		DefaultProject: t.TempDir(),
		Store:          store,
		ProcMan:        procman.New(dwytHome),
	}
	if err := store.SetConfig("setup", `{"ias":["claude"],"tools":["obsidian"]}`); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/api/mcp/configure", bytes.NewReader([]byte(`{"name":"obsidian"}`)))
	c.Request.Header.Set("Content-Type", "application/json")

	ds.apiMCPConfigure(c)

	if rec.Code != 500 {
		t.Fatalf("expected HTTP 500 when dwyt binary cannot be provisioned, got %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	errMsg, _ := payload["error"].(string)
	if !strings.Contains(errMsg, "obsidian MCP not installed") {
		t.Fatalf("expected structured error mentioning obsidian MCP, got %q", errMsg)
	}
}

// TestAPIMCPConfigureIdempotent calls the endpoint multiple times to ensure
// repeated reconfigures do not fail or duplicate files.
func TestAPIMCPConfigureIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	dwytBin := filepath.Join(dwytHome, "bin")
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)

	touchExecutableForMCP(t, filepath.Join(dwytBin, "dwyt"))

	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Close before t.TempDir cleanup: on Windows an open SQLite handle
	// (WAL files included) makes the directory undeletable and fails the
	// test with "file is being used by another process".
	defer store.Close()
	projectPath := t.TempDir()
	ds := &DashboardServer{
		DwytBin:        dwytBin,
		DwytHome:       dwytHome,
		DefaultProject: projectPath,
		Store:          store,
		ProcMan:        procman.New(dwytHome),
	}
	if err := store.SetConfig("setup", `{"ias":["claude"],"tools":["obsidian"]}`); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/api/mcp/configure", bytes.NewReader([]byte(`{"name":"obsidian"}`)))
		c.Request.Header.Set("Content-Type", "application/json")
		ds.apiMCPConfigure(c)
		if rec.Code != 200 {
			t.Fatalf("call %d: expected 200, got %d: %s", i, rec.Code, rec.Body.String())
		}
	}

	reg, err := mcpregistry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reg.IsBinaryInstalled("obsidian") {
		t.Fatal("obsidian must remain installed across reconfigures")
	}
}

func touchExecutableForMCP(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
}

// keep the import of runtime referenced; the canonical command is platform
// dependent but the test bodies are platform-agnostic.
var _ = runtime.GOOS
