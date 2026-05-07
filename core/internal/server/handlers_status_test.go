package server

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/gin-gonic/gin"
)

func TestEstimateCodebaseTokenSavings(t *testing.T) {
	saved, used := estimateCodebaseTokenSavings(1000, 3000)
	if saved <= 0 {
		t.Fatalf("expected codebase savings, got saved=%d used=%d", saved, used)
	}
	if used <= 0 {
		t.Fatalf("expected MCP token cost, got %d", used)
	}
}

func TestEstimateObsidianTokenSavingsIgnoresTinyVault(t *testing.T) {
	saved, used := estimateObsidianTokenSavings(3, 400)
	if saved != 0 || used != 0 {
		t.Fatalf("tiny vault should not report savings, got saved=%d used=%d", saved, used)
	}
}

func TestEstimateObsidianTokenSavings(t *testing.T) {
	saved, used := estimateObsidianTokenSavings(6, 12000)
	if saved <= 0 {
		t.Fatalf("expected obsidian savings, got saved=%d used=%d", saved, used)
	}
	if used <= 0 {
		t.Fatalf("expected Obsidian MCP token cost, got %d", used)
	}
}

func TestCalculateGlobalTokenSavingsUsesLocalEstimates(t *testing.T) {
	details := map[string]*ToolDetail{
		"codebase-memory-mcp": {
			TokensSaved:       1000,
			TokensUsed:        200,
			WithoutDWYTTokens: 1200,
			WithDWYTTokens:    200,
			EstimationSource:  "local_estimate:codebase_graph_metadata",
		},
		"obsidian": {
			TokensSaved:       500,
			TokensUsed:        100,
			WithoutDWYTTokens: 600,
			WithDWYTTokens:    100,
			EstimationSource:  "local_estimate:obsidian_markdown_bytes",
		},
	}
	global := calculateGlobalTokenSavings(details)
	if global.TokensSaved != 1500 || global.WithoutDWYTTokens != 1800 || global.WithDWYTTokens != 300 {
		t.Fatalf("unexpected global savings: %#v", global)
	}
	if global.EstimationSource == "" {
		t.Fatal("expected estimation source for local estimates")
	}
}

func TestDetailCBMCPUsesCodebaseSQLiteGraphWhenStoreHasNoCounts(t *testing.T) {
	dwytHome := t.TempDir()
	projectPath := t.TempDir()
	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.TouchProject(projectPath); err != nil {
		t.Fatal(err)
	}

	writeCodebaseGraphDB(t, dwytHome, projectPath, 100, 300)
	dwytBin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dwytBin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dwytBin, "codebase-memory-mcp"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	ds := &DashboardServer{
		DwytHome:       dwytHome,
		DwytBin:        dwytBin,
		DefaultProject: projectPath,
		Store:          store,
		ProcMan:        procman.New(dwytHome),
	}
	detail := ds.detailCBMCP(projectPath)
	if detail.TokensSaved <= 0 {
		t.Fatalf("expected codebase token savings from graph DB, got %#v", detail)
	}
	if detail.IndexedNodes != 100 || detail.IndexedEdges != 300 {
		t.Fatalf("expected graph counts to be surfaced, got nodes=%d edges=%d", detail.IndexedNodes, detail.IndexedEdges)
	}
	pj, err := store.GetProjectByPath(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if pj.IndexedAt == nil || pj.Nodes != 100 || pj.Edges != 300 {
		t.Fatalf("expected store to be healed from graph DB, got %#v", pj)
	}
}

func TestAPIHealthIncludesDaemonVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ds := &DashboardServer{ReleaseVersion: "v4.8.0"}

	ds.apiHealth(ctx)

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["version"] != "v4.8.0" {
		t.Fatalf("expected health version v4.8.0, got %#v", payload["version"])
	}
}

func writeCodebaseGraphDB(t *testing.T, dwytHome, projectPath string, nodes, edges int) {
	t.Helper()
	codebaseDir := filepath.Join(dwytHome, "codebase")
	if err := os.MkdirAll(codebaseDir, 0755); err != nil {
		t.Fatal(err)
	}
	conn, err := sql.Open("sqlite", filepath.Join(codebaseDir, codebaseProjectName(projectPath)+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_, err = conn.Exec(`
		CREATE TABLE projects (name TEXT PRIMARY KEY, indexed_at TEXT NOT NULL, root_path TEXT NOT NULL);
		CREATE TABLE nodes (id INTEGER PRIMARY KEY AUTOINCREMENT, project TEXT NOT NULL);
		CREATE TABLE edges (id INTEGER PRIMARY KEY AUTOINCREMENT, project TEXT NOT NULL);
	`)
	if err != nil {
		t.Fatal(err)
	}
	projectName := codebaseProjectName(projectPath)
	if _, err := conn.Exec(`INSERT INTO projects (name, indexed_at, root_path) VALUES (?, ?, ?)`, projectName, time.Now().UTC().Format(time.RFC3339), projectPath); err != nil {
		t.Fatal(err)
	}

	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	nodeStmt, err := tx.Prepare(`INSERT INTO nodes (project) VALUES (?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < nodes; i++ {
		if _, err := nodeStmt.Exec(projectName); err != nil {
			t.Fatal(err)
		}
	}
	nodeStmt.Close()
	edgeStmt, err := tx.Prepare(`INSERT INTO edges (project) VALUES (?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < edges; i++ {
		if _, err := edgeStmt.Exec(projectName); err != nil {
			t.Fatal(err)
		}
	}
	edgeStmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
