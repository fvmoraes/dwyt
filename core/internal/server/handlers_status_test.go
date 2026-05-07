package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

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
