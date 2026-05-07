package server

import "testing"

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
