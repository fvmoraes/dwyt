package mcpregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The registry once stored the Codebase wiring under the historical "codebase"
// key while the canonical dwyt_codebase entry also existed. The sync then wrote
// BOTH into every client config, and the two instances of the same binary
// contended for the Codebase daemon until both failed (Kiro: "MCP error
// -32000: Connection closed" after the ~30s handoff timeout).

func TestSetCodebaseTargetStoresCanonicalKey(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the pre-fix state: a legacy key alongside the canonical one.
	legacyTarget := filepath.Join(dwytHome, "bin", "codebase-memory-mcp")
	reg.MCPServers["codebase"] = MCPServerEntry{Command: legacyTarget, Enabled: true}

	if err := reg.SetCodebaseTarget(legacyTarget); err != nil {
		t.Fatal(err)
	}

	if _, exists := reg.MCPServers["codebase"]; exists {
		t.Fatal("the legacy codebase key must be gone after SetCodebaseTarget")
	}
	entry, ok := reg.MCPServers[ServerCodebase]
	if !ok {
		t.Fatal("the entry must live under the canonical dwyt_codebase key")
	}
	if entry.Target != legacyTarget {
		t.Fatalf("unexpected target %q", entry.Target)
	}
	if !entry.Enabled {
		t.Fatal("the canonical entry's enabled flag must win")
	}

	// A canonical-disabled flag is never resurrected by a legacy entry.
	cb := reg.MCPServers[ServerCodebase]
	cb.Enabled = false
	reg.MCPServers[ServerCodebase] = cb
	reg.MCPServers["codebase"] = MCPServerEntry{Command: legacyTarget, Enabled: true}
	if err := reg.SetCodebaseTarget(legacyTarget); err != nil {
		t.Fatal(err)
	}
	if reg.MCPServers[ServerCodebase].Enabled {
		t.Fatal("canonical disabled must stay disabled")
	}
	if _, exists := reg.MCPServers["codebase"]; exists {
		t.Fatal("legacy key must still be cleaned in the disabled scenario")
	}
}

func TestEntriesForSyncMergesLegacyIntoCanonical(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))
	touchExecutable(t, filepath.Join(dwytHome, "bin", "codebase-memory-mcp"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// The exact live state that produced the duplicate: legacy + canonical.
	reg.MCPServers["codebase"] = MCPServerEntry{
		Command: filepath.Join(dwytHome, "bin", "codebase-memory-mcp"),
		Enabled: true,
	}

	entries := reg.entriesForSync(nil)
	if _, exists := entries["codebase"]; exists {
		t.Fatalf("legacy key must never reach client configs, got %v", keysOf(entries))
	}
	cb, ok := entries[ServerCodebase]
	if !ok {
		t.Fatalf("canonical codebase entry missing, got %v", keysOf(entries))
	}
	// The canonical entry carries the proxy wiring; the legacy one ran the raw
	// binary directly, so the command proves which entry won.
	if filepath.Base(cb.Command) != "dwyt" && filepath.Base(cb.Command) != "dwyt.exe" {
		t.Fatalf("expected the canonical shim wiring to win, got %q", cb.Command)
	}
	if filepath.Base(cb.Target) != "codebase-memory-mcp" && filepath.Base(cb.Target) != "codebase-memory-mcp.exe" {
		t.Fatalf("expected the proxy target on the canonical entry, got %q", cb.Target)
	}
	for _, want := range []string{ServerOptimizer, ServerObsidian} {
		if _, ok := entries[want]; !ok {
			t.Fatalf("canonical %s missing from sync entries: %v", want, keysOf(entries))
		}
	}
}

func TestSyncKiroCleansLegacyDuplicate(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))
	touchExecutable(t, filepath.Join(dwytHome, "bin", "codebase-memory-mcp"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	reg.MCPServers["codebase"] = MCPServerEntry{
		Command: filepath.Join(dwytHome, "bin", "codebase-memory-mcp"),
		Enabled: true,
	}

	project := t.TempDir()
	kiroDir := filepath.Join(project, ".kiro", "settings")
	if err := os.MkdirAll(kiroDir, 0755); err != nil {
		t.Fatal(err)
	}
	// A config file that already carries the duplicate, as written before the fix.
	stale := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"codebase": map[string]interface{}{
				"command": filepath.Join(dwytHome, "bin", "codebase-memory-mcp"),
				"type":    "stdio",
			},
			ServerCodebase:  map[string]interface{}{"command": "placeholder", "type": "stdio"},
			ServerObsidian:  map[string]interface{}{"command": "placeholder", "type": "stdio"},
			ServerOptimizer: map[string]interface{}{"command": "placeholder", "type": "stdio"},
		},
	}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(filepath.Join(kiroDir, "mcp.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := reg.syncKiro(project, nil); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(filepath.Join(kiroDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		MCPServers map[string]interface{} `json:"mcpServers"`
	}
	if err := json.Unmarshal(written, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, exists := parsed.MCPServers["codebase"]; exists {
		t.Fatalf("legacy codebase entry must be cleaned from the kiro config, got %v", parsed.MCPServers)
	}
	for _, want := range []string{ServerOptimizer, ServerCodebase, ServerObsidian} {
		if _, ok := parsed.MCPServers[want]; !ok {
			t.Fatalf("canonical %s missing after sync: %v", want, parsed.MCPServers)
		}
	}
}

func keysOf(entries map[string]MCPServerEntry) []string {
	out := make([]string, 0, len(entries))
	for k := range entries {
		out = append(out, k)
	}
	return out
}
